package utils

func Assert(value bool, stm string) {
	if !value {
		panic(stm)
	}
}
