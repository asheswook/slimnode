package fusefs

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/hanwen/go-fuse/v2/fuse"

	"github.com/asheswook/bitcoin-slimnode/internal/blockmap"
	"github.com/asheswook/bitcoin-slimnode/internal/manifest"
	"github.com/asheswook/bitcoin-slimnode/internal/remote"
	"github.com/asheswook/bitcoin-slimnode/internal/store"
)

// ============================================================================
// Mock: mockStore
// ============================================================================

type mockStore struct {
	mu               sync.RWMutex
	files            map[string]*store.FileEntry
	forceGetFileErr  error
	forceUpsertErr   error
	forceUpdateStateErr error
	forceListErr     error
}

func newMockStore() *mockStore {
	return &mockStore{files: make(map[string]*store.FileEntry)}
}

func (s *mockStore) GetFile(filename string) (*store.FileEntry, error) {
	if s.forceGetFileErr != nil {
		return nil, s.forceGetFileErr
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.files[filename]
	if !ok {
		return nil, fmt.Errorf("store: file not found: %s", filename)
	}
	cp := *e
	return &cp, nil
}

func (s *mockStore) ListFiles() ([]store.FileEntry, error) {
	if s.forceListErr != nil {
		return nil, s.forceListErr
	}
	return nil, nil
}

func (s *mockStore) ListByState(_ store.FileState) ([]store.FileEntry, error) {
	if s.forceListErr != nil {
		return nil, s.forceListErr
	}
	return nil, nil
}

func (s *mockStore) ListCachedByLRU(_ int) ([]store.FileEntry, error) {
	if s.forceListErr != nil {
		return nil, s.forceListErr
	}
	return nil, nil
}

func (s *mockStore) UpdateLastAccess(_ string, _ time.Time) error { return nil }
func (s *mockStore) DeleteFile(_ string) error                     { return nil }
func (s *mockStore) Close() error                                  { return nil }

func (s *mockStore) UpsertFile(entry *store.FileEntry) error {
	if s.forceUpsertErr != nil {
		return s.forceUpsertErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *entry
	s.files[entry.Filename] = &cp
	return nil
}

func (s *mockStore) UpdateState(filename string, state store.FileState) error {
	if s.forceUpdateStateErr != nil {
		return s.forceUpdateStateErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.files[filename]; ok {
		e.State = state
	}
	return nil
}

// ============================================================================
// Mock: mockCache (backed by a real temp directory)
// ============================================================================

type mockCache struct {
	dir            string
	forceStoreErr  error
	forceRemoveErr error
}

func newMockCache(dir string) *mockCache {
	return &mockCache{dir: dir}
}

func (c *mockCache) Has(filename string) bool {
	_, err := os.Stat(c.Path(filename))
	return err == nil
}

func (c *mockCache) Path(filename string) string {
	return filepath.Join(c.dir, filename)
}

func (c *mockCache) Store(filename string, r io.Reader, _ string) error {
	if c.forceStoreErr != nil {
		return c.forceStoreErr
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	return os.WriteFile(c.Path(filename), data, 0644)
}

func (c *mockCache) Remove(filename string) error {
	if c.forceRemoveErr != nil {
		return c.forceRemoveErr
	}
	return os.Remove(c.Path(filename))
}

func (c *mockCache) Usage() (int64, int64) { return 0, 0 }

// ============================================================================
// Mock: mockRemoteClient
// ============================================================================

type fetchBlockCall struct {
	filename string
	offset   int64
	length   int64
}

type mockRemoteClient struct {
	mu                   sync.Mutex
	blockmapData         map[string][]byte // filename → raw blockmap bytes; absent = ErrFileNotFound
	blockData            map[string][]byte // "filename:offset" → block bytes
	fileData             map[string][]byte // filename → full file bytes
	fetchBlockCalls      []fetchBlockCall
	fetchBmCalls         []string // filenames for which FetchBlockmap was called
	forceFetchFileErr    error
	forceFetchManifestErr error
	forceFetchBlockErr   error
}

func newMockRemoteClient() *mockRemoteClient {
	return &mockRemoteClient{
		blockmapData: make(map[string][]byte),
		blockData:    make(map[string][]byte),
		fileData:     make(map[string][]byte),
	}
}

func (c *mockRemoteClient) FetchFile(_ context.Context, filename string, dest io.Writer) error {
	if c.forceFetchFileErr != nil {
		return c.forceFetchFileErr
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	data, ok := c.fileData[filename]
	if !ok {
		return remote.ErrFileNotFound
	}
	_, err := dest.Write(data)
	return err
}

func (c *mockRemoteClient) FetchManifest(_ context.Context, _ string) (*manifest.Manifest, string, error) {
	if c.forceFetchManifestErr != nil {
		return nil, "", c.forceFetchManifestErr
	}
	return nil, "", nil
}

func (c *mockRemoteClient) HealthCheck(_ context.Context) error { return nil }

func (c *mockRemoteClient) FetchSnapshot(_ context.Context, _ string, _ io.Writer) error {
	return nil
}

func (c *mockRemoteClient) FetchBlockmap(_ context.Context, filename string) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.fetchBmCalls = append(c.fetchBmCalls, filename)
	data, ok := c.blockmapData[filename]
	if !ok {
		return nil, remote.ErrFileNotFound
	}
	return data, nil
}

func (c *mockRemoteClient) FetchBlock(_ context.Context, filename string, offset, length int64) ([]byte, error) {
	if c.forceFetchBlockErr != nil {
		return nil, c.forceFetchBlockErr
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.fetchBlockCalls = append(c.fetchBlockCalls, fetchBlockCall{filename, offset, length})
	key := fmt.Sprintf("%s:%d", filename, offset)
	data, ok := c.blockData[key]
	if !ok {
		return nil, remote.ErrFileNotFound
	}
	return data, nil
}

func (c *mockRemoteClient) fetchBlockCallCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.fetchBlockCalls)
}

func (c *mockRemoteClient) fetchBmCallCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.fetchBmCalls)
}

// ============================================================================
// Mock: mockBlockCache
// ============================================================================

type mockBlockCache struct {
	mu               sync.RWMutex
	blocks           map[string][]byte // "filename:offset" → block data
	forceGetBlockErr error
	forceStoreBlockErr error
}

func newMockBlockCache() *mockBlockCache {
	return &mockBlockCache{blocks: make(map[string][]byte)}
}

func bcKey(blkFile string, fileOffset int64) string {
	return fmt.Sprintf("%s:%d", blkFile, fileOffset)
}

func (c *mockBlockCache) HasBlock(blkFile string, fileOffset int64) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, ok := c.blocks[bcKey(blkFile, fileOffset)]
	return ok
}

