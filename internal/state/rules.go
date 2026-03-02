package state

import "github.com/asheswook/bitcoin-slimnode/internal/store"

// IsWritable returns true only for ACTIVE files.
func IsWritable(s store.FileState) bool {
	return s == store.FileStateActive
}

// IsLocal returns true for files stored on local disk (ACTIVE, LOCAL_FINALIZED).
func IsLocal(s store.FileState) bool {
	return s == store.FileStateActive || s == store.FileStateLocalFinalized
}

// IsEvictable returns true only for CACHED files.
// ACTIVE and LOCAL_FINALIZED must NEVER be evicted.
func IsEvictable(s store.FileState) bool {
	return s == store.FileStateCached
}

// SourceForState returns the FileSource for a given state.
func SourceForState(s store.FileState) store.FileSource {
	if IsLocal(s) {
		return store.FileSourceLocal
	}
	return store.FileSourceServer
}
