# SlimNode

Bitcoin full node with ~72% storage reduction. Run bitcoind with ~255 GB instead of ~920 GB by serving historical block files from a remote archive server via FUSE.

## Why SlimNode

A Bitcoin full node accumulates ~920 GB of block data, growing by ~9 GB per month. About 720 GB of this is `blk*.dat` files — raw block data written once and almost never read again. Once a block file reaches 128 MB and is sealed, it never changes.

SlimNode treats historical block files as what they are: immutable, rarely-accessed archives. They live on an archive server and are fetched on demand via FUSE. Only recently-written active files stay local.

**Full validation.** Every transaction, every signature, every block — validated by bitcoind itself, exactly as on a standard node. SlimNode changes *where* block data is stored, not *how* it is validated.

**No Bitcoin Core modifications.** Bitcoin Core already supports `-blocksdir` to separate block files from other data. SlimNode mounts a FUSE filesystem at that path. bitcoind cannot tell the difference.

**Trustless.** Fetched block data is verified by Bitcoin Core's own mechanisms: proof-of-work validation and block hash comparison on every read. A malicious or corrupted server cannot cause silent data corruption.

**Implementation-agnostic.** SlimNode works with Bitcoin Core, Bitcoin Knots, and any other implementation that supports `-blocksdir` and the standard block file format. The choice of software is entirely yours.

For the full design rationale, trust model, privacy considerations, and Q&A, see [idea](docs/idea.md).

## How It Works

Two deployment modes are supported: self-hosted and S3+CDN.

**Self-Hosted** — the server serves block files directly over HTTP:

```
┌──────────────┐         ┌──────────────────┐         ┌──────────────┐
│  bitcoind    │  reads  │  slimnode mount  │  HTTP   │  slimnode    │
│              │ ──────> │  (FUSE)          │ ──────> │  server      │
│ -blocksdir=  │         │                  │         │  serve       │
│  /mnt/blocks │         │  local cache +   │         │              │
│ -blocksxor=0 │         │  remote fetch    │         │  blocks dir  │
└──────────────┘         └──────────────────┘         └──────────────┘
```

**S3+CDN** — block files are served from a CDN, the server only provides the manifest:

```
┌──────────────┐         ┌─────────────────┐  files   ┌──────────────┐
│  bitcoind    │  reads  │  slimnode mount  │ ──────> │  CDN (S3)    │
│              │ ──────> │  (FUSE)          │         └──────────────┘
│ -blocksdir=  │         │                  │ manifest┌──────────────┐
│  /mnt/blocks │         │  local cache +   │ ──────> │  slimnode    │
│ -blocksxor=0 │         │  CDN fetch       │         │  server      │
└──────────────┘         └──────────────────┘         │  serve       │
                                                      └──────────────┘
                         ┌──────────────────┐  upload  ┌──────────────┐
                         │  slimnode-server │ ──────> │  S3 bucket   │
                         │  sync (daemon)   │         │  + CDN       │
                         └──────────────────┘         └──────────────┘
```

The client auto-detects which mode to use. If the manifest contains a `base_url` field, file downloads go directly to the CDN. Otherwise, files are fetched from the self-hosted server.

- **Active blocks** (recently written by bitcoind): stored locally as normal files
- **Finalized blocks** (full 128 MB blk/rev files): fetched from archive server or CDN on demand, cached on local disk with LRU eviction
- **FUSE mount**: transparently serves both local and remote files to bitcoind

## Prerequisites

- Linux (FUSE3 support required)
- Go 1.22+ (build only)
- A synced Bitcoin full node (for the archive server)
- `fusermount3` installed on the client machine

```bash
# Debian/Ubuntu
sudo apt install fuse3

# RHEL/Fedora
sudo dnf install fuse3
```

## Build

```bash
git clone https://github.com/asheswook/bitcoin-lfn.git
cd bitcoin-lfn
make build

# Binaries:
#   bin/slimnode         - client (FUSE + daemon)
#   bin/slimnode-server  - server tools
```

Or build manually:

```bash
CGO_ENABLED=0 go build -o bin/slimnode ./cmd/slimnode
CGO_ENABLED=0 go build -o bin/slimnode-server ./cmd/slimnode-server
```

## Quick Start

> **Public archive server coming soon.** A free public mainnet SlimNode server will be available shortly, so you can skip Step 1 and go straight to Step 2. Watch this repository for updates.

### 1. Set Up the Archive Server

The archive server must run a **fully synced** Bitcoin node with **`-blocksxor=0`**. This is required because SlimNode does not implement XOR obfuscation — the server's block files must be unobfuscated.

> **Important:** If your existing node was synced with the default XOR enabled, you must resync with `-blocksxor=0`. Bitcoin Core uses a per-node random XOR key, making existing block data incompatible with XOR-disabled mode.

Choose one of two deployment modes:

