package btree

import (
	"encoding/binary"
)

type KV struct {
	Key   []byte
	Value []byte
}

/*
| type | nkeys |  pointers  |  offsets   | key-values | unused |
|  2B  |   2B  | nkeys × 8B | nkeys × 2B |     ...    |        |

| key_size | val_size | key | val |
|    2B    |    2B    | ... | ... |*/

func createTestBNodeRaw(ntype, nkeys uint16, ptrs []uint64, offsets []uint16, kvs []KV) BNode {
	kvsSize := 0

	for _, kv := range kvs[:nkeys] {
		kvsSize += 4 + len(kv.Key) + len(kv.Value)
	}

	data := make([]byte, 4+(int(nkeys)*8)+(int(nkeys)*2)+int(kvsSize))

	binary.LittleEndian.PutUint16(data, ntype)
	binary.LittleEndian.PutUint16(data[2:], nkeys)
	start := 4

	for _, ptr := range ptrs[:nkeys] {
		binary.LittleEndian.PutUint64(data[start:], ptr)
		start += 8
	}

	for _, offset := range offsets[:nkeys] {
		binary.LittleEndian.PutUint16(data[start:], offset)
		start += 2
	}

	for _, kv := range kvs[:nkeys] {
		kvSize := 4 + len(kv.Key) + len(kv.Value)
		buff := make([]byte, kvSize)
		binary.LittleEndian.PutUint16(buff, uint16(len(kv.Key)))
		binary.LittleEndian.PutUint16(buff[2:], uint16(len(kv.Value)))
		copy(buff[4:], kv.Key)
		copy(buff[4+len(kv.Key):], kv.Value)
		copy(data[start:], buff)

		start += kvSize
	}

	return BNode(data)
}

func createTestBNode(ntype uint16, kvs []KV) BNode {
	ptrs := make([]uint64, len(kvs))

	offsets := make([]uint16, len(kvs))
	c := 0
	for i := 0; i < len(kvs); i++ {
		kv := kvs[i]
		kvSize := 4 + len(kv.Key) + len(kv.Value)
		c += kvSize
		offsets[i] = uint16(c)
	}

	return createTestBNodeRaw(ntype, uint16(len(kvs)), ptrs, offsets, kvs)
}
