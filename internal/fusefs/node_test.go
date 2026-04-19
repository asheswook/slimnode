package fusefs

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/hanwen/go-fuse/v2/fuse"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/asheswook/bitcoin-slimnode/internal/store"
)

func TestGetattr_SpecialFile(t *testing.T) {
	st := newMockStore()
	ca := newMockCache(t.TempDir())
	testFS := makeTestFS(t, st, ca, newMockRemoteClient(), nil, nil)
	n := &FileNode{fs: testFS, filename: "xor.dat", special: true}
	var out fuse.AttrOut
	errno := n.Getattr(t.Context(), nil, &out)
	assert.Equal(t, syscall.Errno(0), errno)
	assert.Equal(t, uint32(fuse.S_IFREG|0644), out.Attr.Mode)
	assert.Equal(t, uint64(8), out.Attr.Size)
}

func TestGetattr_ActiveFile(t *testing.T) {
	st := newMockStore()
	st.files["blk00000.dat"] = &store.FileEntry{Filename: "blk00000.dat", State: store.FileStateActive, Size: 2048}
	ca := newMockCache(t.TempDir())
	testFS := makeTestFS(t, st, ca, newMockRemoteClient(), nil, nil)

	// Create a real file with a DIFFERENT size than the store entry
	realPath := filepath.Join(testFS.localDir, "blk00000.dat")
	require.NoError(t, os.WriteFile(realPath, make([]byte, 4096), 0644))

	n := &FileNode{fs: testFS, filename: "blk00000.dat"}
	var out fuse.AttrOut
	errno := n.Getattr(t.Context(), nil, &out)
	assert.Equal(t, syscall.Errno(0), errno)
	assert.Equal(t, uint32(fuse.S_IFREG|0644), out.Attr.Mode)
	// Must return REAL file size (4096), not stale store size (2048)
	assert.Equal(t, uint64(4096), out.Attr.Size)
}

func TestGetattr_ActiveFile_FallbackToStoreSize(t *testing.T) {
	st := newMockStore()
	st.files["blk00000.dat"] = &store.FileEntry{Filename: "blk00000.dat", State: store.FileStateActive, Size: 2048}
	ca := newMockCache(t.TempDir())
	testFS := makeTestFS(t, st, ca, newMockRemoteClient(), nil, nil)
	// Don't create a real file - test fallback
	n := &FileNode{fs: testFS, filename: "blk00000.dat"}
	var out fuse.AttrOut
	errno := n.Getattr(t.Context(), nil, &out)
	assert.Equal(t, syscall.Errno(0), errno)
	assert.Equal(t, uint64(2048), out.Attr.Size)
}

func TestGetattr_CachedFile(t *testing.T) {
	st := newMockStore()
	st.files["blk00001.dat"] = &store.FileEntry{Filename: "blk00001.dat", State: store.FileStateCached, Size: 4096}
	ca := newMockCache(t.TempDir())
	testFS := makeTestFS(t, st, ca, newMockRemoteClient(), nil, nil)
	n := &FileNode{fs: testFS, filename: "blk00001.dat"}
	var out fuse.AttrOut
	errno := n.Getattr(t.Context(), nil, &out)
	assert.Equal(t, syscall.Errno(0), errno)
	assert.Equal(t, uint32(fuse.S_IFREG|0444), out.Attr.Mode)
	assert.Equal(t, uint64(4096), out.Attr.Size)
}

func TestGetattr_LocalFinalizedFile(t *testing.T) {
	st := newMockStore()
	st.files["blk00002.dat"] = &store.FileEntry{Filename: "blk00002.dat", State: store.FileStateLocalFinalized, Size: 8192}
	ca := newMockCache(t.TempDir())
	testFS := makeTestFS(t, st, ca, newMockRemoteClient(), nil, nil)
	n := &FileNode{fs: testFS, filename: "blk00002.dat"}
	var out fuse.AttrOut
	errno := n.Getattr(t.Context(), nil, &out)
	assert.Equal(t, syscall.Errno(0), errno)
	assert.Equal(t, uint32(fuse.S_IFREG|0444), out.Attr.Mode)
}