**Option A: Self-Hosted**

```bash
# Generate initial manifest
bin/slimnode-server manifest-gen \
  --blocks-dir <bitcoind block dir (e.g. ~/.bitcoin/blocks)> \
  --output manifest.json \
  --chain mainnet

# Start the HTTP server
# --scan-interval automatically regenerates the manifest as new files finalize
bin/slimnode-server serve \
  --blocks-dir <bitcoind block dir (e.g. ~/.bitcoin/blocks)> \
  --manifest manifest.json \
  --chain mainnet \
  --scan-interval 10m \
  --listen :8080
```

**Option B: S3+CDN**

Block files are uploaded to S3 and served via CDN. The `sync` daemon handles scanning, uploading, and manifest generation. A lightweight `serve` instance provides the manifest API for clients.

```bash
# Start the sync daemon
bin/slimnode-server sync \
  --blocks-dir <bitcoind block dir (e.g. ~/.bitcoin/blocks)> \
  --bucket my-slimnode-bucket \
  --base-url https://cdn.example.com \
  --endpoint https://account-id.r2.cloudflarestorage.com \
  --manifest manifest.json \
  --scan-interval 10m

# Start serve for manifest API (reads manifest written by sync)
bin/slimnode-server serve \
  --blocks-dir <bitcoind block dir (e.g. ~/.bitcoin/blocks)> \
  --manifest manifest.json \
  --listen :8080
```

The `--endpoint` flag is for S3-compatible providers (Cloudflare R2, Backblaze, Wasabi, MinIO). For AWS S3, omit it. AWS credentials are resolved via the standard SDK chain (environment variables, `~/.aws/credentials`, IAM role).

When the client fetches the manifest and sees a `base_url`, file downloads go directly to the CDN instead of the self-hosted server.

The server exposes:

| Endpoint | Description |
|---|---|
| `GET /v1/health` | Health check (`{"status":"ok"}`) |
| `GET /v1/manifest` | Manifest JSON (supports `If-None-Match` / `ETag`) |
| `GET /v1/file/{name}` | Block file download (supports `Range`, returns `X-SHA256` header) |
| `GET /v1/blockmap/{name}` | Blockmap file download (binary) |

### 2. Set Up the SlimNode Client

On the machine where you want to run bitcoind with reduced storage:

```bash
# Create config file
mkdir -p ~/.slimnode
cat > ~/.slimnode/config.conf << 'EOF'
[general]
general.chain = mainnet
general.cache-dir = ~/.slimnode/cache
general.local-dir = ~/.slimnode/local
general.mount-point = /mnt/bitcoin-blocks
general.bitcoin-datadir = ~/.bitcoin
general.log-level = info

[server]
server.url = http://your-archive-server:8080
server.request-timeout = 30s
server.retry-count = 3

[cache]
cache.max-size-gb = 50
cache.min-keep-recent = 10

[compaction]
compaction.trigger = auto
compaction.threshold = 85
compaction.pre-download = true
EOF

# Create mount point
sudo mkdir -p /mnt/bitcoin-blocks
sudo chown $USER:$USER /mnt/bitcoin-blocks

# Create local index directory
# bitcoind will rebuild blocks/index here during -reindex
mkdir -p ~/.slimnode/local/index

# Initialize: downloads manifest and blockmaps,
# creates symlink: ~/.bitcoin/blocks/index → ~/.slimnode/local/index
bin/slimnode init --config ~/.slimnode/config.conf

# Mount the FUSE filesystem
bin/slimnode mount --config ~/.slimnode/config.conf --foreground
```

### 3. Run bitcoind

With the FUSE mount active, start bitcoind with `-reindex`. This scans all block files through the FUSE layer — downloading them from the archive server — and rebuilds the local block index and chainstate from scratch.

```bash
bitcoind \
  -blocksdir=/mnt/bitcoin-blocks \
  -blocksxor=0 \
  -reindex \
  -datadir=~/.bitcoin
```

> **Expected time:** Downloading ~720 GB through FUSE typically takes 3–5 days depending on network speed. CPU-bound chainstate validation adds additional time on top of that. This is a one-time cost.

Once reindex completes, restart bitcoind normally:

```bash
bitcoin-cli stop

bitcoind \
  -blocksdir=/mnt/bitcoin-blocks \
  -blocksxor=0 \
  -datadir=~/.bitcoin
```

## Reference

Full configuration options, CLI commands, storage layout, manifest management, and troubleshooting: [docs/reference.md](docs/reference.md)

---

## Contributing

SlimNode is under active development. Issues, bug reports, and pull requests are welcome.

Before contributing code, please open an issue to discuss the change. This is an experimental project — major components may shift.

```bash
# Run tests
make test

# Run integration tests (requires FUSE)
make test-integration

# Lint
make lint
```

## License

MIT — see [LICENSE](LICENSE).
