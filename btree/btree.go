package btree

import (
	"bytes"
	"encoding/binary"
	"errors"

	"github.com/obayona/godb/utils"
)

// Pages use separate internal and leaf layouts.
//
// Internal page:
//
//	| type | nkeys | child pointers | value offsets | separator keys |
//	|  2B  |   2B  |   nkeys * 8B   |  nkeys * 2B  |      ...       |
//
// Leaf page:
//
//	| type | nkeys | previous leaf | next leaf | value offsets | key-values |
//	|  2B  |   2B  |      8B       |    8B     |  nkeys * 2B  |    ...     |
//
// Each separator or key-value entry is encoded as:
//
//	| key length | value length | key | value |
//	|     2B     |      2B      | ... |  ...  |
//
// Internal values are empty. Leaf links contain physical page pointers and
// form a bidirectional list. The first leaf starts with the dummy empty key
// used to cover the key space; its previous pointer is zero. The final leaf's
// next pointer is zero.
const (
	INTERNAL_HEADER = 4
	LEAF_HEADER     = 20
)

const BTREE_PAGE_SIZE = 4096
const BTREE_MAX_KEY_SIZE = 1000
const BTREE_MAX_VAL_SIZE = 3000

func init() {
	node1max := LEAF_HEADER + 2 + 4 + BTREE_MAX_KEY_SIZE + BTREE_MAX_VAL_SIZE
	utils.Assert(node1max <= BTREE_PAGE_SIZE)
}

const (
	BNODE_NODE = 1 // internal nodes without values
	BNODE_LEAF = 2 // leaf nodes with values
)

type BNode []byte // can be dumped to the disk

type BTree struct {
	// pointer (a nonzero page number)
	Root uint64
	// callbacks for managing on-disk pages
	GetPage func(uint64) []byte // dereference a pointer
	NewPage func([]byte) uint64 // allocate a new page
	DelPage func(uint64)        // deallocate a page
	// SetPage returns writable bytes for an existing page. The tree uses it
	// only to maintain leaf links; key/value contents remain copy-on-write.
	SetPage func(uint64) []byte
	// DisableLeafLinks keeps iterators on root-to-leaf paths. It is intended for
	// historical snapshots whose link metadata may have advanced in a newer
	// copy-on-write tree version. Normal trees traverse sibling links.
	DisableLeafLinks bool
}

// header
func (node BNode) BType() uint16 {
	return binary.LittleEndian.Uint16(node[0:2])
}
func (node BNode) NKeys() uint16 {
	return binary.LittleEndian.Uint16(node[2:4])
}
func (node BNode) SetHeader(btype uint16, nkeys uint16) {
	binary.LittleEndian.PutUint16(node[0:2], btype)
	binary.LittleEndian.PutUint16(node[2:4], nkeys)
}

func (node BNode) headerSize() uint16 {
	if node.BType() == BNODE_LEAF {
		return LEAF_HEADER
	}
	return INTERNAL_HEADER
}

func (node BNode) pointerBytes() uint16 {
	if node.BType() == BNODE_NODE {
		return 8 * node.NKeys()
	}
	return 0
}

// PrevLeaf returns the previous leaf page pointer.
func (node BNode) PrevLeaf() uint64 {
	utils.Assert(node.BType() == BNODE_LEAF)
	return binary.LittleEndian.Uint64(node[4:12])
}

// NextLeaf returns the next leaf page pointer.
func (node BNode) NextLeaf() uint64 {
	utils.Assert(node.BType() == BNODE_LEAF)
	return binary.LittleEndian.Uint64(node[12:20])
}

// SetPrevLeaf sets the previous leaf page pointer.
func (node BNode) SetPrevLeaf(ptr uint64) {
	utils.Assert(node.BType() == BNODE_LEAF)
	binary.LittleEndian.PutUint64(node[4:12], ptr)
}

// SetNextLeaf sets the next leaf page pointer.
func (node BNode) SetNextLeaf(ptr uint64) {
	utils.Assert(node.BType() == BNODE_LEAF)
	binary.LittleEndian.PutUint64(node[12:20], ptr)
}

