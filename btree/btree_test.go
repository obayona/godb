package btree

import (
	"fmt"
	"math/rand"
	"sort"
	"testing"
	"unsafe"

	"github.com/obayona/godb/utils"
	is "github.com/stretchr/testify/require"
)

type C struct {
	tree  BTree
	ref   map[string]string
	pages map[uint64]BNode
}

func newC() *C {
	pages := map[uint64]BNode{}
	return &C{
		tree: BTree{
			GetPage: func(ptr uint64) []byte {
				node, ok := pages[ptr]
				utils.Assert(ok)
				return node
			},
			NewPage: func(node []byte) uint64 {
				utils.Assert(BNode(node).NBytes() <= BTREE_PAGE_SIZE)
				ptr := uint64(uintptr(unsafe.Pointer(&node[0])))
				utils.Assert(pages[ptr] == nil)
				pages[ptr] = node
				return ptr
			},
			DelPage: func(ptr uint64) {
				utils.Assert(pages[ptr] != nil)
				delete(pages, ptr)
			},
		},
		ref:   map[string]string{},
		pages: pages,
	}
}

func (c *C) add(key string, val string) {
	_, err := c.tree.Upsert([]byte(key), []byte(val))
	utils.Assert(err == nil)
	c.ref[key] = val
}

func (c *C) del(key string) bool {
	delete(c.ref, key)
	deleted, err := c.tree.Delete(&DeleteReq{Key: []byte(key)})
	utils.Assert(err == nil)
	return deleted
}

func (c *C) dump() ([]string, []string) {
	keys := []string{}
	vals := []string{}

	var nodeDump func(uint64)
	nodeDump = func(ptr uint64) {
		node := BNode(c.tree.GetPage(ptr))
		nkeys := node.NKeys()
		if node.BType() == BNODE_LEAF {
			for i := uint16(0); i < nkeys; i++ {
				keys = append(keys, string(node.GetKey(i)))
				vals = append(vals, string(node.GetVal(i)))
			}
		} else {
			for i := uint16(0); i < nkeys; i++ {
				ptr := node.GetPtr(i)
				nodeDump(ptr)
			}
		}
	}

	nodeDump(c.tree.Root)
	utils.Assert(keys[0] == "")
	utils.Assert(vals[0] == "")
	return keys[1:], vals[1:]
}

func (c *C) verify(t *testing.T) {
	keys, vals := c.dump()

	rkeys, rvals := []string{}, []string{}
	for k, v := range c.ref {
		rkeys = append(rkeys, k)
		rvals = append(rvals, v)
	}
	is.Equal(t, len(rkeys), len(keys))
	sort.Stable(utils.SortAdapter{
		Length:   len(rkeys),
		LessFunc: func(i, j int) bool { return rkeys[i] < rkeys[j] },
		SwapFunc: func(i, j int) {
			k, v := rkeys[i], rvals[i]
			rkeys[i], rvals[i] = rkeys[j], rvals[j]
			rkeys[j], rvals[j] = k, v
		},
	})

	is.Equal(t, rkeys, keys)
	is.Equal(t, rvals, vals)

	var nodeVerify func(BNode)
	nodeVerify = func(node BNode) {
		nkeys := node.NKeys()
		utils.Assert(nkeys >= 1)
		if node.BType() == BNODE_LEAF {
			return
		}
		for i := uint16(0); i < nkeys; i++ {
			key := node.GetKey(i)
			kid := BNode(c.tree.GetPage(node.GetPtr(i)))
			is.Equal(t, key, kid.GetKey(0))
			nodeVerify(kid)
		}
	}

	nodeVerify(c.tree.GetPage(c.tree.Root))
}

func commonTestBasic(t *testing.T, hasher func(uint32) uint32) {
	c := newC()
	c.add("k", "v")
	c.verify(t)

	// insert
	for i := 0; i < 250000; i++ {
		key := fmt.Sprintf("key%d", hasher(uint32(i)))
		val := fmt.Sprintf("vvv%d", hasher(uint32(-i)))
		c.add(key, val)
		if i < 2000 {
			c.verify(t)
		}
	}
	c.verify(t)

	// del
	for i := 2000; i < 250000; i++ {
		key := fmt.Sprintf("key%d", hasher(uint32(i)))
		is.True(t, c.del(key))
	}
	c.verify(t)

	// overwrite
	for i := 0; i < 2000; i++ {
		key := fmt.Sprintf("key%d", hasher(uint32(i)))
		val := fmt.Sprintf("vvv%d", hasher(uint32(+i)))
		c.add(key, val)
		c.verify(t)
	}

	is.False(t, c.del("kk"))

	for i := 0; i < 2000; i++ {
		key := fmt.Sprintf("key%d", hasher(uint32(i)))
		is.True(t, c.del(key))
		c.verify(t)
	}

	c.add("k", "v2")
	c.verify(t)
	c.del("k")
	c.verify(t)

	// the dummy empty key
	is.Equal(t, 1, len(c.pages))
	is.Equal(t, uint16(1), BNode(c.tree.GetPage(c.tree.Root)).NKeys())
}

func TestBTreeBasicAscending(t *testing.T) {
	commonTestBasic(t, func(h uint32) uint32 { return +h })
}

func TestBTreeBasicDescending(t *testing.T) {
	commonTestBasic(t, func(h uint32) uint32 { return -h })
}

func TestBTreeBasicRand(t *testing.T) {
	commonTestBasic(t, utils.Fmix32)
}

func TestBTreeRandLength(t *testing.T) {
	c := newC()
	for i := 0; i < 2000; i++ {
		klen := utils.Fmix32(uint32(2*i+0)) % BTREE_MAX_KEY_SIZE
		vlen := utils.Fmix32(uint32(2*i+1)) % BTREE_MAX_VAL_SIZE
		if klen == 0 {
			continue
		}

		key := make([]byte, klen)
		rand.Read(key)
		val := make([]byte, vlen)
		// rand.Read(val)
		c.add(string(key), string(val))
		c.verify(t)
	}
}

func TestBTreeIncLength(t *testing.T) {
	for l := 1; l < BTREE_MAX_KEY_SIZE+BTREE_MAX_VAL_SIZE; l++ {
		c := newC()

		klen := l
		if klen > BTREE_MAX_KEY_SIZE {
			klen = BTREE_MAX_KEY_SIZE
		}
		vlen := l - klen
		key := make([]byte, klen)
		val := make([]byte, vlen)

		factor := BTREE_PAGE_SIZE / l
		size := factor * factor * 2
		if size > 4000 {
			size = 4000
		}
		if size < 10 {
			size = 10
		}
		for i := 0; i < size; i++ {
			rand.Read(key)
			c.add(string(key), string(val))
		}
		c.verify(t)
	}
}
