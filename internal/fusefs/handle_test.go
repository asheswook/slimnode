package fusefs

import (
	"context"
	"fmt"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/asheswook/bitcoin-lfn/internal/blockmap"
	"github.com/asheswook/bitcoin-lfn/internal/manifest"
	"github.com/asheswook/bitcoin-lfn/internal/store"
)

// ============================================================================
// Test 1: TestReadViaBlockmap_SingleBlock
// ============================================================================

func TestReadViaBlockmap_SingleBlock(t *testing.T) {
	const filename = "blk00100.dat"
	entry, blockBytes := buildTestBlock(0, 1000)

	bc := newMockBlockCache()
	rc := newMockRemoteClient()
	rc.blockData[fmt.Sprintf("%s:%d", filename, entry.FileOffset)] = blockBytes

	fsys := makeTestFS(t, newMockStore(), newMockCache(t.TempDir()), rc, bc, nil)
	// Pre-populate blockmap in fs so getOrLoadBlockmap doesn't hit remote
	fsys.blockmaps[filename] = &blockmap.Blockmap{
		Filename: filename,
		Entries:  []blockmap.BlockmapEntry{entry},
	}

	h := makeHandle(fsys, filename, store.FileStateRemote)

	dest := make([]byte, 100)
	result, errno := h.readViaBlockmap(context.Background(), dest, 50)
	require.Equal(t, syscall.Errno(0), errno)

	data := readResultBytes(t, result)
	require.Len(t, data, 100)
	assert.Equal(t, blockBytes[50:150], data, "bytes should come from block data at [50:150]")

	// Block should now be cached
	assert.True(t, bc.HasBlock(filename, 0), "block must be stored in block cache")
	// FetchBlock called exactly once
	assert.Equal(t, 1, rc.fetchBlockCallCount())
}

// ============================================================================
// Test 2: TestReadViaBlockmap_CacheHit
// ============================================================================

func TestReadViaBlockmap_CacheHit(t *testing.T) {
	const filename = "blk00101.dat"
	entry, blockBytes := buildTestBlock(0, 1000)

	bc := newMockBlockCache()
	// Pre-populate block cache – FetchBlock must NOT be called
	require.NoError(t, bc.StoreBlock(filename, entry.FileOffset, blockBytes))

	rc := newMockRemoteClient()
	fsys := makeTestFS(t, newMockStore(), newMockCache(t.TempDir()), rc, bc, nil)
	fsys.blockmaps[filename] = &blockmap.Blockmap{
		Filename: filename,
		Entries:  []blockmap.BlockmapEntry{entry},
	}

	h := makeHandle(fsys, filename, store.FileStateRemote)

	dest := make([]byte, 100)
	result, errno := h.readViaBlockmap(context.Background(), dest, 50)
	require.Equal(t, syscall.Errno(0), errno)

	data := readResultBytes(t, result)
	require.Len(t, data, 100)
	assert.Equal(t, blockBytes[50:150], data)

	// FetchBlock must NOT have been called (cache hit)
	assert.Equal(t, 0, rc.fetchBlockCallCount(), "FetchBlock must not be called on cache hit")
}

// ============================================================================
// Test 3: TestReadViaBlockmap_CrossBlockRead
// ============================================================================

func TestReadViaBlockmap_CrossBlockRead(t *testing.T) {
	const filename = "blk00102.dat"

	// Block A: offset 0, BlockDataSize=500 → covers file bytes [0, 508)
	entryA, blockBytesA := buildTestBlock(0, 500)
	// Block B: offset 508, BlockDataSize=1000 → covers file bytes [508, 1516)
	entryB, blockBytesB := buildTestBlock(508, 1000)

	bc := newMockBlockCache()
	rc := newMockRemoteClient()
	rc.blockData[fmt.Sprintf("%s:%d", filename, entryA.FileOffset)] = blockBytesA
	rc.blockData[fmt.Sprintf("%s:%d", filename, entryB.FileOffset)] = blockBytesB

	fsys := makeTestFS(t, newMockStore(), newMockCache(t.TempDir()), rc, bc, nil)
	fsys.blockmaps[filename] = &blockmap.Blockmap{
		Filename: filename,
		Entries:  []blockmap.BlockmapEntry{entryA, entryB},
	}

	h := makeHandle(fsys, filename, store.FileStateRemote)

	// Read 100 bytes at offset 500: spans [500,600)
	// Block A covers [500,508) → 8 bytes
	// Block B covers [508,600) → 92 bytes
	dest := make([]byte, 100)
	result, errno := h.readViaBlockmap(context.Background(), dest, 500)
	require.Equal(t, syscall.Errno(0), errno)

	data := readResultBytes(t, result)
	require.Len(t, data, 100)

	// Verify assembly
	expected := make([]byte, 100)
	copy(expected[0:8], blockBytesA[500:508])  // srcOffset=500, dstOffset=0
	copy(expected[8:100], blockBytesB[0:92])   // srcOffset=0, dstOffset=8

	assert.Equal(t, expected, data, "cross-block assembly must be correct")

	// Both blocks were fetched
	assert.Equal(t, 2, rc.fetchBlockCallCount(), "both blocks must be fetched")
	assert.True(t, bc.HasBlock(filename, entryA.FileOffset))
	assert.True(t, bc.HasBlock(filename, entryB.FileOffset))
}

