package fusefs

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/asheswook/bitcoin-slimnode/internal/store"
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
	h := &WriteHandle{fs: testFS, filename: filename, file: f}
	h.markOpened()
	return h, testFS
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
	content := []byte("test data")
	_, errno := h.Write(t.Context(), content, 0)
	require.Equal(t, syscall.Errno(0), errno)

	stored := st.files["blk00003.dat"]
	assert.Equal(t, store.FileStateActive, stored.State)

	path := filepath.Join(testFS.localDir, "blk00003.dat")
	require.NoError(t, os.Truncate(path, store.FinalizedFileThreshold))
	require.NoError(t, os.WriteFile(filepath.Join(testFS.localDir, "blk00004.dat"), []byte("next"), 0644))
	_, errno = h.Write(t.Context(), []byte{0x01}, store.FinalizedFileThreshold-1)
	require.Equal(t, syscall.Errno(0), errno)
	errno = h.Release(t.Context())
	assert.Equal(t, syscall.Errno(0), errno)

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

func TestWrite_DoesNotFinalizeAtMaxBlockFileSizeDuringWrite(t *testing.T) {
	st := newMockStore()
	entry := &store.FileEntry{Filename: "blk00999.dat", State: store.FileStateActive}
	st.files[entry.Filename] = entry
	h, testFS := makeWriteHandle(t, st, entry.Filename)

	path := filepath.Join(testFS.localDir, entry.Filename)
	require.NoError(t, os.Truncate(path, store.MaxBlockFileSize))

	_, errno := h.Write(t.Context(), []byte("abcd"), 0)
	assert.Equal(t, syscall.Errno(0), errno)

	updated := st.files[entry.Filename]
	assert.Equal(t, store.FileStateActive, updated.State)
	select {
	case name := <-testFS.finCh:
		t.Fatalf("unexpected finalization event for %s", name)
	default:
	}
}

func TestRelease_FinalizesBlockAtThreshold(t *testing.T) {
	st := newMockStore()
	entry := &store.FileEntry{Filename: "blk00123.dat", State: store.FileStateActive}
	st.files[entry.Filename] = entry
	h, testFS := makeWriteHandle(t, st, entry.Filename)

	path := filepath.Join(testFS.localDir, entry.Filename)
	require.NoError(t, os.Truncate(path, store.FinalizedFileThreshold))
	require.NoError(t, os.WriteFile(filepath.Join(testFS.localDir, "blk00124.dat"), []byte("x"), 0644))

	errno := h.Release(t.Context())
	assert.Equal(t, syscall.Errno(0), errno)

	updated := st.files[entry.Filename]
	assert.Equal(t, store.FileStateLocalFinalized, updated.State)
	select {
	case name := <-testFS.finCh:
		assert.Equal(t, entry.Filename, name)
	default:
		t.Fatal("expected finalization event in channel")
	}
}

func TestRelease_DoesNotFinalizeBelowThreshold(t *testing.T) {
	st := newMockStore()
	entry := &store.FileEntry{Filename: "blk00124.dat", State: store.FileStateActive}
	st.files[entry.Filename] = entry
	h, testFS := makeWriteHandle(t, st, entry.Filename)

	path := filepath.Join(testFS.localDir, entry.Filename)
	require.NoError(t, os.Truncate(path, store.FinalizedFileThreshold-1))

	errno := h.Release(t.Context())
	assert.Equal(t, syscall.Errno(0), errno)

	updated := st.files[entry.Filename]
	assert.Equal(t, store.FileStateActive, updated.State)
	select {
	case name := <-testFS.finCh:
		t.Fatalf("unexpected finalization event for %s", name)
	default:
	}
}

func TestRelease_DoesNotFinalizeRevEvenWithSuccessorBlock(t *testing.T) {
	st := newMockStore()
	entry := &store.FileEntry{Filename: "rev05501.dat", State: store.FileStateActive}
	st.files[entry.Filename] = entry
	h, testFS := makeWriteHandle(t, st, entry.Filename)

	path := filepath.Join(testFS.localDir, entry.Filename)
	require.NoError(t, os.Truncate(path, store.FinalizedFileThreshold))
	require.NoError(t, os.WriteFile(filepath.Join(testFS.localDir, "blk05502.dat"), []byte("next"), 0644))

	errno := h.Release(t.Context())
	assert.Equal(t, syscall.Errno(0), errno)

	updated := st.files[entry.Filename]
	assert.Equal(t, store.FileStateActive, updated.State)
	select {
	case name := <-testFS.finCh:
		t.Fatalf("unexpected finalization event for %s", name)
	default:
	}
}

