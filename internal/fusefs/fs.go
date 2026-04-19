package fusefs

import (
	"sync"
	"time"

	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
	"golang.org/x/sync/singleflight"

	"github.com/asheswook/bitcoin-slimnode/internal/blockcache"
	"github.com/asheswook/bitcoin-slimnode/internal/blockmap"
	"github.com/asheswook/bitcoin-slimnode/internal/cache"
	"github.com/asheswook/bitcoin-slimnode/internal/manifest"
	"github.com/asheswook/bitcoin-slimnode/internal/store"
)

// FS is the SlimNode FUSE filesystem.
type FS struct {
	mountPoint  string
	st          store.Store
	ca          cache.Cache
	rc          RemoteClient
	fetchPolicy *FetchPolicy
	manifest    *manifest.Manifest
	localDir    string
	indexDir    string
	downloads   singleflight.Group
	downloadSem chan struct{}
	mu          sync.RWMutex
	finCh       chan string
	server      *fuse.Server

	writeMu            sync.Mutex
	activeWriteHandles map[string]int

	// Block-level fetch support
	blockmaps    map[string]*blockmap.Blockmap
	blockmapsMu  sync.RWMutex
	bc           blockcache.BlockCache
	blockDL      singleflight.Group
	noBlockmap   map[string]bool // negative cache: files known to have no blockmap
	noBlockmapMu sync.RWMutex

	indexLoopback fs.InodeEmbedder
}

// New creates a new FS.
func New(mountPoint, localDir, indexDir string, st store.Store, ca cache.Cache, rc RemoteClient, m *manifest.Manifest, bc blockcache.BlockCache, blockmaps map[string]*blockmap.Blockmap, fetchPolicies ...*FetchPolicy) *FS {
	bmField := blockmaps
	if bmField == nil {
		bmField = make(map[string]*blockmap.Blockmap)
	}

	var policy *FetchPolicy
	if len(fetchPolicies) > 0 {
		policy = fetchPolicies[0]
	}
	if policy == nil {
		policy = NewFetchPolicy(FetchPolicyConfig{})
	}

	var loopback fs.InodeEmbedder
	if indexDir != "" {
		loopback, _ = fs.NewLoopbackRoot(indexDir)
	}

	return &FS{
		mountPoint:         mountPoint,
		localDir:           localDir,
		indexDir:           indexDir,
		st:                 st,
		ca:                 ca,
		rc:                 rc,
		fetchPolicy:        policy,
		manifest:           m,
		bc:                 bc,
		blockmaps:          bmField,
		noBlockmap:         make(map[string]bool),
		downloadSem:        make(chan struct{}, 4),
		finCh:              make(chan string, 64),
		activeWriteHandles: make(map[string]int),
		indexLoopback:      loopback,
	}
}

// UpdateManifest atomically replaces the current manifest.
func (f *FS) UpdateManifest(m *manifest.Manifest) {
	f.mu.Lock()
	f.manifest = m
	f.mu.Unlock()
}

// Mount mounts the FUSE filesystem and returns the server.
func Mount(f *FS) (*fuse.Server, error) {
	one := time.Second
	hour := time.Hour

	opts := &fs.Options{
		MountOptions: fuse.MountOptions{
			AllowOther:  true,
			FsName:      "slimnode",
			Name:        "slimnode",
			DirectMount: true,
			EnableLocks: true,
		},
		AttrTimeout:  &one,
		EntryTimeout: &hour,
	}

	root := &RootNode{fs: f}
	server, err := fs.Mount(f.mountPoint, root, opts)
	if err != nil {
		return nil, err
	}
	f.server = server
	return server, nil
}