// pointers
func (node BNode) GetPtr(idx uint16) uint64 {
	utils.Assert(node.BType() == BNODE_NODE)
	utils.Assert(idx < node.NKeys())
	pos := INTERNAL_HEADER + 8*idx
	return binary.LittleEndian.Uint64(node[pos:])
}
func (node BNode) SetPtr(idx uint16, val uint64) {
	utils.Assert(node.BType() == BNODE_NODE)
	utils.Assert(idx < node.NKeys())
	// utils.Assert(node.BType() == BNODE_LEAF || val != 0)
	// utils.Assert(node.BType() == BNODE_NODE || val == 0)
	pos := INTERNAL_HEADER + 8*idx
	binary.LittleEndian.PutUint64(node[pos:], val)
}

// offset list
func offsetPos(node BNode, idx uint16) uint16 {
	utils.Assert(1 <= idx && idx <= node.NKeys())
	return node.headerSize() + node.pointerBytes() + 2*(idx-1)
}
func (node BNode) GetOffset(idx uint16) uint16 {
	if idx == 0 {
		return 0
	}
	return binary.LittleEndian.Uint16(node[offsetPos(node, idx):])
}
func (node BNode) SetOffset(idx uint16, offset uint16) {
	binary.LittleEndian.PutUint16(node[offsetPos(node, idx):], offset)
}

// key-values
func (node BNode) KVPos(idx uint16) uint16 {
	utils.Assert(idx <= node.NKeys())
	return node.headerSize() + node.pointerBytes() + 2*node.NKeys() + node.GetOffset(idx)
}
func (node BNode) GetKey(idx uint16) []byte {
	utils.Assert(idx < node.NKeys())
	pos := node.KVPos(idx)
	klen := binary.LittleEndian.Uint16(node[pos:])
	return node[pos+4:][:klen]
}
func (node BNode) GetVal(idx uint16) []byte {
	utils.Assert(idx < node.NKeys())
	pos := node.KVPos(idx)
	klen := binary.LittleEndian.Uint16(node[pos+0:])
	vlen := binary.LittleEndian.Uint16(node[pos+2:])
	return node[pos+4+klen:][:vlen]
}

// node size in bytes
func (node BNode) NBytes() uint16 {
	return node.KVPos(node.NKeys())
}

// returns the first kid node whose range intersects the key. (kid[i] <= key)
// TODO: bisect
func nodeLookupLE(node BNode, key []byte) uint16 {
	nkeys := node.NKeys()
	found := uint16(0)
	// the first key is a copy from the parent node,
	// thus it's always less than or equal to the key.
	for i := uint16(1); i < nkeys; i++ {
		cmp := bytes.Compare(node.GetKey(i), key)
		if cmp <= 0 {
			found = i
		}
		if cmp >= 0 {
			break
		}
	}
	return found
}

// add a new key to a leaf node
func leafInsert(
	new BNode, old BNode, idx uint16,
	key []byte, val []byte,
) {
	new.SetHeader(BNODE_LEAF, old.NKeys()+1)
	new.SetPrevLeaf(old.PrevLeaf())
	new.SetNextLeaf(old.NextLeaf())
	nodeAppendRange(new, old, 0, 0, idx)
	nodeAppendKV(new, idx, 0, key, val)
	nodeAppendRange(new, old, idx+1, idx, old.NKeys()-idx)
}

// update an existing key from a leaf node
func leafUpdate(
	new BNode, old BNode, idx uint16,
	key []byte, val []byte,
) {
	new.SetHeader(BNODE_LEAF, old.NKeys())
	new.SetPrevLeaf(old.PrevLeaf())
	new.SetNextLeaf(old.NextLeaf())
	nodeAppendRange(new, old, 0, 0, idx)
	nodeAppendKV(new, idx, 0, key, val)
	nodeAppendRange(new, old, idx+1, idx+1, old.NKeys()-(idx+1))
}

// replace a link with the same key
func nodeReplaceKid1ptr(new BNode, old BNode, idx uint16, ptr uint64) {
	copy(new, old[:old.NBytes()])
	new.SetPtr(idx, ptr) // only the pointer is changed
}

// replace a link with multiple links
func nodeReplaceKidN(
	tree *BTree, new BNode, old BNode, idx uint16,
	kids ...BNode,
) {
	ptrs := make([]uint64, len(kids))
	for i, kid := range kids {
		ptrs[i] = tree.NewPage(kid)
	}
	nodeReplaceKidNPtrs(new, old, idx, kids, ptrs)
}