func TestGetattr_RemoteFile(t *testing.T) {
	st := newMockStore()
	st.files["blk00003.dat"] = &store.FileEntry{Filename: "blk00003.dat", State: store.FileStateRemote, Size: 16384}
	ca := newMockCache(t.TempDir())
	testFS := makeTestFS(t, st, ca, newMockRemoteClient(), nil, nil)
	n := &FileNode{fs: testFS, filename: "blk00003.dat"}
	var out fuse.AttrOut
	errno := n.Getattr(t.Context(), nil, &out)
	assert.Equal(t, syscall.Errno(0), errno)
	assert.Equal(t, uint32(fuse.S_IFREG|0444), out.Attr.Mode)
}

func TestGetattr_StoreError(t *testing.T) {
	st := newMockStore()
	st.forceGetFileErr = syscall.EIO
	ca := newMockCache(t.TempDir())
	testFS := makeTestFS(t, st, ca, newMockRemoteClient(), nil, nil)
	n := &FileNode{fs: testFS, filename: "missing.dat"}
	var out fuse.AttrOut
	errno := n.Getattr(t.Context(), nil, &out)
	assert.Equal(t, syscall.ENOENT, errno)
}

func TestOpen_SpecialFile(t *testing.T) {
	st := newMockStore()
	ca := newMockCache(t.TempDir())
	testFS := makeTestFS(t, st, ca, newMockRemoteClient(), nil, nil)
	n := &FileNode{fs: testFS, filename: "xor.dat", special: true}
	fh, flags, errno := n.Open(t.Context(), 0)
	require.Equal(t, syscall.Errno(0), errno)
	assert.NotNil(t, fh)
	assert.Equal(t, uint32(0), flags)
	if fhTyped, ok := fh.(*FileHandle); ok {
		assert.True(t, fhTyped.special)
	}
}

func TestOpen_CachedFile(t *testing.T) {
	st := newMockStore()
	st.files["blk00000.dat"] = &store.FileEntry{Filename: "blk00000.dat", State: store.FileStateCached}
	ca := newMockCache(t.TempDir())
	testFS := makeTestFS(t, st, ca, newMockRemoteClient(), nil, nil)
	n := &FileNode{fs: testFS, filename: "blk00000.dat"}
	fh, flags, errno := n.Open(t.Context(), 0)
	require.Equal(t, syscall.Errno(0), errno)
	assert.NotNil(t, fh)
	assert.Equal(t, uint32(fuse.FOPEN_KEEP_CACHE), flags)
}

func TestOpen_LocalFinalizedFile(t *testing.T) {
	st := newMockStore()
	st.files["blk00001.dat"] = &store.FileEntry{Filename: "blk00001.dat", State: store.FileStateLocalFinalized}
	ca := newMockCache(t.TempDir())
	testFS := makeTestFS(t, st, ca, newMockRemoteClient(), nil, nil)
	n := &FileNode{fs: testFS, filename: "blk00001.dat"}
	fh, flags, errno := n.Open(t.Context(), 0)
	require.Equal(t, syscall.Errno(0), errno)
	assert.NotNil(t, fh)
	assert.Equal(t, uint32(fuse.FOPEN_KEEP_CACHE), flags)
}

func TestOpen_ActiveFile(t *testing.T) {
	st := newMockStore()
	st.files["blk00002.dat"] = &store.FileEntry{Filename: "blk00002.dat", State: store.FileStateActive}
	ca := newMockCache(t.TempDir())
	testFS := makeTestFS(t, st, ca, newMockRemoteClient(), nil, nil)
	// Create the file in localDir so os.OpenFile succeeds
	path := filepath.Join(testFS.localDir, "blk00002.dat")
	require.NoError(t, os.WriteFile(path, []byte{}, 0644))
	n := &FileNode{fs: testFS, filename: "blk00002.dat"}
	fh, flags, errno := n.Open(t.Context(), syscall.O_RDWR)
	require.Equal(t, syscall.Errno(0), errno)
	assert.NotNil(t, fh)
	assert.Equal(t, uint32(0), flags)
	_, isWriteHandle := fh.(*WriteHandle)
	assert.True(t, isWriteHandle)
}

