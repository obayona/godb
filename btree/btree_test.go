package btree

import (
	"encoding/binary"
	"slices"
	"strings"
	"testing"
)

func TestBType(t *testing.T) {
	// create a byte slice representing node
	// | 1  | ... | type node
	// | 2B | ....|
	buff := make([]byte, 2)
	binary.LittleEndian.PutUint16(buff, BNODE_NODE)
	bNode := BNode(buff)

	if size := bNode.btype(); size != BNODE_NODE {
		t.Errorf("Expected: %v; got: %v", BNODE_NODE, size)
	}

	// create a byte slice representing node
	// | 2  | ... | type node
	binary.LittleEndian.PutUint16(buff, BNODE_LEAF)
	bNode = BNode(buff)

	if size := bNode.btype(); size != BNODE_LEAF {
		t.Errorf("Expected: %v; got: %v", BNODE_LEAF, size)
	}
}

func TestNKeys(t *testing.T) {
	// create a byte slice representing node with 6 keys
	// | .. | 6  | ....
	// | 2B | 2B | ...
	buff := make([]byte, 4)
	binary.LittleEndian.PutUint16(buff[2:], 6)
	bNode := BNode(buff)

	if nkeys := bNode.nkeys(); nkeys != 6 {
		t.Errorf("Expected: %v; got: %v", 6, nkeys)
	}
}

func TestGetPtr(t *testing.T) {
	// create a byte slice representing node
	// | 1 | 3 | (pointers) 4 | 13 | 15 | ...

	ptrs := []uint64{4, 13, 15}
	bNode := createTestBNodeRaw(BNODE_LEAF, 3, ptrs, make([]uint16, 3), make([]KV, 3))

	tests := []struct {
		Name     string
		Index    uint16
		Expected uint64
	}{
		{"Ptr index 0", 0, 4},
		{"Ptr index 1", 1, 13},
		{"Ptr index 2", 2, 15},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			if got := bNode.getPtr(tt.Index); got != tt.Expected {
				t.Errorf("Expected: %v; got: %v", tt.Expected, got)
			}
		})
	}
}

func TestSetPtr(t *testing.T) {
	// create a byte slice representing node
	// | 1 | 3 | (pointers) 4 | 13 | 15 | ...
	ptrs := []uint64{4, 13, 15}
	bNode := createTestBNodeRaw(BNODE_LEAF, 3, ptrs, make([]uint16, 3), make([]KV, 3))

	newPtrs := []uint64{10, 12, 18}
	for idx, ptr := range newPtrs {
		bNode.setPtr(uint16(idx), ptr)
		if got := bNode.getPtr(uint16(idx)); got != ptr {
			t.Errorf("Expected: %v; got: %v", ptr, got)
		}
	}
}

func TestGetOffset(t *testing.T) {
	// create a byte slice representing node
	// | 1 | 3 | .. | .. | .. | 4 | 8 | 12 | ......
	offsets := []uint16{4, 8, 12}
	bNode := createTestBNodeRaw(BNODE_LEAF, 3, make([]uint64, 3), offsets, make([]KV, 3))

	tests := []struct {
		Name     string
		Index    uint16
		Expected uint16
	}{
		{"Offset index 0", 0, 0},
		{"Offset index 1", 1, 4},
		{"Offset index 2", 2, 8},
		{"Offset index 2", 3, 12},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			if got := bNode.getOffset(tt.Index); got != tt.Expected {
				t.Errorf("Expected: %v; got: %v", tt.Expected, got)
			}
		})
	}
}

func TestSetOffset(t *testing.T) {
	// create a byte slice representing node
	// | 1 | 3 | .. | .. | .. | 4 | 8 | 12 | ......
	offsets := []uint16{4, 8, 12}
	bNode := createTestBNodeRaw(BNODE_LEAF, 3, make([]uint64, 3), offsets, make([]KV, 3))

	newOffsets := []uint16{0, 7, 13, 17}

	for idx, offset := range newOffsets {
		bNode.setOffset(uint16(idx), offset)
		if got := bNode.getOffset((uint16(idx))); got != offset {
			t.Errorf("Expected: %v; got: %v", offset, got)
		}
	}
}