func nodeReplaceKidNPtrs(
	new BNode, old BNode, idx uint16,
	kids []BNode, ptrs []uint64,
) {
	utils.Assert(len(kids) == len(ptrs))
	inc := uint16(len(kids))
	if inc == 1 && bytes.Equal(kids[0].GetKey(0), old.GetKey(idx)) {
		// common case, only replace 1 pointer
		nodeReplaceKid1ptr(new, old, idx, ptrs[0])
		return
	}

	new.SetHeader(BNODE_NODE, old.NKeys()+inc-1)
	nodeAppendRange(new, old, 0, 0, idx)
	for i, node := range kids {
		nodeAppendKV(new, idx+uint16(i), ptrs[i], node.GetKey(0), nil)
	}
	nodeAppendRange(new, old, idx+inc, idx+1, old.NKeys()-(idx+1))
}

// allocateLeafReplacement allocates one or more leaves that replace oldPtr,
// links them together, and redirects the two outside neighbors. Existing leaf
// contents remain copy-on-write; only sibling-link fields are updated in place.
func allocateLeafReplacement(
	tree *BTree, oldPtr uint64, old BNode, leaves []BNode,
) []uint64 {
	utils.Assert(old.BType() == BNODE_LEAF)
	utils.Assert(len(leaves) > 0)
	ptrs := make([]uint64, len(leaves))
	for i, leaf := range leaves {
		utils.Assert(leaf.BType() == BNODE_LEAF)
		ptrs[i] = tree.NewPage(leaf)
	}
	for i, ptr := range ptrs {
		leaf := BNode(tree.SetPage(ptr))
		if i == 0 {
			leaf.SetPrevLeaf(old.PrevLeaf())
		} else {
			leaf.SetPrevLeaf(ptrs[i-1])
		}
		if i+1 == len(ptrs) {
			leaf.SetNextLeaf(old.NextLeaf())
		} else {
			leaf.SetNextLeaf(ptrs[i+1])
		}
	}
	if prev := old.PrevLeaf(); prev != 0 {
		BNode(tree.SetPage(prev)).SetNextLeaf(ptrs[0])
	}
	if next := old.NextLeaf(); next != 0 {
		BNode(tree.SetPage(next)).SetPrevLeaf(ptrs[len(ptrs)-1])
	}
	return ptrs
}

func allocateLeafMerge(
	tree *BTree, leftPtr uint64, left BNode, rightPtr uint64, right BNode, merged BNode,
) uint64 {
	utils.Assert(left.NextLeaf() == rightPtr)
	utils.Assert(right.PrevLeaf() == leftPtr)
	ptr := tree.NewPage(merged)
	leaf := BNode(tree.SetPage(ptr))
	leaf.SetPrevLeaf(left.PrevLeaf())
	leaf.SetNextLeaf(right.NextLeaf())
	if prev := left.PrevLeaf(); prev != 0 {
		BNode(tree.SetPage(prev)).SetNextLeaf(ptr)
	}
	if next := right.NextLeaf(); next != 0 {
		BNode(tree.SetPage(next)).SetPrevLeaf(ptr)
	}
	return ptr
}

// replace 2 adjacent links with 1
func nodeReplace2Kid(
	new BNode, old BNode, idx uint16,
	ptr uint64, key []byte,
) {
	new.SetHeader(BNODE_NODE, old.NKeys()-1)
	nodeAppendRange(new, old, 0, 0, idx)
	nodeAppendKV(new, idx, ptr, key, nil)
	nodeAppendRange(new, old, idx+1, idx+2, old.NKeys()-(idx+2))
}

// copy a KV into the position
func nodeAppendKV(new BNode, idx uint16, ptr uint64, key []byte, val []byte) {
	// ptrs
	if new.BType() == BNODE_NODE {
		new.SetPtr(idx, ptr)
	} else {
		utils.Assert(ptr == 0)
	}
	// KVs
	pos := new.KVPos(idx)
	binary.LittleEndian.PutUint16(new[pos+0:], uint16(len(key)))
	binary.LittleEndian.PutUint16(new[pos+2:], uint16(len(val)))
	copy(new[pos+4:], key)
	copy(new[pos+4+uint16(len(key)):], val)
	// the offset of the next key
	new.SetOffset(idx+1, new.GetOffset(idx)+4+uint16((len(key)+len(val))))
}