func TestOpen_ActiveFile_ReadOnlyReturnsFileHandle(t *testing.T) {
	st := newMockStore()
	st.files["blk00002.dat"] = &store.FileEntry{Filename: "blk00002.dat", State: store.FileStateActive}
	ca := newMockCache(t.TempDir())
	testFS := makeTestFS(t, st, ca, newMockRemoteClient(), nil, nil)
	path := filepath.Join(testFS.localDir, "blk00002.dat")
	require.NoError(t, os.WriteFile(path, []byte("data"), 0644))

	n := &FileNode{fs: testFS, filename: "blk00002.dat"}
	fh, flags, errno := n.Open(t.Context(), syscall.O_RDONLY)
	require.Equal(t, syscall.Errno(0), errno)
	assert.Equal(t, uint32(0), flags)
	_, isFileHandle := fh.(*FileHandle)
	assert.True(t, isFileHandle)
}

func TestOpen_RemoteFile(t *testing.T) {
	st := newMockStore()
	st.files["blk00003.dat"] = &store.FileEntry{Filename: "blk00003.dat", State: store.FileStateRemote}
	ca := newMockCache(t.TempDir())
	testFS := makeTestFS(t, st, ca, newMockRemoteClient(), nil, nil)
	n := &FileNode{fs: testFS, filename: "blk00003.dat"}
	fh, flags, errno := n.Open(t.Context(), 0)
	require.Equal(t, syscall.Errno(0), errno)
	assert.NotNil(t, fh)
	assert.Equal(t, uint32(0), flags)
}

func TestOpen_StoreError(t *testing.T) {
	st := newMockStore()
	st.forceGetFileErr = syscall.EIO
	ca := newMockCache(t.TempDir())
	testFS := makeTestFS(t, st, ca, newMockRemoteClient(), nil, nil)
	n := &FileNode{fs: testFS, filename: "missing.dat"}
	fh, _, errno := n.Open(t.Context(), 0)
	assert.Equal(t, syscall.ENOENT, errno)
	assert.Nil(t, fh)
}

func TestFileNode_Open_RemoteRevWithWriteFlag(t *testing.T) {
	st := newMockStore()
	st.files["rev00000.dat"] = &store.FileEntry{Filename: "rev00000.dat", State: store.FileStateRemote}
	ca := newMockCache(t.TempDir())
	testFS := makeTestFS(t, st, ca, newMockRemoteClient(), nil, nil)
	n := &FileNode{fs: testFS, filename: "rev00000.dat"}
	fh, flags, errno := n.Open(t.Context(), syscall.O_RDWR)
	require.Equal(t, syscall.Errno(0), errno)
	require.NotNil(t, fh)
	assert.Equal(t, uint32(fuse.FOPEN_KEEP_CACHE), flags)
	_, isNull := fh.(*NullWriteHandle)
	assert.True(t, isNull)
}

func TestFileNode_Open_RemoteBlkWithWriteFlag(t *testing.T) {
	st := newMockStore()
	st.files["blk00000.dat"] = &store.FileEntry{Filename: "blk00000.dat", State: store.FileStateRemote}
	ca := newMockCache(t.TempDir())
	testFS := makeTestFS(t, st, ca, newMockRemoteClient(), nil, nil)
	n := &FileNode{fs: testFS, filename: "blk00000.dat"}
	fh, flags, errno := n.Open(t.Context(), syscall.O_RDWR)
	require.Equal(t, syscall.Errno(0), errno)
	require.NotNil(t, fh)
	assert.Equal(t, uint32(fuse.FOPEN_KEEP_CACHE), flags)
	_, isNull := fh.(*NullWriteHandle)
	assert.True(t, isNull, "expected NullWriteHandle for REMOTE blk with write flag")
}

