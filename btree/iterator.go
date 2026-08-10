package btree

import (
	"bytes"

	"github.com/obayona/godb/utils"
)

// B-tree iterator
type BIter struct {
	tree    *BTree
	path    []BNode  // from root to leaf
	pos     []uint16 // indexes into nodes
	leafPtr uint64
}

// current KV pair
func (iter *BIter) Deref() ([]byte, []byte) {
	utils.Assert(iter.Valid())
	last := len(iter.path) - 1
	node := iter.path[last]
	pos := iter.pos[last]
	return node.GetKey(pos), node.GetVal(pos)
}

func iterIsFirst(iter *BIter) bool {
	last := len(iter.path) - 1
	return last < 0 || iter.pos[last] == 0 &&
		iter.path[last].BType() == BNODE_LEAF &&
		iter.path[last].PrevLeaf() == 0
}

func iterIsEnd(iter *BIter) bool {
	last := len(iter.path) - 1
	return last < 0 || iter.pos[last] >= iter.path[last].NKeys()
}

func (iter *BIter) Valid() bool {
	return !(iterIsFirst(iter) || iterIsEnd(iter))
}

func iterPrev(iter *BIter, level int) {
	if iter.pos[level] > 0 {
		iter.pos[level]-- // move within this node
	} else if level > 0 {
		iterPrev(iter, level-1) // move to a sibling node
	} else {
		panic("unreachable") // dummy key
	}
	if level+1 < len(iter.pos) { // update the child node
		node := iter.path[level]
		kid := BNode(iter.tree.GetPage(node.GetPtr(iter.pos[level])))
		iter.path[level+1] = kid
		iter.pos[level+1] = kid.NKeys() - 1
	}
}

func iterNext(iter *BIter, level int) {
	if iter.pos[level]+1 < iter.path[level].NKeys() {
		iter.pos[level]++ // move within this node
	} else if level > 0 {
		iterNext(iter, level-1) // move to a sibling node
	} else {
		leaf := len(iter.pos) - 1
		iter.pos[leaf]++
		utils.Assert(iter.pos[leaf] == iter.path[leaf].NKeys())
		return // past the last key
	}
	if level+1 < len(iter.pos) { // update the child node
		node := iter.path[level]
		kid := BNode(iter.tree.GetPage(node.GetPtr(iter.pos[level])))
		iter.path[level+1] = kid
		iter.pos[level+1] = 0
	}
}

func (iter *BIter) Prev() {
	if !iterIsFirst(iter) {
		if !iter.tree.DisableLeafLinks {
			last := len(iter.path) - 1
			if iter.pos[last] > 0 {
				iter.pos[last]--
				return
			}
			ptr := iter.path[last].PrevLeaf()
			utils.Assert(ptr != 0)
			leaf := BNode(iter.tree.GetPage(ptr))
			iter.path[last] = leaf
			iter.pos[last] = leaf.NKeys() - 1
			iter.leafPtr = ptr
			return
		}
		iterPrev(iter, len(iter.path)-1)
	}
}

func (iter *BIter) Next() {
	if !iterIsEnd(iter) {
		if !iter.tree.DisableLeafLinks {
			last := len(iter.path) - 1
			if iter.pos[last]+1 < iter.path[last].NKeys() {
				iter.pos[last]++
				return
			}
			ptr := iter.path[last].NextLeaf()
			if ptr == 0 {
				iter.pos[last] = iter.path[last].NKeys()
				return
			}
			leaf := BNode(iter.tree.GetPage(ptr))
			iter.path[last] = leaf
			iter.pos[last] = 0
			iter.leafPtr = ptr
			return
		}
		iterNext(iter, len(iter.path)-1)
	}
}

// find the closest position that is less or equal to the input key
func (tree *BTree) SeekLE(key []byte) *BIter {
	iter := &BIter{tree: tree}
	for ptr := tree.Root; ptr != 0; {
		node := BNode(tree.GetPage(ptr))
		idx := nodeLookupLE(node, key)
		iter.path = append(iter.path, node)
		iter.pos = append(iter.pos, idx)
		if node.BType() == BNODE_LEAF {
			iter.leafPtr = ptr
			break
		}
		ptr = node.GetPtr(idx)
	}
	return iter
}

const (
	CMP_GE = +3 // >=
	CMP_GT = +2 // >
	CMP_LT = -2 // <
	CMP_LE = -3 // <=
)

// key cmp ref
func CmpOK(key []byte, cmp int, ref []byte) bool {
	r := bytes.Compare(key, ref)
	switch cmp {
	case CMP_GE:
		return r >= 0
	case CMP_GT:
		return r > 0
	case CMP_LT:
		return r < 0
	case CMP_LE:
		return r <= 0
	default:
		panic("what?")
	}
}

// find the closest position to a key with respect to the `cmp` relation
func (tree *BTree) Seek(key []byte, cmp int) *BIter {
	iter := tree.SeekLE(key)
	utils.Assert(iterIsFirst(iter) || !iterIsEnd(iter))
	if cmp != CMP_LE {
		cur := []byte(nil) // dummy key
		if !iterIsFirst(iter) {
			cur, _ = iter.Deref()
		}
		if len(key) == 0 || !CmpOK(cur, cmp, key) {
			// off by one
			if cmp > 0 {
				iter.Next()
			} else {
				iter.Prev()
			}
		}
	}
	if iter.Valid() {
		cur, _ := iter.Deref()
		utils.Assert(CmpOK(cur, cmp, key))
	}
	return iter
}
