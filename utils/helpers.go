package utils

// SortAdapter adapts three functions to Go's sort.Interface. It is useful
// when parallel slices must be reordered together without defining a new
// collection type for each caller.
type SortAdapter struct {
	Length   int
	LessFunc func(i, j int) bool
	SwapFunc func(i, j int)
}

// Len returns the number of elements in the collection being sorted.
func (s SortAdapter) Len() int { return s.Length }

// Less reports whether the element at i should sort before the element at j.
func (s SortAdapter) Less(i, j int) bool { return s.LessFunc(i, j) }

// Swap exchanges the elements at indexes i and j.
func (s SortAdapter) Swap(i, j int) { s.SwapFunc(i, j) }

// Fmix32 applies the 32-bit MurmurHash3 finalizer. Tests use it to generate a
// deterministic, well-distributed sequence without depending on random seeds.
func Fmix32(h uint32) uint32 {
	h ^= h >> 16
	h *= 0x85ebca6b
	h ^= h >> 13
	h *= 0xc2b2ae35
	h ^= h >> 16
	return h
}