func TestFileNode_Open_RemoteBlkWithWriteFlag_ReadSkipsStaleLocal(t *testing.T) {
	const filename = "blk05455.dat"
	const off int64 = 117361686
	remoteData := []byte{0x91, 0xF0, 0xBA, 0xDE, 0xC4, 0x6C, 0x9C, 0x90, 0x3D, 0x9E, 0x9E, 0xDA, 0xE6, 0x7C, 0x16, 0x7B}

	st := newMockStore()
	st.files[filename] = &store.FileEntry{Filename: filename, State: store.FileStateRemote, Source: store.FileSourceServer}
	ca := newMockCache(t.TempDir())
	rc := newMockRemoteClient()
	rc.blockData[fmt.Sprintf("%s:%d", filename, off)] = remoteData
	fsys := makeTestFS(t, st, ca, rc, nil, nil)

	require.NoError(t, os.WriteFile(filepath.Join(fsys.localDir, filename), make([]byte, len(remoteData)), 0644))

	n := &FileNode{fs: fsys, filename: filename}
	fh, _, errno := n.Open(t.Context(), syscall.O_RDWR)
	require.Equal(t, syscall.Errno(0), errno)
	require.NotNil(t, fh)

	nh, ok := fh.(*NullWriteHandle)
	require.True(t, ok, "expected NullWriteHandle for REMOTE blk with write flag")

	dest := make([]byte, len(remoteData))
	result, readErrno := nh.Read(t.Context(), dest, off)
	require.Equal(t, syscall.Errno(0), readErrno)
	assert.Equal(t, remoteData, readResultBytes(t, result))
	assert.Equal(t, 1, rc.fetchBlockCallCount(), "must fetch from remote and skip stale local")
}

func TestFileNode_Open_RemoteRevReadOnly(t *testing.T) {
	st := newMockStore()
	st.files["rev00000.dat"] = &store.FileEntry{Filename: "rev00000.dat", State: store.FileStateRemote}
	ca := newMockCache(t.TempDir())
	testFS := makeTestFS(t, st, ca, newMockRemoteClient(), nil, nil)
	n := &FileNode{fs: testFS, filename: "rev00000.dat"}
	fh, _, errno := n.Open(t.Context(), syscall.O_RDONLY)
	require.Equal(t, syscall.Errno(0), errno)
	require.NotNil(t, fh)
	_, isFileHandle := fh.(*FileHandle)
	assert.True(t, isFileHandle)
}

func TestOpen_CachedRevWithWriteFlag(t *testing.T) {
	st := newMockStore()
	st.files["rev00100.dat"] = &store.FileEntry{Filename: "rev00100.dat", State: store.FileStateCached}
	ca := newMockCache(t.TempDir())
	testFS := makeTestFS(t, st, ca, newMockRemoteClient(), nil, nil)
	n := &FileNode{fs: testFS, filename: "rev00100.dat"}

	tests := []struct {
		name  string
		flags uint32
	}{
		{"O_WRONLY", syscall.O_WRONLY},
		{"O_RDWR", syscall.O_RDWR},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fh, flags, errno := n.Open(t.Context(), tt.flags)
			require.Equal(t, syscall.Errno(0), errno)
			require.NotNil(t, fh)
			assert.Equal(t, uint32(fuse.FOPEN_KEEP_CACHE), flags)
			_, isNull := fh.(*NullWriteHandle)
			assert.True(t, isNull, "expected NullWriteHandle for CACHED rev with write flag")
		})
	}
}

func TestOpen_CachedRevReadOnly(t *testing.T) {
	st := newMockStore()
	st.files["rev00100.dat"] = &store.FileEntry{Filename: "rev00100.dat", State: store.FileStateCached}
	ca := newMockCache(t.TempDir())
	testFS := makeTestFS(t, st, ca, newMockRemoteClient(), nil, nil)
	n := &FileNode{fs: testFS, filename: "rev00100.dat"}
	fh, flags, errno := n.Open(t.Context(), syscall.O_RDONLY)
	require.Equal(t, syscall.Errno(0), errno)
	require.NotNil(t, fh)
	assert.Equal(t, uint32(fuse.FOPEN_KEEP_CACHE), flags)
	_, isFileHandle := fh.(*FileHandle)
	assert.True(t, isFileHandle, "expected FileHandle for CACHED rev with read-only flag")
}

