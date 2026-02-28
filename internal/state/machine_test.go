package state

import (
	"errors"
	"testing"

	"github.com/asheswook/bitcoin-lfn/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidTransitions(t *testing.T) {
	tests := []struct {
		current store.FileState
		event   Event
		want    store.FileState
	}{
		{store.FileStateActive, EventFinalize, store.FileStateLocalFinalized},
		{store.FileStateRemote, EventCacheFetch, store.FileStateCached},
		{store.FileStateCached, EventCacheEvict, store.FileStateRemote},
		{store.FileStateLocalFinalized, EventCompact, store.FileStateRemote},
	}
	for _, tt := range tests {
		got, err := Transition(tt.current, tt.event)
		require.NoError(t, err, "%s + %s", tt.current, tt.event)
		assert.Equal(t, tt.want, got)
	}
}

func TestInvalidTransitions(t *testing.T) {
	tests := []struct {
		current store.FileState
		event   Event
	}{
		{store.FileStateActive, EventCacheFetch},
		{store.FileStateActive, EventCacheEvict},
		{store.FileStateActive, EventCompact},
		{store.FileStateRemote, EventFinalize},
		{store.FileStateRemote, EventCompact},
		{store.FileStateRemote, EventCacheEvict},
		{store.FileStateCached, EventFinalize},
		{store.FileStateCached, EventCompact},
		{store.FileStateCached, EventCacheFetch},
		{store.FileStateLocalFinalized, EventCacheFetch},
		{store.FileStateLocalFinalized, EventCacheEvict},
		{store.FileStateLocalFinalized, EventFinalize},
	}
	for _, tt := range tests {
		_, err := Transition(tt.current, tt.event)
		assert.True(t, errors.Is(err, ErrInvalidTransition),
			"%s + %s should be invalid", tt.current, tt.event)
	}
}

func TestIsWritable(t *testing.T) {
	assert.True(t, IsWritable(store.FileStateActive))
	assert.False(t, IsWritable(store.FileStateLocalFinalized))
	assert.False(t, IsWritable(store.FileStateCached))
	assert.False(t, IsWritable(store.FileStateRemote))
}

func TestIsLocal(t *testing.T) {
	assert.True(t, IsLocal(store.FileStateActive))
	assert.True(t, IsLocal(store.FileStateLocalFinalized))
	assert.False(t, IsLocal(store.FileStateCached))
	assert.False(t, IsLocal(store.FileStateRemote))
}

func TestIsEvictable(t *testing.T) {
	assert.False(t, IsEvictable(store.FileStateActive))
	assert.False(t, IsEvictable(store.FileStateLocalFinalized))
	assert.True(t, IsEvictable(store.FileStateCached))
	assert.False(t, IsEvictable(store.FileStateRemote))
}

func TestSourceForState(t *testing.T) {
	assert.Equal(t, store.FileSourceLocal, SourceForState(store.FileStateActive))
	assert.Equal(t, store.FileSourceLocal, SourceForState(store.FileStateLocalFinalized))
	assert.Equal(t, store.FileSourceServer, SourceForState(store.FileStateCached))
	assert.Equal(t, store.FileSourceServer, SourceForState(store.FileStateRemote))
}