func TestKvPos(t *testing.T) {
	// create a byte slice representing node
	// | 1 | 3 | .. | .. | .. | 4 | 8 | 12 | ...
	offsets := []uint16{4, 8, 12}
	bNode := createTestBNodeRaw(BNODE_LEAF, 3, make([]uint64, 3), offsets, make([]KV, 3))

	tests := []struct {
		Name     string
		Index    uint16
		Expected uint16
	}{
		{"Ptr index 0", 0, 34},
		{"Ptr index 1", 1, 38},
		{"Ptr index 2", 2, 42},
		{"Ptr index 2", 3, 46},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			if got := bNode.kvPos(tt.Index); got != tt.Expected {
				t.Errorf("Expected: %v; got: %v", tt.Expected, got)
			}
		})
	}
}

func TestGetKey(t *testing.T) {
	kvs := []KV{
		{Key: []byte("abc"), Value: []byte("cd")}, {Key: []byte("vw"), Value: []byte("yz")},
	}
	bNode := createTestBNode(BNODE_LEAF, kvs)

	tests := []struct {
		Name     string
		Index    uint16
		Expected string
	}{
		{"Key index 0", 0, "abc"},
		{"Key index 1", 1, "vw"},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			got := bNode.getKey(tt.Index)
			result := string(got)

			if result != tt.Expected {
				t.Errorf("Expected: %v; got: %v", tt.Expected, result)
			}
		})
	}
}

func TestGetVal(t *testing.T) {
	kvs := []KV{
		{Key: []byte("ab"), Value: []byte("cde")}, {Key: []byte("vw"), Value: []byte("yz")},
	}
	bNode := createTestBNode(BNODE_LEAF, kvs)

	tests := []struct {
		Name     string
		Index    uint16
		Expected string
	}{
		{"Key index 0", 0, "cde"},
		{"Key index 1", 1, "yz"},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			got := bNode.getVal(tt.Index)
			result := string(got)
			if result != tt.Expected {
				t.Errorf("Expected: %v; got: %v", tt.Expected, result)
			}
		})
	}
}

func TestNodeAppendKV(t *testing.T) {
	kvs := []KV{
		{Key: []byte("ab"), Value: []byte("cde")}, {Key: []byte("vw"), Value: []byte("yz")},
	}

	bNode := createTestBNode(BNODE_LEAF, kvs)

	tests := []struct {
		Name       string
		Index      uint16
		Ptr        uint64
		Key        []byte
		Val        []byte
		NextOffset uint16
	}{
		{Name: "Index 0", Index: 0, Ptr: 2, Key: []byte("qw"), Val: []byte("tyu"), NextOffset: 9},
		{Name: "Index 1", Index: 1, Ptr: 4, Key: []byte("12"), Val: []byte("34"), NextOffset: 17},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			nodeAppendKV(bNode, tt.Index, tt.Ptr, tt.Key, tt.Val)
			if got := bNode.getPtr(tt.Index); got != tt.Ptr {
				t.Errorf("Expected ptr: %v; got: %v", tt.Ptr, got)
			}
			if got := bNode.getKey(tt.Index); slices.Compare(got, tt.Key) != 0 {
				t.Errorf("Expected key: %v; got: %v", tt.Key, got)
			}
			if got := bNode.getVal(tt.Index); slices.Compare(got, tt.Val) != 0 {
				t.Errorf("Expected value: %v; got: %v", tt.Val, got)
			}
			if got := bNode.getOffset(tt.Index + 1); got != tt.NextOffset {
				t.Errorf("Expected next offset: %v; got: %v", tt.NextOffset, got)
			}
		})
	}
}

func TestNBytes(t *testing.T) {
	kvs := []KV{
		{Key: []byte("ab"), Value: []byte("cde")}, {Key: []byte("vw"), Value: []byte("yz")},
	}
	bNode := createTestBNode(BNODE_LEAF, kvs)

	if got := bNode.nbytes(); got != 41 {
		t.Errorf("Expected: %v; got: %v", 41, got)
	}
}