// copy multiple KVs into the position
func nodeAppendRange(
	new BNode, old BNode,
	dstNew uint16, srcOld uint16, n uint16,
) {
	utils.Assert(srcOld+n <= old.NKeys())
	utils.Assert(dstNew+n <= new.NKeys())
	if n == 0 {
		return
	}

	// pointers
	if new.BType() == BNODE_NODE {
		utils.Assert(old.BType() == BNODE_NODE)
		for i := uint16(0); i < n; i++ {
			new.SetPtr(dstNew+i, old.GetPtr(srcOld+i))
		}
	}
	// offsets
	dstBegin := new.GetOffset(dstNew)
	srcBegin := old.GetOffset(srcOld)
	for i := uint16(1); i <= n; i++ { // NOTE: the range is [1, n]
		offset := dstBegin + old.GetOffset(srcOld+i) - srcBegin
		new.SetOffset(dstNew+i, offset)
	}
	// KVs
	begin := old.KVPos(srcOld)
	end := old.KVPos(srcOld + n)
	copy(new[new.KVPos(dstNew):], old[begin:end])
}

// split a bigger-than-allowed node into two.
// the second node always fits on a page.
func nodeSplit2(left BNode, right BNode, old BNode) {
	utils.Assert(old.NKeys() >= 2)

	// the initial guess
	nleft := old.NKeys() / 2

	// try to fit the left half
	nbytes := func(start, count uint16) uint16 {
		header := uint16(INTERNAL_HEADER)
		pointers := uint16(8 * count)
		if old.BType() == BNODE_LEAF {
			header = LEAF_HEADER
			pointers = 0
		}
		payload := old.GetOffset(start+count) - old.GetOffset(start)
		return header + pointers + 2*count + payload
	}
	leftBytes := func() uint16 {
		return nbytes(0, nleft)
	}
	for leftBytes() > BTREE_PAGE_SIZE {
		nleft--
	}
	utils.Assert(nleft >= 1)

	// try to fit the right half
	rightBytes := func() uint16 {
		return nbytes(nleft, old.NKeys()-nleft)
	}
	for rightBytes() > BTREE_PAGE_SIZE {
		nleft++
	}
	utils.Assert(nleft < old.NKeys())
	nright := old.NKeys() - nleft

	left.SetHeader(old.BType(), nleft)
	right.SetHeader(old.BType(), nright)
	if old.BType() == BNODE_LEAF {
		left.SetPrevLeaf(old.PrevLeaf())
		right.SetNextLeaf(old.NextLeaf())
	}
	nodeAppendRange(left, old, 0, 0, nleft)
	nodeAppendRange(right, old, 0, nleft, nright)
	// the left half may be still too big
	utils.Assert(right.NBytes() <= BTREE_PAGE_SIZE)
}

// split a node if it's too big. the results are 1~3 nodes.
func nodeSplit3(old BNode) (uint16, [3]BNode) {
	if old.NBytes() <= BTREE_PAGE_SIZE {
		old = old[:BTREE_PAGE_SIZE]
		return 1, [3]BNode{old} // not split
	}
	left := BNode(make([]byte, 2*BTREE_PAGE_SIZE)) // might be split later
	right := BNode(make([]byte, BTREE_PAGE_SIZE))
	nodeSplit2(left, right, old)
	if left.NBytes() <= BTREE_PAGE_SIZE {
		left = left[:BTREE_PAGE_SIZE]
		return 2, [3]BNode{left, right} // 2 nodes
	}
	leftleft := BNode(make([]byte, BTREE_PAGE_SIZE))
	middle := BNode(make([]byte, BTREE_PAGE_SIZE))
	nodeSplit2(leftleft, middle, left)
	utils.Assert(leftleft.NBytes() <= BTREE_PAGE_SIZE)
	return 3, [3]BNode{leftleft, middle, right} // 3 nodes
}

// update modes
const (
	MODE_UPSERT      = 0 // insert or replace
	MODE_UPDATE_ONLY = 1 // update existing keys
	MODE_INSERT_ONLY = 2 // only add new keys
)