func TestRelease_DoesNotFinalizeWithoutSuccessorBlock(t *testing.T) {
	st := newMockStore()
	entry := &store.FileEntry{Filename: "blk05499.dat", State: store.FileStateActive}
	st.files[entry.Filename] = entry
	h, testFS := makeWriteHandle(t, st, entry.Filename)

	path := filepath.Join(testFS.localDir, entry.Filename)
	require.NoError(t, os.Truncate(path, store.MaxBlockFileSize))

	errno := h.Release(t.Context())
	assert.Equal(t, syscall.Errno(0), errno)

	updated := st.files[entry.Filename]
	assert.Equal(t, store.FileStateActive, updated.State)
	select {
	case name := <-testFS.finCh:
		t.Fatalf("unexpected finalization event for %s", name)
	default:
	}
}

func TestRelease_FinalizesWithSuccessorBlockEvenWithoutThreshold(t *testing.T) {
	st := newMockStore()
	entry := &store.FileEntry{Filename: "blk07010.dat", State: store.FileStateActive}
	st.files[entry.Filename] = entry
	h, testFS := makeWriteHandle(t, st, entry.Filename)

	path := filepath.Join(testFS.localDir, entry.Filename)
	require.NoError(t, os.Truncate(path, 1024))
	require.NoError(t, os.WriteFile(filepath.Join(testFS.localDir, "blk07011.dat"), []byte("next"), 0644))

	errno := h.Release(t.Context())
	assert.Equal(t, syscall.Errno(0), errno)

	updated := st.files[entry.Filename]
	assert.Equal(t, store.FileStateLocalFinalized, updated.State)
	select {
	case name := <-testFS.finCh:
		assert.Equal(t, entry.Filename, name)
	default:
		t.Fatal("expected finalization event in channel")
	}
}

func TestRelease_DoesNotFinalizeNonBlockFiles(t *testing.T) {
	st := newMockStore()
	entry := &store.FileEntry{Filename: "metadata.tmp", State: store.FileStateActive}
	st.files[entry.Filename] = entry
	h, testFS := makeWriteHandle(t, st, entry.Filename)

	path := filepath.Join(testFS.localDir, entry.Filename)
	require.NoError(t, os.Truncate(path, store.MaxBlockFileSize))

	errno := h.Release(t.Context())
	assert.Equal(t, syscall.Errno(0), errno)

	updated := st.files[entry.Filename]
	assert.Equal(t, store.FileStateActive, updated.State)
	select {
	case name := <-testFS.finCh:
		t.Fatalf("unexpected finalization event for %s", name)
	default:
	}
}

func TestRelease_ClosesFileDescriptor(t *testing.T) {
	st := newMockStore()
	entry := &store.FileEntry{Filename: "rev00123.dat", State: store.FileStateActive}
	st.files[entry.Filename] = entry
	h, testFS := makeWriteHandle(t, st, entry.Filename)

	path := filepath.Join(testFS.localDir, entry.Filename)
	require.NoError(t, os.Truncate(path, store.FinalizedFileThreshold))

	errno := h.Release(t.Context())
	assert.Equal(t, syscall.Errno(0), errno)

	_, err := h.file.Stat()
	require.Error(t, err)
	assert.True(t, strings.Contains(strings.ToLower(err.Error()), "closed"))
}

func TestRelease_DoesNotFinalizeWithConcurrentOpenHandle(t *testing.T) {
	st := newMockStore()
	entry := &store.FileEntry{Filename: "blk07000.dat", State: store.FileStateActive}
	st.files[entry.Filename] = entry

	h1, testFS := makeWriteHandle(t, st, entry.Filename)
	path := filepath.Join(testFS.localDir, entry.Filename)
	file2, err := os.OpenFile(path, os.O_RDWR, 0644)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(testFS.localDir, "blk07001.dat"), []byte("next"), 0644))
	h2 := &WriteHandle{fs: testFS, filename: entry.Filename, file: file2}
	h2.markOpened()
	t.Cleanup(func() { _ = file2.Close() })

	require.NoError(t, os.Truncate(path, store.FinalizedFileThreshold))

	errno := h1.Release(t.Context())
	assert.Equal(t, syscall.Errno(0), errno)
	assert.Equal(t, store.FileStateActive, st.files[entry.Filename].State)
	select {
	case name := <-testFS.finCh:
		t.Fatalf("unexpected finalization event for %s", name)
	default:
	}

	errno = h2.Release(t.Context())
	assert.Equal(t, syscall.Errno(0), errno)
	assert.Equal(t, store.FileStateLocalFinalized, st.files[entry.Filename].State)
	select {
	case name := <-testFS.finCh:
		assert.Equal(t, entry.Filename, name)
	default:
		t.Fatal("expected finalization event in channel")
	}
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
	// Non-ACTIVE -> no-op, should return OK
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