func TestNodeAppendRange(t *testing.T) {
	kvs := []KV{
		{Key: []byte("ab"), Value: []byte("cde")},
		{Key: []byte("vw"), Value: []byte("yz")},
		{Key: []byte("123"), Value: []byte("456")},
		{Key: []byte("78"), Value: []byte("00009")},
	}
	bNode := createTestBNode(BNODE_LEAF, kvs)
	bNode.setPtr(0, 1)
	bNode.setPtr(1, 6)
	bNode.setPtr(2, 7)
	bNode.setPtr(3, 32)

	bNodeNew := BNode(make([]byte, len(bNode))) // zero node
	bNodeNew.setHeader(bNode.btype(), bNode.nkeys())

	nodeAppendRange(bNodeNew, bNode, 0, 1, 3) // copy from key "vw" to key "78" on new node on idx zero

	tests := []struct {
		Name  string
		Index uint16
		Ptr   uint64
		Key   []byte
		Val   []byte
	}{
		{Name: "Index 0", Index: 0, Ptr: 6, Key: []byte("vw"), Val: []byte("yz")},
		{Name: "Index 1", Index: 1, Ptr: 7, Key: []byte("123"), Val: []byte("456")},
		{Name: "Index 2", Index: 2, Ptr: 32, Key: []byte("78"), Val: []byte("00009")},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			if got := bNodeNew.getPtr(tt.Index); got != tt.Ptr {
				t.Errorf("Expected ptr: %v; got: %v", tt.Ptr, got)
			}
			if got := bNodeNew.getKey(tt.Index); slices.Compare(got, tt.Key) != 0 {
				t.Errorf("Expected key: %v; got: %v", tt.Key, got)
			}
			if got := bNodeNew.getVal(tt.Index); slices.Compare(got, tt.Val) != 0 {
				t.Errorf("Expected value: %v; got: %v", tt.Val, got)
			}
		})
	}

}

func TestLeafInsert(t *testing.T) {
	kvs := []KV{
		{Key: []byte("ab"), Value: []byte("cde")},
		{Key: []byte("vw"), Value: []byte("yz")},
		{Key: []byte("123"), Value: []byte("456")},
	}
	bNode := createTestBNode(BNODE_LEAF, kvs)
	bNode.setPtr(0, 1)
	bNode.setPtr(1, 6)
	bNode.setPtr(2, 7)

	bNodeNew := BNode(make([]byte, len(bNode)+20)) // zero node with extra space

	leafInsert(bNodeNew, bNode, 1, []byte("cd"), []byte("ef"))

	tests := []struct {
		Name  string
		Index uint16
		Ptr   uint64
		Key   []byte
		Val   []byte
	}{
		{Name: "Index 0", Index: 0, Ptr: 1, Key: []byte("ab"), Val: []byte("cde")},
		{Name: "Index 1", Index: 1, Ptr: 0, Key: []byte("cd"), Val: []byte("ef")}, // new kv
		{Name: "Index 2", Index: 2, Ptr: 6, Key: []byte("vw"), Val: []byte("yz")},
		{Name: "Index 3", Index: 3, Ptr: 7, Key: []byte("123"), Val: []byte("456")},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			if got := bNodeNew.getPtr(tt.Index); got != tt.Ptr {
				t.Errorf("Expected ptr: %v; got: %v", tt.Ptr, got)
			}
			if got := bNodeNew.getKey(tt.Index); slices.Compare(got, tt.Key) != 0 {
				t.Errorf("Expected key: %v; got: %v", tt.Key, got)
			}
			if got := bNodeNew.getVal(tt.Index); slices.Compare(got, tt.Val) != 0 {
				t.Errorf("Expected value: %v; got: %v", tt.Val, got)
			}
		})
	}

}