// ============================================================================
// Test 4: TestRead_NoBc_FallsBackToFullFile
// ============================================================================

func TestRead_NoBc_FallsBackToFullFile(t *testing.T) {
	const filename = "blk00103.dat"
	fileContent := []byte("hello, full file content for testing fallback path")

	st := newMockStore()
	require.NoError(t, st.UpsertFile(&store.FileEntry{
		Filename:  filename,
		State:     store.FileStateRemote,
		Source:    store.FileSourceServer,
		CreatedAt: time.Now(),
	}))

	rc := newMockRemoteClient()
	rc.fileData[filename] = fileContent

	cacheDir := t.TempDir()
	ca := newMockCache(cacheDir)

	// bc = nil → no blockmap path
	fsys := makeTestFS(t, st, ca, rc, nil, nil)

	h := makeHandle(fsys, filename, store.FileStateRemote)

	dest := make([]byte, len(fileContent))
	result, errno := h.Read(context.Background(), dest, 0)
	require.Equal(t, syscall.Errno(0), errno, "Read must succeed via full-file fallback")

	data := readResultBytes(t, result)
	assert.Equal(t, fileContent, data)

	// File should now be cached on disk
	assert.True(t, ca.Has(filename), "file must be written to cache")

	// FetchBlockmap must NOT have been called
	assert.Equal(t, 0, rc.fetchBmCallCount(), "FetchBlockmap must not be called when bc=nil")
}

// ============================================================================
// Test 5: TestRead_RevFile_SkipsBlockmap
// ============================================================================

func TestRead_RevFile_SkipsBlockmap(t *testing.T) {
	const filename = "rev00100.dat"
	fileContent := []byte("rev file full content, no blockmap for rev files")

	st := newMockStore()
	require.NoError(t, st.UpsertFile(&store.FileEntry{
		Filename:  filename,
		State:     store.FileStateRemote,
		Source:    store.FileSourceServer,
		CreatedAt: time.Now(),
	}))

	rc := newMockRemoteClient()
	rc.fileData[filename] = fileContent

	cacheDir := t.TempDir()
	ca := newMockCache(cacheDir)

	// bc is set, but filename starts with "rev" → blockmap path skipped
	bc := newMockBlockCache()
	fsys := makeTestFS(t, st, ca, rc, bc, nil)

	h := makeHandle(fsys, filename, store.FileStateRemote)

	dest := make([]byte, len(fileContent))
	result, errno := h.Read(context.Background(), dest, 0)
	require.Equal(t, syscall.Errno(0), errno, "Read must succeed via full-file path for rev files")

	data := readResultBytes(t, result)
	assert.Equal(t, fileContent, data)

	// Blockmap and FetchBlock must NOT have been called
	assert.Equal(t, 0, rc.fetchBmCallCount(), "FetchBlockmap must not be called for rev files")
	assert.Equal(t, 0, rc.fetchBlockCallCount(), "FetchBlock must not be called for rev files")
}

// ============================================================================
// Test 6: TestGetOrLoadBlockmap_NegativeCache
// ============================================================================