func (c *mockBlockCache) GetBlock(blkFile string, fileOffset int64) ([]byte, error) {
	if c.forceGetBlockErr != nil {
		return nil, c.forceGetBlockErr
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	data, ok := c.blocks[bcKey(blkFile, fileOffset)]
	if !ok {
		return nil, fmt.Errorf("block not found: %s:%d", blkFile, fileOffset)
	}
	cp := make([]byte, len(data))
	copy(cp, data)
	return cp, nil
}

func (c *mockBlockCache) StoreBlock(blkFile string, fileOffset int64, data []byte) error {
	if c.forceStoreBlockErr != nil {
		return c.forceStoreBlockErr
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	cp := make([]byte, len(data))
	copy(cp, data)
	c.blocks[bcKey(blkFile, fileOffset)] = cp
	return nil
}

func (c *mockBlockCache) RemoveFile(blkFile string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	prefix := blkFile + ":"
	for k := range c.blocks {
		if len(k) >= len(prefix) && k[:len(prefix)] == prefix {
			delete(c.blocks, k)
		}
	}
	return nil
}

func (c *mockBlockCache) Usage() (int64, int64) { return 0, 0 }

func (c *mockBlockCache) blockCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.blocks)
}

// ============================================================================
// Test helpers
// ============================================================================

