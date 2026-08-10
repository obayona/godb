package kvstore

import (
	"slices"
	"testing"

	"github.com/obayona/godb/btree"
	"github.com/obayona/godb/utils"
)

type L struct {
	free  FreeList
	pages map[uint64][]byte // simulate disk pages
	// references
	added   []uint64
	removed []uint64
}

func newL() *L {
	pages := map[uint64][]byte{}
	pages[1] = make([]byte, btree.BTREE_PAGE_SIZE) // initial node
	append := uint64(1000)                         // [1000, 10000)
	return &L{
		free: FreeList{
			get: func(ptr uint64) []byte {
				utils.Assert(pages[ptr] != nil)
				return pages[ptr]
			},
			set: func(ptr uint64) []byte {
				utils.Assert(pages[ptr] != nil)
				return pages[ptr]
			},
			new: func(node []byte) uint64 {
				utils.Assert(pages[append] == nil)
				pages[append] = node
				append++
				return append - 1
			},
			headPage: 1, // initial node
			tailPage: 1,
		},
		pages: pages,
	}
}

// returns the content and the list nodes
func flDump(free *FreeList) (list []uint64, nodes []uint64) {
	ptr := free.headPage
	nodes = append(nodes, ptr)
	for seq := free.headSeq; seq != free.tailSeq; {
		utils.Assert(ptr != 0)
		node := LNode(free.get(ptr))
		item, _ := node.getItem(seq2idx(seq))
		list = append(list, item)
		seq++
		if seq2idx(seq) == 0 {
			ptr = node.getNext()
			nodes = append(nodes, ptr)
		}
	}
	return
}

func (l *L) push(ptr uint64) {
	utils.Assert(l.pages[ptr] == nil)
	l.pages[ptr] = make([]byte, btree.BTREE_PAGE_SIZE)
	l.free.PushTail(ptr)
	l.added = append(l.added, ptr)
}

func (l *L) pop() uint64 {
	ptr := l.free.PopHead()
	if ptr != 0 {
		l.removed = append(l.removed, ptr)
	}
	return ptr
}

func (l *L) verify() {
	l.free.check()

	// dump all pointers from `l.pages`
	appended := []uint64{}
	ptrs := []uint64{}
	for ptr := range l.pages {
		if 1000 <= ptr && ptr < 10000 {
			appended = append(appended, ptr)
		} else if ptr != 1 {
			utils.Assert(slices.Contains(l.added, ptr))
		}
		ptrs = append(ptrs, ptr)
	}
	// dump all pointers from the free list
	list, nodes := flDump(&l.free)

	// any pointer is either in the free list, a list node, or removed.
	utils.Assert(len(l.pages) == len(list)+len(nodes)+len(l.removed))
	combined := slices.Concat(list, nodes, l.removed)
	slices.Sort(combined)
	slices.Sort(ptrs)
	utils.Assert(slices.Equal(combined, ptrs))

	// any pointer is either the initial node, an allocated node, or added
	utils.Assert(len(l.pages) == 1+len(appended)+len(l.added))
	combined = slices.Concat([]uint64{1}, appended, l.added)
	slices.Sort(combined)
	utils.Assert(slices.Equal(combined, ptrs))
}

func TestFreeListEmptyFullEmpty(t *testing.T) {
	for N := 0; N < 2000; N++ {
		l := newL()
		for i := 0; i < N; i++ {
			l.push(10000 + uint64(i))
		}
		l.verify()

		utils.Assert(l.pop() == 0)
		l.free.SetMaxVer(0)
		ptr := l.pop()
		for ptr != 0 {
			l.free.SetMaxVer(0)
			ptr = l.pop()
		}
		l.verify()

		list, nodes := flDump(&l.free)
		utils.Assert(len(list) == 0)
		utils.Assert(len(nodes) == 1)
		// println("N", N)
	}
}

func TestFreeListEmptyFullEmpty2(t *testing.T) {
	for N := 0; N < 2000; N++ {
		l := newL()
		for i := 0; i < N; i++ {
			l.push(10000 + uint64(i))
			l.free.SetMaxVer(0) // allow self-reuse
		}
		l.verify()

		ptr := l.pop()
		for ptr != 0 {
			l.free.SetMaxVer(0)
			ptr = l.pop()
		}
		l.verify()

		list, nodes := flDump(&l.free)
		utils.Assert(len(list) == 0)
		utils.Assert(len(nodes) == 1)
		// println("N", N)
	}
}

func TestFreeListRandom(t *testing.T) {
	for N := 0; N < 1000; N++ {
		l := newL()
		for i := 0; i < 2000; i++ {
			ptr := uint64(10000 + utils.Fmix32(uint32(i)))
			if ptr%2 == 0 {
				l.push(ptr)
			} else {
				l.pop()
			}
		}
		l.verify()
	}
}
