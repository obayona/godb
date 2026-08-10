package utils

// Assert panics when cond is false. It is intended for internal invariants
// which indicate a programming or storage-corruption bug when violated.
func Assert(cond bool) {
	if !cond {
		panic("assertion failure")
	}
}