func TestOpen_CachedBlkWithWriteFlag(t *testing.T) {
	st := newMockStore()
	st.files["blk00100.dat"] = &store.FileEntry{Filename: "blk00100.dat", State: store.FileStateCached}
	ca := newMockCache(t.TempDir())
	testFS := makeTestFS(t, st, ca, newMockRemoteClient(), nil, nil)
	n := &FileNode{fs: testFS, filename: "blk00100.dat"}
	fh, flags, errno := n.Open(t.Context(), syscall.O_RDWR)
	require.Equal(t, syscall.Errno(0), errno)
	require.NotNil(t, fh)
	assert.Equal(t, uint32(fuse.FOPEN_KEEP_CACHE), flags)
	_, isNull := fh.(*NullWriteHandle)
	assert.True(t, isNull, "expected NullWriteHandle for CACHED blk with write flag")
}

func TestOpen_LocalFinalizedRevWithWriteFlag(t *testing.T) {
	st := newMockStore()
	st.files["rev00200.dat"] = &store.FileEntry{Filename: "rev00200.dat", State: store.FileStateLocalFinalized}
	ca := newMockCache(t.TempDir())
	testFS := makeTestFS(t, st, ca, newMockRemoteClient(), nil, nil)
	n := &FileNode{fs: testFS, filename: "rev00200.dat"}

	tests := []struct {
		name  string
		flags uint32
	}{
		{"O_WRONLY", syscall.O_WRONLY},
		{"O_RDWR", syscall.O_RDWR},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fh, flags, errno := n.Open(t.Context(), tt.flags)
			require.Equal(t, syscall.Errno(0), errno)
			require.NotNil(t, fh)
			assert.Equal(t, uint32(fuse.FOPEN_KEEP_CACHE), flags)
			_, isNull := fh.(*NullWriteHandle)
			assert.True(t, isNull, "expected NullWriteHandle for LOCAL_FINALIZED rev with write flag")
		})
	}
}

func TestOpen_LocalFinalizedRevReadOnly(t *testing.T) {
	st := newMockStore()
	st.files["rev00200.dat"] = &store.FileEntry{Filename: "rev00200.dat", State: store.FileStateLocalFinalized}
	ca := newMockCache(t.TempDir())
	testFS := makeTestFS(t, st, ca, newMockRemoteClient(), nil, nil)
	n := &FileNode{fs: testFS, filename: "rev00200.dat"}
	fh, flags, errno := n.Open(t.Context(), syscall.O_RDONLY)
	require.Equal(t, syscall.Errno(0), errno)
	require.NotNil(t, fh)
	assert.Equal(t, uint32(fuse.FOPEN_KEEP_CACHE), flags)
	_, isFileHandle := fh.(*FileHandle)
	assert.True(t, isFileHandle, "expected FileHandle for LOCAL_FINALIZED rev with read-only flag")
}

func TestOpen_LocalFinalizedBlkWithWriteFlag(t *testing.T) {
	st := newMockStore()
	st.files["blk00200.dat"] = &store.FileEntry{Filename: "blk00200.dat", State: store.FileStateLocalFinalized}
	ca := newMockCache(t.TempDir())
	testFS := makeTestFS(t, st, ca, newMockRemoteClient(), nil, nil)
	n := &FileNode{fs: testFS, filename: "blk00200.dat"}
	fh, flags, errno := n.Open(t.Context(), syscall.O_RDWR)
	require.Equal(t, syscall.Errno(0), errno)
	require.NotNil(t, fh)
	assert.Equal(t, uint32(fuse.FOPEN_KEEP_CACHE), flags)
	_, isNull := fh.(*NullWriteHandle)
	assert.True(t, isNull, "expected NullWriteHandle for LOCAL_FINALIZED blk with write flag")
}

func TestFileNode_Setattr_TruncateRemoteRev(t *testing.T) {
	st := newMockStore()
	st.files["rev00000.dat"] = &store.FileEntry{Filename: "rev00000.dat", State: store.FileStateRemote}
	ca := newMockCache(t.TempDir())
	testFS := makeTestFS(t, st, ca, newMockRemoteClient(), nil, nil)
	n := &FileNode{fs: testFS, filename: "rev00000.dat"}
	var in fuse.SetAttrIn
	in.Valid |= fuse.FATTR_SIZE
	in.Size = 0
	var out fuse.AttrOut
	errno := n.Setattr(t.Context(), nil, &in, &out)
	assert.Equal(t, syscall.Errno(0), errno)
}
