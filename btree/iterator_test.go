package btree

import (
	"fmt"
	"testing"

	"github.com/obayona/godb/utils"
	is "github.com/stretchr/testify/require"
)

func TestBTreeIter(t *testing.T) {
	{
		c := newC()
		iter := c.tree.SeekLE(nil)
		is.False(t, iter.Valid())
	}

	sizes := []int{5, 2500}
	for _, sz := range sizes {
		c := newC()

		for i := 0; i < sz; i++ {
			key := fmt.Sprintf("key%010d", i)
			val := fmt.Sprintf("vvv%d", utils.Fmix32(uint32(-i)))
			c.add(key, val)
		}
		c.verify(t)

		prevk, prevv := []byte(nil), []byte(nil)
		for i := 0; i < sz; i++ {
			key := []byte(fmt.Sprintf("key%010d", i))
			val := []byte(fmt.Sprintf("vvv%d", utils.Fmix32(uint32(-i))))
			// fmt.Println(i, string(key), val)

			iter := c.tree.SeekLE(key)
			is.True(t, iter.Valid())
			gotk, gotv := iter.Deref()
			is.Equal(t, key, gotk)
			is.Equal(t, val, gotv)

			iter.Prev()
			if i > 0 {
				is.True(t, iter.Valid())
				gotk, gotv := iter.Deref()
				is.Equal(t, prevk, gotk)
				is.Equal(t, prevv, gotv)
			} else {
				is.False(t, iter.Valid())
			}

			iter.Next()
			{
				is.True(t, iter.Valid())
				gotk, gotv := iter.Deref()
				is.Equal(t, key, gotk)
				is.Equal(t, val, gotv)
			}

			if i+1 == sz {
				iter.Next()
				is.False(t, iter.Valid())
			}

			prevk, prevv = key, val
		}
	}
}

func TestBTreeIteratorCrossesLeafLinks(t *testing.T) {
	c := newC()
	for i := 0; i < 2500; i++ {
		c.add(fmt.Sprintf("key%010d", i), fmt.Sprintf("value-%d", i))
	}

	ptr := c.tree.Root
	leaf := BNode(c.tree.GetPage(ptr))
	for leaf.BType() == BNODE_NODE {
		ptr = leaf.GetPtr(0)
		leaf = BNode(c.tree.GetPage(ptr))
	}
	is.NotZero(t, leaf.NextLeaf())
	next := BNode(c.tree.GetPage(leaf.NextLeaf()))

	lastKey := append([]byte(nil), leaf.GetKey(leaf.NKeys()-1)...)
	wantNext := append([]byte(nil), next.GetKey(0)...)
	iter := c.tree.SeekLE(lastKey)
	for i := 0; i+1 < len(iter.pos); i++ {
		iter.pos[i] = ^uint16(0) // crossing must not consult the parent path
	}
	iter.Next()
	got, _ := iter.Deref()
	is.Equal(t, wantNext, got)

	iter.Prev()
	got, _ = iter.Deref()
	is.Equal(t, lastKey, got)
}