type UpdateReq struct {
	tree *BTree
	// out
	Added   bool   // added a new key
	Updated bool   // added a new key or an old key was changed
	Old     []byte // the value before the update
	// in
	Key  []byte
	Val  []byte
	Mode int
}

// insert a KV into a node, the result might be split.
// the caller is responsible for deallocating the input node
// and splitting and allocating result nodes.
func treeInsert(req *UpdateReq, node BNode) BNode {
	// the result node.
	// it's allowed to be bigger than 1 page and will be split if so
	new := BNode(make([]byte, 2*BTREE_PAGE_SIZE))
	// where to insert the key?
	idx := nodeLookupLE(node, req.Key)
	// act depending on the node type
	switch node.BType() {
	case BNODE_LEAF:
		// leaf, node.GetKey(idx) <= key
		if bytes.Equal(req.Key, node.GetKey(idx)) {
			// found the key, update it.
			if req.Mode == MODE_INSERT_ONLY {
				return BNode{}
			}
			if bytes.Equal(req.Val, node.GetVal(idx)) {
				return BNode{}
			}
			leafUpdate(new, node, idx, req.Key, req.Val)
			req.Updated = true
			req.Old = node.GetVal(idx)
		} else {
			// insert it after the position.
			if req.Mode == MODE_UPDATE_ONLY {
				return BNode{}
			}
			leafInsert(new, node, idx+1, req.Key, req.Val)
			req.Updated = true
			req.Added = true
		}
		return new
	case BNODE_NODE:
		// internal node, insert it to a kid node.
		return nodeInsert(req, new, node, idx)
	default:
		panic("bad node!")
	}
}

// part of the treeInsert(): KV insertion to an internal node
func nodeInsert(req *UpdateReq, new BNode, node BNode, idx uint16) BNode {
	kptr := node.GetPtr(idx)
	kid := BNode(req.tree.GetPage(kptr))
	// recursive insertion to the kid node
	updated := treeInsert(req, kid)
	if len(updated) == 0 {
		return BNode{}
	}
	// split the result
	nsplit, split := nodeSplit3(updated)
	if kid.BType() == BNODE_LEAF {
		ptrs := allocateLeafReplacement(req.tree, kptr, kid, split[:nsplit])
		req.tree.DelPage(kptr)
		nodeReplaceKidNPtrs(new, node, idx, split[:nsplit], ptrs)
	} else {
		// deallocate and replace an internal child.
		req.tree.DelPage(kptr)
		nodeReplaceKidN(req.tree, new, node, idx, split[:nsplit]...)
	}
	return new
}

// remove a key from a leaf node
func leafDelete(new BNode, old BNode, idx uint16) {
	new.SetHeader(BNODE_LEAF, old.NKeys()-1)
	new.SetPrevLeaf(old.PrevLeaf())
	new.SetNextLeaf(old.NextLeaf())
	nodeAppendRange(new, old, 0, 0, idx)
	nodeAppendRange(new, old, idx, idx+1, old.NKeys()-(idx+1))
}

// merge 2 nodes into 1
func nodeMerge(new BNode, left BNode, right BNode) {
	utils.Assert(left.BType() == right.BType())
	new.SetHeader(left.BType(), left.NKeys()+right.NKeys())
	if left.BType() == BNODE_LEAF {
		new.SetPrevLeaf(left.PrevLeaf())
		new.SetNextLeaf(right.NextLeaf())
	}
	nodeAppendRange(new, left, 0, 0, left.NKeys())
	nodeAppendRange(new, right, left.NKeys(), 0, right.NKeys())
	utils.Assert(new.NBytes() <= BTREE_PAGE_SIZE)
}

type DeleteReq struct {
	tree *BTree
	// in
	Key []byte
	// out
	Old []byte
}

// delete a key from the tree
func treeDelete(req *DeleteReq, node BNode) BNode {
	// where to find the key?
	idx := nodeLookupLE(node, req.Key)
	// act depending on the node type
	switch node.BType() {
	case BNODE_LEAF:
		if !bytes.Equal(req.Key, node.GetKey(idx)) {
			return BNode{} // not found
		}
		// delete the key in the leaf
		req.Old = node.GetVal(idx)
		new := BNode(make([]byte, BTREE_PAGE_SIZE))
		leafDelete(new, node, idx)
		return new
	case BNODE_NODE:
		return nodeDelete(req, node, idx)
	default:
		panic("bad node!")
	}
}

