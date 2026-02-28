package fusefs

import (
	"hash/fnv"
	"strconv"
	"strings"
)

// InodeForFile returns a stable inode number for a given filename.
//
// blk files: 1_000_000 + file number (e.g. blk00100.dat → 1_000_100)
// rev files: 2_000_000 + file number (e.g. rev00100.dat → 2_000_100)
// xor.dat:   3
// .lock:     4
// unknown:   hash-based fallback
func InodeForFile(name string) uint64 {
	switch name {
	case "xor.dat":
		return 3
	case ".lock":
		return 4
	}

	if strings.HasPrefix(name, "blk") && strings.HasSuffix(name, ".dat") {
		numStr := strings.TrimPrefix(name, "blk")
		numStr = strings.TrimSuffix(numStr, ".dat")
		if n, err := strconv.ParseUint(numStr, 10, 64); err == nil {
			return 1_000_000 + n
		}
	}

	if strings.HasPrefix(name, "rev") && strings.HasSuffix(name, ".dat") {
		numStr := strings.TrimPrefix(name, "rev")
		numStr = strings.TrimSuffix(numStr, ".dat")
		if n, err := strconv.ParseUint(numStr, 10, 64); err == nil {
			return 2_000_000 + n
		}
	}

	// Hash-based fallback for unknown filenames.
	h := fnv.New64a()
	h.Write([]byte(name))
	return 3_000_000 + h.Sum64()%1_000_000
}
