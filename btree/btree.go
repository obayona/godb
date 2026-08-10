package btree

import (
	"bytes"
	"encoding/binary"
	"errors"

	"github.com/obayona/godb/utils"
)

// node format:
// | type | nkeys |  pointers  |   offsets  | key-values
// |  2B  |   2B  | nkeys * 8B | nkeys * 2B | ...

// key-value format:
// | klen | vlen | key | val |
// |  2B  |  2B  | ... | ... |

const HEADER = 4

const BTREE_PAGE_SIZE = 4096
const BTREE_MAX_KEY_SIZE = 1000
const BTREE_MAX_VAL_SIZE = 3000

func init() {
	node1max := HEADER + 8 + 2 + 4 + BTREE_MAX_KEY_SIZE + BTREE_MAX_VAL_SIZE
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

// pointers
func (node BNode) GetPtr(idx uint16) uint64 {
	utils.Assert(idx < node.NKeys())
	pos := HEADER + 8*idx
	return binary.LittleEndian.Uint64(node[pos:])
}
func (node BNode) SetPtr(idx uint16, val uint64) {
	utils.Assert(idx < node.NKeys())
	// utils.Assert(node.BType() == BNODE_LEAF || val != 0)
	// utils.Assert(node.BType() == BNODE_NODE || val == 0)
	pos := HEADER + 8*idx
	binary.LittleEndian.PutUint64(node[pos:], val)
}

// offset list
func offsetPos(node BNode, idx uint16) uint16 {
	utils.Assert(1 <= idx && idx <= node.NKeys())
	return HEADER + 8*node.NKeys() + 2*(idx-1)
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
	return HEADER + 8*node.NKeys() + 2*node.NKeys() + node.GetOffset(idx)
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
	inc := uint16(len(kids))
	if inc == 1 && bytes.Equal(kids[0].GetKey(0), old.GetKey(idx)) {
		// common case, only replace 1 pointer
		nodeReplaceKid1ptr(new, old, idx, tree.NewPage(kids[0]))
		return
	}

	new.SetHeader(BNODE_NODE, old.NKeys()+inc-1)
	nodeAppendRange(new, old, 0, 0, idx)
	for i, node := range kids {
		nodeAppendKV(new, idx+uint16(i), tree.NewPage(node), node.GetKey(0), nil)
	}
	nodeAppendRange(new, old, idx+inc, idx+1, old.NKeys()-(idx+1))
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
	new.SetPtr(idx, ptr)
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
	for i := uint16(0); i < n; i++ {
		new.SetPtr(dstNew+i, old.GetPtr(srcOld+i))
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
	left_bytes := func() uint16 {
		return HEADER + 8*nleft + 2*nleft + old.GetOffset(nleft)
	}
	for left_bytes() > BTREE_PAGE_SIZE {
		nleft--
	}
	utils.Assert(nleft >= 1)

	// try to fit the right half
	right_bytes := func() uint16 {
		return old.NBytes() - left_bytes() + HEADER
	}
	for right_bytes() > BTREE_PAGE_SIZE {
		nleft++
	}
	utils.Assert(nleft < old.NKeys())
	nright := old.NKeys() - nleft

	left.SetHeader(old.BType(), nleft)
	right.SetHeader(old.BType(), nright)
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
	// recursive insertion to the kid node
	updated := treeInsert(req, req.tree.GetPage(kptr))
	if len(updated) == 0 {
		return BNode{}
	}
	// split the result
	nsplit, split := nodeSplit3(updated)
	// deallocate the kid node
	req.tree.DelPage(kptr)
	// update the kid links
	nodeReplaceKidN(req.tree, new, node, idx, split[:nsplit]...)
	return new
}

// remove a key from a leaf node
func leafDelete(new BNode, old BNode, idx uint16) {
	new.SetHeader(BNODE_LEAF, old.NKeys()-1)
	nodeAppendRange(new, old, 0, 0, idx)
	nodeAppendRange(new, old, idx, idx+1, old.NKeys()-(idx+1))
}

// merge 2 nodes into 1
func nodeMerge(new BNode, left BNode, right BNode) {
	new.SetHeader(left.BType(), left.NKeys()+right.NKeys())
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
	updated := treeDelete(req, tree.GetPage(kptr))
	if len(updated) == 0 {
		return BNode{} // not found
	}
	tree.DelPage(kptr)

	new := BNode(make([]byte, BTREE_PAGE_SIZE))
	// check for merging
	mergeDir, sibling := shouldMerge(tree, node, idx, updated)
	switch {
	case mergeDir < 0: // left
		merged := BNode(make([]byte, BTREE_PAGE_SIZE))
		nodeMerge(merged, sibling, updated)
		tree.DelPage(node.GetPtr(idx - 1))
		nodeReplace2Kid(new, node, idx-1, tree.NewPage(merged), merged.GetKey(0))
	case mergeDir > 0: // right
		merged := BNode(make([]byte, BTREE_PAGE_SIZE))
		nodeMerge(merged, updated, sibling)
		tree.DelPage(node.GetPtr(idx + 1))
		nodeReplace2Kid(new, node, idx, tree.NewPage(merged), merged.GetKey(0))
	case mergeDir == 0 && updated.NKeys() == 0:
		utils.Assert(node.NKeys() == 1 && idx == 0) // 1 empty child but no sibling
		new.SetHeader(BNODE_NODE, 0)                // the parent becomes empty too
	case mergeDir == 0 && updated.NKeys() > 0: // no merge
		nodeReplaceKidN(tree, new, node, idx, updated)
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
		merged := sibling.NBytes() + updated.NBytes() - HEADER
		if merged <= BTREE_PAGE_SIZE {
			return -1, sibling // left
		}
	}
	if idx+1 < node.NKeys() {
		sibling := BNode(tree.GetPage(node.GetPtr(idx + 1)))
		merged := sibling.NBytes() + updated.NBytes() - HEADER
		if merged <= BTREE_PAGE_SIZE {
			return +1, sibling // right
		}
	}
	return 0, BNode{}
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
	updated := treeInsert(req, tree.GetPage(tree.Root))
	if len(updated) == 0 {
		return false, nil // not updated
	}

	// replace the root node
	nsplit, split := nodeSplit3(updated)
	tree.DelPage(tree.Root)
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
	updated := treeDelete(req, tree.GetPage(tree.Root))
	if len(updated) == 0 {
		return false, nil // not found
	}

	tree.DelPage(tree.Root)
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