func TestGetOrLoadBlockmap_NegativeCache(t *testing.T) {
	const filename = "blk00200.dat"

	rc := newMockRemoteClient()
	// Manifest with file entry but NO BlockmapSHA256
	m := &manifest.Manifest{
		Files: []manifest.ManifestFile{
			{Name: filename, Size: 1000, SHA256: "abc", BlockmapSHA256: ""},
		},
	}
	fsys := makeTestFS(t, newMockStore(), newMockCache(t.TempDir()), rc, newMockBlockCache(), m)
	h := makeHandle(fsys, filename, store.FileStateRemote)

	// First call: should detect no blockmap, set negative cache
	bm1, err := h.getOrLoadBlockmap(context.Background())
	require.NoError(t, err)
	assert.Nil(t, bm1, "must return nil when file has no blockmap")

	// Verify negative cache is set
	fsys.noBlockmapMu.RLock()
	noBM := fsys.noBlockmap[filename]
	fsys.noBlockmapMu.RUnlock()
	assert.True(t, noBM, "negative cache must be set")

	// Second call: must return immediately without hitting remote
	bmCallsBefore := rc.fetchBmCallCount()
	bm2, err := h.getOrLoadBlockmap(context.Background())
	require.NoError(t, err)
	assert.Nil(t, bm2, "second call must still return nil")
	assert.Equal(t, bmCallsBefore, rc.fetchBmCallCount(), "FetchBlockmap must not be called on second call")
}

// ============================================================================
// Test 7: TestGetOrLoadBlockmap_FetchAndVerify
// ============================================================================

func TestGetOrLoadBlockmap_FetchAndVerify(t *testing.T) {
	const filename = "blk00201.dat"

	entry, _ := buildTestBlock(0, 500)
	rawBM, hashHex := buildBlockmapBytes([]blockmap.BlockmapEntry{entry})

	rc := newMockRemoteClient()
	rc.blockmapData[filename] = rawBM

	m := &manifest.Manifest{
		Files: []manifest.ManifestFile{
			{Name: filename, Size: 5000, SHA256: "dummy", BlockmapSHA256: hashHex},
		},
	}
	fsys := makeTestFS(t, newMockStore(), newMockCache(t.TempDir()), rc, newMockBlockCache(), m)
	h := makeHandle(fsys, filename, store.FileStateRemote)

	bm, err := h.getOrLoadBlockmap(context.Background())
	require.NoError(t, err)
	require.NotNil(t, bm, "blockmap must be returned when hash matches")

	assert.Equal(t, filename, bm.Filename)
	require.Len(t, bm.Entries, 1)
	assert.Equal(t, entry.FileOffset, bm.Entries[0].FileOffset)
	assert.Equal(t, entry.BlockDataSize, bm.Entries[0].BlockDataSize)
	assert.Equal(t, entry.BlockHash, bm.Entries[0].BlockHash)

	// Must be cached in fs.blockmaps
	fsys.blockmapsMu.RLock()
	cached := fsys.blockmaps[filename]
	fsys.blockmapsMu.RUnlock()
	assert.NotNil(t, cached, "blockmap must be stored in fs.blockmaps cache")

	// A second call must use the in-memory cache, not hit remote again
	bmCallsBefore := rc.fetchBmCallCount()
	bm2, err := h.getOrLoadBlockmap(context.Background())
	require.NoError(t, err)
	assert.Same(t, bm, bm2, "second call must return cached blockmap pointer")
	assert.Equal(t, bmCallsBefore, rc.fetchBmCallCount(), "FetchBlockmap must not be called on cache hit")
}

// ============================================================================
// Test 8: TestGetOrLoadBlockmap_SHA256Mismatch
// ============================================================================

func TestGetOrLoadBlockmap_SHA256Mismatch(t *testing.T) {
	const filename = "blk00202.dat"

	entry, _ := buildTestBlock(0, 500)
	rawBM, _ := buildBlockmapBytes([]blockmap.BlockmapEntry{entry})

	rc := newMockRemoteClient()
	rc.blockmapData[filename] = rawBM

	m := &manifest.Manifest{
		Files: []manifest.ManifestFile{
			{Name: filename, Size: 5000, SHA256: "dummy", BlockmapSHA256: "0000000000000000000000000000000000000000000000000000000000000000"},
		},
	}
	fsys := makeTestFS(t, newMockStore(), newMockCache(t.TempDir()), rc, newMockBlockCache(), m)
	h := makeHandle(fsys, filename, store.FileStateRemote)

	bm, err := h.getOrLoadBlockmap(context.Background())
	require.NoError(t, err, "SHA-256 mismatch must not return error (graceful fallback)")
	assert.Nil(t, bm, "must return nil on hash mismatch")

	// Negative cache must be set
	fsys.noBlockmapMu.RLock()
	noBM := fsys.noBlockmap[filename]
	fsys.noBlockmapMu.RUnlock()
	assert.True(t, noBM, "negative cache must be set on hash mismatch")
}

// ============================================================================
// Test 9: TestGetOrLoadBlockmap_ServerReturns404
// ============================================================================

