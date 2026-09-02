package sample

func preserve(x uint64) uint64 {
	var pointer *uint64 = &x
	_ = pointer
	{
		x := uint32(3)
		_ = x
	}
	return x + 9007199254740993
}