// part of the treeDelete()
func nodeDelete(req *DeleteReq, node BNode, idx uint16) BNode {
	tree := req.tree
	// recurse into the kid
	kptr := node.GetPtr(idx)
	kid := BNode(tree.GetPage(kptr))
	updated := treeDelete(req, kid)
	if len(updated) == 0 {
		return BNode{} // not found
	}
	new := BNode(make([]byte, BTREE_PAGE_SIZE))
	// check for merging
	mergeDir, sibling := shouldMerge(tree, node, idx, updated)
	switch {
	case mergeDir < 0: // left
		siblingPtr := node.GetPtr(idx - 1)
		merged := BNode(make([]byte, BTREE_PAGE_SIZE))
		nodeMerge(merged, sibling, updated)
		var ptr uint64
		if kid.BType() == BNODE_LEAF {
			ptr = allocateLeafMerge(tree, siblingPtr, sibling, kptr, kid, merged)
		} else {
			ptr = tree.NewPage(merged)
		}
		tree.DelPage(siblingPtr)
		tree.DelPage(kptr)
		nodeReplace2Kid(new, node, idx-1, ptr, merged.GetKey(0))
	case mergeDir > 0: // right
		siblingPtr := node.GetPtr(idx + 1)
		merged := BNode(make([]byte, BTREE_PAGE_SIZE))
		nodeMerge(merged, updated, sibling)
		var ptr uint64
		if kid.BType() == BNODE_LEAF {
			ptr = allocateLeafMerge(tree, kptr, kid, siblingPtr, sibling, merged)
		} else {
			ptr = tree.NewPage(merged)
		}
		tree.DelPage(kptr)
		tree.DelPage(siblingPtr)
		nodeReplace2Kid(new, node, idx, ptr, merged.GetKey(0))
	case mergeDir == 0 && updated.NKeys() == 0:
		tree.DelPage(kptr)
		utils.Assert(node.NKeys() == 1 && idx == 0) // 1 empty child but no sibling
		new.SetHeader(BNODE_NODE, 0)                // the parent becomes empty too
	case mergeDir == 0 && updated.NKeys() > 0: // no merge
		if kid.BType() == BNODE_LEAF {
			ptrs := allocateLeafReplacement(tree, kptr, kid, []BNode{updated})
			tree.DelPage(kptr)
			nodeReplaceKidNPtrs(new, node, idx, []BNode{updated}, ptrs)
		} else {
			tree.DelPage(kptr)
			nodeReplaceKidN(tree, new, node, idx, updated)
		}
	}
	return new
}

// should the updated kid be merged with a sibling?
func shouldMerge(
	tree *BTree, node BNode,
	idx uint16, updated BNode,
) (int, BNode) {
	if updated.NBytes() > BTREE_PAGE_SIZE/4 {
		return 0, BNode{}
	}

	if idx > 0 {
		sibling := BNode(tree.GetPage(node.GetPtr(idx - 1)))
		merged := mergedNodeSize(sibling, updated)
		if merged <= BTREE_PAGE_SIZE {
			return -1, sibling // left
		}
	}
	if idx+1 < node.NKeys() {
		sibling := BNode(tree.GetPage(node.GetPtr(idx + 1)))
		merged := mergedNodeSize(updated, sibling)
		if merged <= BTREE_PAGE_SIZE {
			return +1, sibling // right
		}
	}
	return 0, BNode{}
}

func mergedNodeSize(left, right BNode) uint16 {
	header := uint16(INTERNAL_HEADER)
	pointers := uint16(8 * (left.NKeys() + right.NKeys()))
	if left.BType() == BNODE_LEAF {
		header = LEAF_HEADER
		pointers = 0
	}
	return header + pointers + 2*(left.NKeys()+right.NKeys()) +
		left.GetOffset(left.NKeys()) + right.GetOffset(right.NKeys())
}

func checkLimit(key []byte, val []byte) error {
	if len(key) == 0 {
		return errors.New("empty key") // used as a dummy key
	}
	if len(key) > BTREE_MAX_KEY_SIZE {
		return errors.New("key too long")
	}
	if len(val) > BTREE_MAX_VAL_SIZE {
		return errors.New("value too long")
	}
	return nil
}

