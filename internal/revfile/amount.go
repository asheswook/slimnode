package revfile

// CompressAmount compresses a satoshi amount using Bitcoin Core's encoding.
// 0 -> 0. Strips trailing decimal zeros up to 9 places.
func CompressAmount(n uint64) uint64 {
	if n == 0 {
		return 0
	}
	e := 0
	for n%10 == 0 && e < 9 {
		n /= 10
		e++
	}
	if e < 9 {
		d := n % 10
		// d must be 1-9
		n /= 10
		return 1 + (n*9+d-1)*10 + uint64(e)
	}
	// e == 9
	return 1 + (n-1)*10 + 9
}

// DecompressAmount reverses CompressAmount.
func DecompressAmount(x uint64) uint64 {
	if x == 0 {
		return 0
	}
	x--
	e := x % 10
	x /= 10
	var n uint64
	if e < 9 {
		d := x%9 + 1
		x /= 9
		n = x*10 + d
	} else {
		n = x + 1
	}
	for ; e > 0; e-- {
		n *= 10
	}
	return n
}