func TestGetOrLoadBlockmap_ServerReturns404(t *testing.T) {
	const filename = "blk00203.dat"

	rc := newMockRemoteClient()
	// blockmapData does NOT contain filename → FetchBlockmap returns ErrFileNotFound

	m := &manifest.Manifest{
		Files: []manifest.ManifestFile{
			{Name: filename, Size: 5000, SHA256: "dummy", BlockmapSHA256: "abc123notreal"},
		},
	}
	fsys := makeTestFS(t, newMockStore(), newMockCache(t.TempDir()), rc, newMockBlockCache(), m)
	h := makeHandle(fsys, filename, store.FileStateRemote)

	bm, err := h.getOrLoadBlockmap(context.Background())
	require.NoError(t, err, "404 from server must not return error (graceful fallback)")
	assert.Nil(t, bm, "must return nil when server returns 404")

	// Negative cache must be set
	fsys.noBlockmapMu.RLock()
	noBM := fsys.noBlockmap[filename]
	fsys.noBlockmapMu.RUnlock()
	assert.True(t, noBM, "negative cache must be set on 404")
}

// ============================================================================
// Test 10: TestFetchBlock_HashVerification
// ============================================================================

func TestFetchBlock_HashVerification(t *testing.T) {
	const filename = "blk00300.dat"

	entry, correctBlockBytes := buildTestBlock(0, 500)

	// Corrupt the header bytes (data[8:88]) so the hash won't match
	corruptBytes := make([]byte, len(correctBlockBytes))
	copy(corruptBytes, correctBlockBytes)
	for i := 8; i < 88; i++ {
		corruptBytes[i] ^= 0xFF // flip all bits in header
	}

	bc := newMockBlockCache()
	rc := newMockRemoteClient()
	rc.blockData[fmt.Sprintf("%s:%d", filename, entry.FileOffset)] = corruptBytes

	fsys := makeTestFS(t, newMockStore(), newMockCache(t.TempDir()), rc, bc, nil)
	h := makeHandle(fsys, filename, store.FileStateRemote)

	err := h.fetchBlock(context.Background(), entry)
	assert.Error(t, err, "fetchBlock must return error on hash mismatch")
	assert.Contains(t, err.Error(), "hash mismatch")

	// Block must NOT be stored in block cache
	assert.False(t, bc.HasBlock(filename, entry.FileOffset), "corrupt block must not be cached")
}

// ============================================================================
// Test 11: TestAssembleRead_MultipleBlocks
// ============================================================================

func TestAssembleRead_MultipleBlocks(t *testing.T) {
	const filename = "blk00400.dat"

	// Block A: offset 0, size 200 → covers [0, 208)
	entryA, blockBytesA := buildTestBlock(0, 200)
	// Block B: offset 208, size 300 → covers [208, 516)
	entryB, blockBytesB := buildTestBlock(208, 300)
	// Block C: offset 516, size 400 → covers [516, 924)
	entryC, blockBytesC := buildTestBlock(516, 400)

	bc := newMockBlockCache()
	require.NoError(t, bc.StoreBlock(filename, entryA.FileOffset, blockBytesA))
	require.NoError(t, bc.StoreBlock(filename, entryB.FileOffset, blockBytesB))
	require.NoError(t, bc.StoreBlock(filename, entryC.FileOffset, blockBytesC))

	fsys := makeTestFS(t, newMockStore(), newMockCache(t.TempDir()), newMockRemoteClient(), bc, nil)
	h := makeHandle(fsys, filename, store.FileStateRemote)

	// Read 600 bytes at offset 100: spans [100, 700)
	// Block A [0,208): contributes [100,208) = 108 bytes
	// Block B [208,516): contributes [208,516) = 308 bytes
	// Block C [516,924): contributes [516,700) = 184 bytes
	// Total = 600 bytes
	blocks := []blockmap.BlockmapEntry{entryA, entryB, entryC}
	dest := make([]byte, 600)
	n, err := h.assembleRead(dest, 100, blocks)
	require.NoError(t, err)
	assert.Equal(t, 600, n)

	expected := make([]byte, 600)
	// A: srcOffset=100-0=100, dstOffset=100-100=0, length=108
	copy(expected[0:108], blockBytesA[100:208])
	// B: srcOffset=208-208=0, dstOffset=208-100=108, length=308
	copy(expected[108:416], blockBytesB[0:308])
	// C: srcOffset=516-516=0, dstOffset=516-100=416, length=184
	copy(expected[416:600], blockBytesC[0:184])

	assert.Equal(t, expected, dest[:n], "assembled data must match expected multi-block assembly")
}