func TestLeafUpdate(t *testing.T) {
	kvs := []KV{
		{Key: []byte("ab"), Value: []byte("cde")},
		{Key: []byte("vw"), Value: []byte("yz")},
		{Key: []byte("123"), Value: []byte("456")},
	}
	bNode := createTestBNode(BNODE_LEAF, kvs)
	bNode.setPtr(0, 1)
	bNode.setPtr(1, 6)
	bNode.setPtr(2, 7)

	bNodeNew := BNode(make([]byte, len(bNode)+20)) // zero node with extra space

	leafUpdate(bNodeNew, bNode, 1, []byte("cd"), []byte("efx"))

	tests := []struct {
		Name  string
		Index uint16
		Ptr   uint64
		Key   []byte
		Val   []byte
	}{
		{Name: "Index 0", Index: 0, Ptr: 1, Key: []byte("ab"), Val: []byte("cde")},
		{Name: "Index 1", Index: 1, Ptr: 0, Key: []byte("cd"), Val: []byte("efx")}, // updated kv
		{Name: "Index 2", Index: 2, Ptr: 7, Key: []byte("123"), Val: []byte("456")},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			if got := bNodeNew.getPtr(tt.Index); got != tt.Ptr {
				t.Errorf("Expected ptr: %v; got: %v", tt.Ptr, got)
			}
			if got := bNodeNew.getKey(tt.Index); slices.Compare(got, tt.Key) != 0 {
				t.Errorf("Expected key: %v; got: %v", tt.Key, got)
			}
			if got := bNodeNew.getVal(tt.Index); slices.Compare(got, tt.Val) != 0 {
				t.Errorf("Expected value: %v; got: %v", tt.Val, got)
			}
		})
	}

}

func TestNodeLookupLE(t *testing.T) {
	kvs := []KV{
		{Key: []byte{7, 0}, Value: []byte("abc")},
		{Key: []byte{13, 0}, Value: []byte("yz")},
		{Key: []byte{25, 0}, Value: []byte("we")},
	}

	bNode := createTestBNode(BNODE_LEAF, kvs)

	tests := []struct {
		Name     string
		Key      []byte
		Expected uint16
	}{
		{Name: "Key 6", Key: []byte{6, 0}, Expected: 65535},
		{Name: "Key 7", Key: []byte{7, 0}, Expected: 0},
		{Name: "Key 10", Key: []byte{10, 0}, Expected: 0},
		{Name: "Key 13", Key: []byte{13, 0}, Expected: 1},
		{Name: "Key 14", Key: []byte{14, 0}, Expected: 1},
		{Name: "Key 25", Key: []byte{25, 0}, Expected: 2},
		{Name: "Key 26", Key: []byte{26, 0}, Expected: 2},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			idx := nodeLookupLE(bNode, tt.Key)
			if idx != tt.Expected {
				t.Errorf("Expected ptr: %v; got: %v", tt.Expected, idx)
			}
		})
	}
}

func TestNodeSplit2(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow test in short mode")
	}

	// create oversized node
	kvs := []KV{
		{[]byte("key1"), []byte(strings.Repeat("a", 1000))},
		{[]byte("key2"), []byte(strings.Repeat("b", 1000))},
		{[]byte("key3"), []byte(strings.Repeat("b", 1000))},
		{[]byte("key4"), []byte(strings.Repeat("b", 1000))},
	}

	bNode := createTestBNode(BNODE_LEAF, kvs)
	left := BNode(make([]byte, BTREE_PAGE_SIZE))
	right := BNode(make([]byte, BTREE_PAGE_SIZE))

	nodeSplit2(left, right, bNode)

	if left.nkeys() != 2 {
		t.Errorf("Left node should have 2 keys")
	}

	k1, k2 := string(left.getKey(0)), string(left.getKey(1))
	if k1 != "key1" || k2 != "key2" {
		t.Errorf("Unexpected keys: %v, %v for left node", k1, k2)
	}

	if right.nkeys() != 2 {
		t.Errorf("Right node should have 2 keys")
	}

	k1, k2 = string(right.getKey(0)), string(right.getKey(1))
	if k1 != "key3" || k2 != "key4" {
		t.Errorf("Unexpected keys: %v, %v for left node", k1, k2)
	}
}
