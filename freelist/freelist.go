package freelist

import (
	"encoding/binary"

	"github.com/obayona/godb/btree"
	"github.com/obayona/godb/utils"
)

// node format:
// | next | pointer + version | unused |
// |  8B  |     n*(8B+8B)     |   ...  |
type LNode []byte

const FREE_LIST_HEADER = 8
const FREE_LIST_CAP = (btree.BTREE_PAGE_SIZE - FREE_LIST_HEADER) / 16

// getters & setters
func (node LNode) getNext() uint64 {
	return binary.LittleEndian.Uint64(node[0:8])
}
func (node LNode) setNext(next uint64) {
	binary.LittleEndian.PutUint64(node[0:8], next)
}
func (node LNode) getItem(idx int) (uint64, uint64) {
	offset := FREE_LIST_HEADER + 16*idx
	return binary.LittleEndian.Uint64(node[offset:]),
		binary.LittleEndian.Uint64(node[offset+8:])
}
func (node LNode) setItem(idx int, ptr uint64, version uint64) {
	utils.Assert(idx < FREE_LIST_CAP)
	offset := FREE_LIST_HEADER + 16*idx
	binary.LittleEndian.PutUint64(node[offset+0:], ptr)
	binary.LittleEndian.PutUint64(node[offset+8:], version)
}

// PageIO supplies the page operations required by a FreeList. New must append
// a page, while Set must return writable bytes for an existing page.
type PageIO struct {
	Get func(uint64) []byte
	New func([]byte) uint64
	Set func(uint64) []byte
}

// State is the free-list metadata persisted by the owning storage engine.
type State struct {
	HeadPage uint64
	HeadSeq  uint64
	TailPage uint64
	TailSeq  uint64
}

// FreeList stores reusable page pointers in an on-disk, version-aware queue.
// Its State must be persisted atomically with the storage engine's root page.
type FreeList struct {
	// callbacks for managing on-disk pages
	get func(uint64) []byte // read a page
	new func([]byte) uint64 // append a new page
	set func(uint64) []byte // update an existing page
	// persisted data in the meta page
	headPage uint64 // pointer to the list head node
	headSeq  uint64 // monotonic sequence number to index into the list head
	tailPage uint64
	tailSeq  uint64
	// in-memory states
	maxSeq uint64 // saved `tailSeq` to prevent consuming newly added items
	maxVer uint64 // the oldest reader version
	curVer uint64 // version number when committing
}

// New creates a free list backed by pages and restores its persisted state.
func New(pages PageIO, state State) *FreeList {
	fl := &FreeList{}
	fl.Configure(pages)
	fl.Restore(state)
	return fl
}

// Configure sets the page operations used by the free list.
func (fl *FreeList) Configure(pages PageIO) {
	utils.Assert(pages.Get != nil && pages.New != nil && pages.Set != nil)
	fl.get = pages.Get
	fl.new = pages.New
	fl.set = pages.Set
}

// Restore replaces the persisted queue position.
func (fl *FreeList) Restore(state State) {
	fl.headPage = state.HeadPage
	fl.headSeq = state.HeadSeq
	fl.tailPage = state.TailPage
	fl.tailSeq = state.TailSeq
}

// State returns the metadata that the owning storage engine must persist.
func (fl *FreeList) State() State {
	return State{
		HeadPage: fl.headPage,
		HeadSeq:  fl.headSeq,
		TailPage: fl.tailPage,
		TailSeq:  fl.tailSeq,
	}
}

// PageSets returns the queued reusable pages and the metadata pages that hold
// the queue. The returned slices are snapshots intended for validation and
// diagnostics; modifying them does not change the free list.
func (fl *FreeList) PageSets() (reusable []uint64, metadata []uint64) {
	ptr := fl.headPage
	metadata = append(metadata, ptr)
	for seq := fl.headSeq; seq != fl.tailSeq; {
		utils.Assert(ptr != 0)
		node := LNode(fl.get(ptr))
		item, _ := node.getItem(seq2idx(seq))
		reusable = append(reusable, item)
		seq++
		if seq2idx(seq) == 0 {
			ptr = node.getNext()
			metadata = append(metadata, ptr)
		}
	}
	return reusable, metadata
}

// SetCurrentVersion assigns the version recorded on subsequently freed pages.
func (fl *FreeList) SetCurrentVersion(version uint64) {
	fl.curVer = version
}

func seq2idx(seq uint64) int {
	return int(seq % FREE_LIST_CAP)
}

// versionBefore compares wrapping unsigned transaction versions.
func versionBefore(a, b uint64) bool {
	return a-b > 1<<63
}

func (fl *FreeList) check() {
	utils.Assert(fl.headPage != 0 && fl.tailPage != 0)
	utils.Assert(fl.headSeq != fl.tailSeq || fl.headPage == fl.tailPage)
}

// PopHead returns one reusable page pointer, or zero when none is safe to use.
func (fl *FreeList) PopHead() uint64 {
	ptr, head := flPop(fl)
	if head != 0 { // the empty head node is recycled
		fl.PushTail(head)
	}
	return ptr
}

// remove 1 item from the head node, and remove the head node if empty.
func flPop(fl *FreeList) (ptr uint64, head uint64) {
	fl.check()
	if fl.headSeq == fl.maxSeq {
		return 0, 0 // cannot advance; empty list or the current version
	}
	node := LNode(fl.get(fl.headPage))
	ptr, version := node.getItem(seq2idx(fl.headSeq))
	if versionBefore(fl.maxVer, version) {
		return 0, 0 // cannot advance; still in-use
	}
	fl.headSeq++
	// move to the next one if the head node is empty
	if seq2idx(fl.headSeq) == 0 {
		head, fl.headPage = fl.headPage, node.getNext()
		utils.Assert(fl.headPage != 0)
	}
	return
}

// PushTail records a page pointer for reuse by a future safe version.
func (fl *FreeList) PushTail(ptr uint64) {
	fl.check()
	// add it to the tail node
	LNode(fl.set(fl.tailPage)).setItem(seq2idx(fl.tailSeq), ptr, fl.curVer)
	fl.tailSeq++
	// add a new tail node if it's full (the list is never empty)
	if seq2idx(fl.tailSeq) == 0 {
		// try to reuse from the list head
		next, head := flPop(fl) // may remove the head node
		if next == 0 {
			// or allocate a new node by appending
			next = fl.new(make([]byte, btree.BTREE_PAGE_SIZE))
		}
		// link to the new tail node
		LNode(fl.set(fl.tailPage)).setNext(next)
		fl.tailPage = next
		// also add the head node if it's removed
		if head != 0 {
			LNode(fl.set(fl.tailPage)).setItem(0, head, fl.curVer)
			fl.tailSeq++
		}
	}
}

// SetMaxVer makes newly added items visible and sets the oldest reader version.
func (fl *FreeList) SetMaxVer(maxVer uint64) {
	fl.maxSeq = fl.tailSeq
	fl.maxVer = maxVer
}
