package fusefs

import (
	"sync"
	"testing"

	"github.com/asheswook/bitcoin-slimnode/internal/blockmap"
	"github.com/asheswook/bitcoin-slimnode/internal/manifest"
	"github.com/stretchr/testify/assert"
)

// TestNew verifies that New() initializes the FS struct correctly with nil blockmaps.
func TestNew(t *testing.T) {
	st := newMockStore()
	ca := newMockCache(t.TempDir())
	rc := newMockRemoteClient()
	m := &manifest.Manifest{}
	f := New("/mnt", t.TempDir(), "", st, ca, rc, m, nil, nil)

	assert.NotNil(t, f)
	assert.Equal(t, "/mnt", f.mountPoint)
	assert.Equal(t, st, f.st)
	assert.NotNil(t, f.blockmaps)   // nil input → initialized to empty map
	assert.NotNil(t, f.noBlockmap)  // always initialized
	assert.NotNil(t, f.finCh)       // created with cap=64
	assert.Equal(t, 64, cap(f.finCh))
}

// TestNew_WithBlockmaps verifies that New() preserves non-nil blockmaps.
func TestNew_WithBlockmaps(t *testing.T) {
	bm := map[string]*blockmap.Blockmap{"blk00000.dat": {}}
	f := New("/mnt", t.TempDir(), "", newMockStore(), newMockCache(t.TempDir()), newMockRemoteClient(), nil, nil, bm)

	assert.Equal(t, bm, f.blockmaps)  // preserved
}

// TestUpdateManifest verifies that UpdateManifest() atomically replaces the manifest.
func TestUpdateManifest(t *testing.T) {
	f := makeTestFS(t, newMockStore(), newMockCache(t.TempDir()), newMockRemoteClient(), nil, nil)
	m1 := &manifest.Manifest{Version: 1}
	f.UpdateManifest(m1)
	assert.Equal(t, m1, f.manifest)

	m2 := &manifest.Manifest{Version: 2}
	f.UpdateManifest(m2)
	assert.Equal(t, m2, f.manifest)
}

// TestUpdateManifest_Concurrent verifies that UpdateManifest() is safe for concurrent access.
func TestUpdateManifest_Concurrent(t *testing.T) {
	f := makeTestFS(t, newMockStore(), newMockCache(t.TempDir()), newMockRemoteClient(), nil, nil)
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			f.UpdateManifest(&manifest.Manifest{Version: i})
		}(i)
	}
	wg.Wait()
	// Just verifying no race condition — no assertion needed
}

// TestFinalizationEvents verifies that FinalizationEvents() returns the finCh channel.
func TestFinalizationEvents(t *testing.T) {
	f := makeTestFS(t, newMockStore(), newMockCache(t.TempDir()), newMockRemoteClient(), nil, nil)
	ch := f.FinalizationEvents()
	assert.NotNil(t, ch)

	// Send a message and verify it's receivable
	f.finCh <- "blk00000.dat"
	select {
	case name := <-ch:
		assert.Equal(t, "blk00000.dat", name)
	default:
		t.Fatal("expected message in channel")
	}
}
