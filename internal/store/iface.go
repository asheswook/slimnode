package store

import "time"

// Store persists file state metadata (SQLite).
// This interface is defined in the store package following the btcd database pattern.
type Store interface {
	GetFile(filename string) (*FileEntry, error)
	ListFiles() ([]FileEntry, error)
	ListByState(state FileState) ([]FileEntry, error)
	UpsertFile(entry *FileEntry) error
	UpdateState(filename string, state FileState) error
	UpdateLastAccess(filename string, t time.Time) error
	ListCachedByLRU(limit int) ([]FileEntry, error)
	DeleteFile(filename string) error
	Close() error
}
