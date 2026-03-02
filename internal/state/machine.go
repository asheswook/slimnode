package state

import (
	"github.com/asheswook/bitcoin-slimnode/internal/store"
)

// Event represents a state transition trigger.
type Event string

const (
	// EventFinalize transitions ACTIVE → LOCAL_FINALIZED when file reaches 128 MiB.
	EventFinalize Event = "finalize"
	// EventCacheFetch transitions REMOTE → CACHED when fetched from server.
	EventCacheFetch Event = "cache_fetch"
	// EventCacheEvict transitions CACHED → REMOTE on LRU eviction.
	EventCacheEvict Event = "cache_evict"
	// EventCompact transitions LOCAL_FINALIZED → REMOTE after compaction.
	EventCompact Event = "compact"
)

// Transition validates and applies a state transition.
// Returns the new state or ErrInvalidTransition for forbidden transitions.
func Transition(current store.FileState, event Event) (store.FileState, error) {
	switch current {
	case store.FileStateActive:
		if event == EventFinalize {
			return store.FileStateLocalFinalized, nil
		}
	case store.FileStateRemote:
		if event == EventCacheFetch {
			return store.FileStateCached, nil
		}
	case store.FileStateCached:
		if event == EventCacheEvict {
			return store.FileStateRemote, nil
		}
	case store.FileStateLocalFinalized:
		if event == EventCompact {
			return store.FileStateRemote, nil
		}
	}
	return current, ErrInvalidTransition
}
