package fusefs

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/asheswook/bitcoin-lfn/internal/store"
)

// makeWriteHandle creates a WriteHandle backed by a real temp file.
func makeWriteHandle(t *testing.T, st *mockStore, filename string) (*WriteHandle, *FS) {
	t.Helper()
	ca := newMockCache(t.TempDir())
	testFS := makeTestFS(t, st, ca, newMockRemoteClient(), nil, nil)
	path := filepath.Join(testFS.localDir, filename)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0644)
	require.NoError(t, err)
	t.Cleanup(func() { f.Close() })
	return &WriteHandle{fs: testFS, filename: filename, file: f}, testFS
}

func TestWrite_Normal(t *testing.T) {
	st := newMockStore()
	st.files["blk00000.dat"] = &store.FileEntry{Filename: "blk00000.dat", State: store.FileStateActive}
	h, _ := makeWriteHandle(t, st, "blk00000.dat")
	data := []byte("hello world")
	n, errno := h.Write(t.Context(), data, 0)
	assert.Equal(t, syscall.Errno(0), errno)
	assert.Equal(t, uint32(len(data)), n)
}

func TestWrite_ReadOnlyState(t *testing.T) {
	st := newMockStore()
	st.files["blk00001.dat"] = &store.FileEntry{Filename: "blk00001.dat", State: store.FileStateCached}
	h, _ := makeWriteHandle(t, st, "blk00001.dat")
	_, errno := h.Write(t.Context(), []byte("data"), 0)
	assert.Equal(t, syscall.EACCES, errno)
}

func TestWrite_StoreError(t *testing.T) {
	st := newMockStore()
	st.forceGetFileErr = syscall.EIO
	h, _ := makeWriteHandle(t, st, "blk00002.dat")
	_, errno := h.Write(t.Context(), []byte("data"), 0)
	assert.Equal(t, syscall.ENOENT, errno)
}

func TestWrite_TriggersFinalize(t *testing.T) {
	st := newMockStore()
	entry := &store.FileEntry{Filename: "blk00003.dat", State: store.FileStateActive}
	st.files["blk00003.dat"] = entry
	h, testFS := makeWriteHandle(t, st, "blk00003.dat")
	// Write some data first so file is not empty
	_, errno := h.Write(t.Context(), []byte("test data"), 0)
	require.Equal(t, syscall.Errno(0), errno)
	// Directly call finalize (avoids writing 128MiB)
	h.finalize(entry)
	// Verify finCh got the filename
	select {
	case name := <-testFS.finCh:
		assert.Equal(t, "blk00003.dat", name)
	default:
		t.Fatal("expected finalization event in channel")
	}
	// Verify state changed
	updated := st.files["blk00003.dat"]
	assert.Equal(t, store.FileStateLocalFinalized, updated.State)
}

func TestFsync_ActiveFile(t *testing.T) {
	st := newMockStore()
	st.files["blk00000.dat"] = &store.FileEntry{Filename: "blk00000.dat", State: store.FileStateActive}
	h, _ := makeWriteHandle(t, st, "blk00000.dat")
	errno := h.Fsync(t.Context(), 0)
	assert.Equal(t, syscall.Errno(0), errno)
}

func TestFsync_NonActiveFile(t *testing.T) {
	st := newMockStore()
	st.files["blk00001.dat"] = &store.FileEntry{Filename: "blk00001.dat", State: store.FileStateCached}
	h, _ := makeWriteHandle(t, st, "blk00001.dat")
	errno := h.Fsync(t.Context(), 0)
	// Non-ACTIVE → no-op, should return OK
	assert.Equal(t, syscall.Errno(0), errno)
}

func TestFsync_StoreError(t *testing.T) {
	st := newMockStore()
	st.forceGetFileErr = syscall.EIO
	h, _ := makeWriteHandle(t, st, "blk00002.dat")
	// Fsync returns OK even if GetFile fails (see write.go:57-68)
	errno := h.Fsync(t.Context(), 0)
	assert.Equal(t, syscall.Errno(0), errno)
}

func TestWriteHandle_Read_Data(t *testing.T) {
	st := newMockStore()
	st.files["blk00000.dat"] = &store.FileEntry{Filename: "blk00000.dat", State: store.FileStateActive}
	h, _ := makeWriteHandle(t, st, "blk00000.dat")
	// Write then read back
	payload := []byte("hello write_test")
	_, _ = h.Write(t.Context(), payload, 0)
	buf := make([]byte, len(payload))
	result, errno := h.Read(t.Context(), buf, 0)
	assert.Equal(t, syscall.Errno(0), errno)
	data, _ := result.Bytes(nil)
	assert.Equal(t, payload, data)
}

func TestWriteHandle_Read_Empty(t *testing.T) {
	st := newMockStore()
	h, _ := makeWriteHandle(t, st, "empty.dat")
	buf := make([]byte, 10)
	result, errno := h.Read(t.Context(), buf, 0)
	assert.Equal(t, syscall.Errno(0), errno)
	data, _ := result.Bytes(nil)
	assert.Empty(t, data)
}

func TestFileSHA256_KnownData(t *testing.T) {
	dir := t.TempDir()
	content := []byte("known content for sha256")
	path := filepath.Join(dir, "test.dat")
	require.NoError(t, os.WriteFile(path, content, 0644))
	got, err := fileSHA256(path)
	require.NoError(t, err)
	// Compute expected hash
	h := sha256.Sum256(content)
	expected := hex.EncodeToString(h[:])
	assert.Equal(t, expected, got)
}

func TestFileSHA256_NotExist(t *testing.T) {
	_, err := fileSHA256("/nonexistent/path/that/does/not/exist.dat")
	assert.Error(t, err)
}

func TestFinalize_StateTransition(t *testing.T) {
	st := newMockStore()
	entry := &store.FileEntry{Filename: "blk00000.dat", State: store.FileStateActive, SHA256: ""}
	st.files["blk00000.dat"] = entry
	h, _ := makeWriteHandle(t, st, "blk00000.dat")
	// Write some data so file is non-empty
	_, _ = h.Write(t.Context(), []byte("finalize test data"), 0)
	h.finalize(entry)
	updated := st.files["blk00000.dat"]
	assert.Equal(t, store.FileStateLocalFinalized, updated.State)
	assert.NotEmpty(t, updated.SHA256)
}

func TestFinalize_NotifiesChannel(t *testing.T) {
	st := newMockStore()
	entry := &store.FileEntry{Filename: "blk00001.dat", State: store.FileStateActive}
	st.files["blk00001.dat"] = entry
	h, testFS := makeWriteHandle(t, st, "blk00001.dat")
	_, _ = h.Write(t.Context(), []byte("channel test"), 0)
	h.finalize(entry)
	select {
	case name := <-testFS.finCh:
		assert.Equal(t, "blk00001.dat", name)
	default:
		t.Fatal("expected event in finCh but channel was empty")
	}
}