// the interface
func (tree *BTree) Upsert(key []byte, val []byte) (bool, error) {
	return tree.Update(&UpdateReq{Key: key, Val: val})
}

func (tree *BTree) Update(req *UpdateReq) (bool, error) {
	if err := checkLimit(req.Key, req.Val); err != nil {
		return false, err // the only way for an update to fail
	}

	if tree.Root == 0 {
		// create the first node
		root := BNode(make([]byte, BTREE_PAGE_SIZE))
		root.SetHeader(BNODE_LEAF, 2)
		// a dummy key, this makes the tree cover the whole key space.
		// thus a lookup can always find a containing node.
		nodeAppendKV(root, 0, 0, nil, nil)
		nodeAppendKV(root, 1, 0, req.Key, req.Val)
		tree.Root = tree.NewPage(root)
		req.Added = true
		req.Updated = true
		return true, nil
	}

	req.tree = tree
	oldRootPtr := tree.Root
	oldRoot := BNode(tree.GetPage(oldRootPtr))
	updated := treeInsert(req, oldRoot)
	if len(updated) == 0 {
		return false, nil // not updated
	}

	// replace the root node
	nsplit, split := nodeSplit3(updated)
	if oldRoot.BType() == BNODE_LEAF {
		ptrs := allocateLeafReplacement(tree, oldRootPtr, oldRoot, split[:nsplit])
		tree.DelPage(oldRootPtr)
		if nsplit == 1 {
			tree.Root = ptrs[0]
			return true, nil
		}
		root := BNode(make([]byte, BTREE_PAGE_SIZE))
		root.SetHeader(BNODE_NODE, nsplit)
		for i, knode := range split[:nsplit] {
			nodeAppendKV(root, uint16(i), ptrs[i], knode.GetKey(0), nil)
		}
		tree.Root = tree.NewPage(root)
		return true, nil
	}
	tree.DelPage(oldRootPtr)
	if nsplit > 1 {
		// the root was split, add a new level.
		root := BNode(make([]byte, BTREE_PAGE_SIZE))
		root.SetHeader(BNODE_NODE, nsplit)
		for i, knode := range split[:nsplit] {
			ptr, key := tree.NewPage(knode), knode.GetKey(0)
			nodeAppendKV(root, uint16(i), ptr, key, nil)
		}
		tree.Root = tree.NewPage(root)
	} else {
		tree.Root = tree.NewPage(split[0])
	}
	return true, nil
}

func (tree *BTree) Delete(req *DeleteReq) (bool, error) {
	if err := checkLimit(req.Key, nil); err != nil {
		return false, err // the only way for an update to fail
	}

	if tree.Root == 0 {
		return false, nil
	}

	req.tree = tree
	oldRootPtr := tree.Root
	oldRoot := BNode(tree.GetPage(oldRootPtr))
	updated := treeDelete(req, oldRoot)
	if len(updated) == 0 {
		return false, nil // not found
	}

	if oldRoot.BType() == BNODE_LEAF {
		ptrs := allocateLeafReplacement(tree, oldRootPtr, oldRoot, []BNode{updated})
		tree.DelPage(oldRootPtr)
		tree.Root = ptrs[0]
		return true, nil
	}

	tree.DelPage(oldRootPtr)
	if updated.BType() == BNODE_NODE && updated.NKeys() == 1 {
		// remove a level
		tree.Root = updated.GetPtr(0)
	} else {
		tree.Root = tree.NewPage(updated)
	}
	return true, nil
}

func nodeGetKey(tree *BTree, node BNode, key []byte) ([]byte, bool) {
	idx := nodeLookupLE(node, key)
	switch node.BType() {
	case BNODE_LEAF:
		if bytes.Equal(key, node.GetKey(idx)) {
			return node.GetVal(idx), true
		} else {
			return nil, false
		}
	case BNODE_NODE:
		return nodeGetKey(tree, tree.GetPage(node.GetPtr(idx)), key)
	default:
		panic("bad node!")
	}
}

func (tree *BTree) Get(key []byte) ([]byte, bool) {
	if tree.Root == 0 {
		return nil, false
	}
	return nodeGetKey(tree, tree.GetPage(tree.Root), key)
}
