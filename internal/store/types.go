package store

import "time"

// FileState represents the state of a block file in the state machine.
type FileState string

const (
	FileStateActive         FileState = "ACTIVE"
	FileStateLocalFinalized FileState = "LOCAL_FINALIZED"
	FileStateCached         FileState = "CACHED"
	FileStateRemote         FileState = "REMOTE"
)

// FileSource indicates where a file originated.
type FileSource string

const (
	FileSourceServer FileSource = "server"
	FileSourceLocal  FileSource = "local"
)

// FileEntry represents a file's metadata in the store.
type FileEntry struct {
	Filename    string
	State       FileState
	Source      FileSource
	Size        int64
	SHA256      string // empty for ACTIVE files
	CreatedAt   time.Time
	LastAccess  time.Time
	HeightFirst int64
	HeightLast  int64
}

// MaxBlockFileSize is the Bitcoin Core MAX_BLOCKFILE_SIZE (128 MiB).
const MaxBlockFileSize = 128 * 1024 * 1024

// FinalizedFileThreshold is the minimum size to consider a block file finalized
// when scanning on disk. Bitcoin Core switches to a new file when
// currentSize + blockSize + 8 > MAX_BLOCKFILE_SIZE, so finalized files are always
// slightly under 128 MiB. The gap is at most ~4 MiB (max block size).
const FinalizedFileThreshold = MaxBlockFileSize - 4*1024*1024