// buildTestBlock creates a valid Bitcoin block for testing.
//
// dataSize is BlockDataSize (= 80-byte header + tx data).
// Returns the BlockmapEntry and the full preamble+header+txData bytes.
func buildTestBlock(offset int64, dataSize uint32) (blockmap.BlockmapEntry, []byte) {
	if dataSize < 80 {
		panic("dataSize must be >= 80")
	}

	// Deterministic 80-byte block header based on offset
	header := make([]byte, 80)
	for i := range header {
		header[i] = byte(int64(i)^offset) ^ byte(offset>>8)
	}

	// SHA256d of header
	first := sha256.Sum256(header)
	blockHash := sha256.Sum256(first[:])

	// Preamble: Bitcoin mainnet magic + LE uint32(dataSize)
	preamble := make([]byte, 8)
	preamble[0] = 0xF9
	preamble[1] = 0xBE
	preamble[2] = 0xB4
	preamble[3] = 0xD9
	binary.LittleEndian.PutUint32(preamble[4:8], dataSize)

	// Dummy tx data
	txData := make([]byte, int(dataSize)-80)
	for i := range txData {
		txData[i] = byte(i+1) ^ byte(offset)
	}

	// Full bytes = preamble(8) + header(80) + txData
	blockBytes := make([]byte, 8+int(dataSize))
	copy(blockBytes[0:8], preamble)
	copy(blockBytes[8:88], header)
	copy(blockBytes[88:], txData)

	entry := blockmap.BlockmapEntry{
		BlockHash:     blockHash,
		FileOffset:    offset,
		BlockDataSize: dataSize,
	}
	return entry, blockBytes
}

// buildBlockmapBytes serializes a Blockmap and returns (rawBytes, sha256Hex).
func buildBlockmapBytes(entries []blockmap.BlockmapEntry) ([]byte, string) {
	bm := &blockmap.Blockmap{Entries: entries}
	var buf bytes.Buffer
	if err := blockmap.Write(&buf, bm); err != nil {
		panic(fmt.Sprintf("buildBlockmapBytes: %v", err))
	}
	raw := buf.Bytes()
	hash := sha256.Sum256(raw)
	return raw, fmt.Sprintf("%x", hash)
}

// makeTestFS creates a minimal FS for testing.
// Pass bc=nil to simulate no block-level fetch support.
func makeTestFS(t *testing.T, st *mockStore, ca *mockCache, rc *mockRemoteClient, bc *mockBlockCache, m *manifest.Manifest) *FS {
	t.Helper()
	f := &FS{
		localDir:   t.TempDir(),
		st:         st,
		ca:         ca,
		rc:         rc,
		manifest:   m,
		blockmaps:  make(map[string]*blockmap.Blockmap),
		noBlockmap: make(map[string]bool),
		finCh:      make(chan string, 64),
	}
	if bc != nil {
		f.bc = bc
	}
	return f
}

// makeHandle creates a FileHandle pointing at the given FS.
func makeHandle(fsys *FS, filename string, state store.FileState) *FileHandle {
	return &FileHandle{
		fs:       fsys,
		filename: filename,
		state:    state,
	}
}

// readResultBytes extracts the raw bytes from a fuse.ReadResult.
func readResultBytes(t *testing.T, r fuse.ReadResult) []byte {
	t.Helper()
	if r == nil {
		return nil
	}
	data, _ := r.Bytes(nil)
	return data
}

// ============================================================================
// Test: TestMockStore_ForceError
// ============================================================================

func TestMockStore_ForceError(t *testing.T) {
	st := newMockStore()
	testErr := fmt.Errorf("injected error")

	// Test forceGetFileErr
	st.forceGetFileErr = testErr
	_, err := st.GetFile("test.dat")
	if err != testErr {
		t.Errorf("GetFile: expected %v, got %v", testErr, err)
	}
	st.forceGetFileErr = nil

	// Test forceUpsertErr
	st.forceUpsertErr = testErr
	err = st.UpsertFile(&store.FileEntry{Filename: "test.dat"})
	if err != testErr {
		t.Errorf("UpsertFile: expected %v, got %v", testErr, err)
	}
	st.forceUpsertErr = nil

	// Test forceUpdateStateErr
	st.forceUpdateStateErr = testErr
	err = st.UpdateState("test.dat", store.FileStateActive)
	if err != testErr {
		t.Errorf("UpdateState: expected %v, got %v", testErr, err)
	}
	st.forceUpdateStateErr = nil

	// Test forceListErr
	st.forceListErr = testErr
	_, err = st.ListFiles()
	if err != testErr {
		t.Errorf("ListFiles: expected %v, got %v", testErr, err)
	}
}
