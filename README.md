# SlimNode

Bitcoin full node with ~72% storage reduction. Run bitcoind with ~255 GB instead of ~920 GB by serving historical block files from a remote archive server via FUSE.

## Why SlimNode

A Bitcoin full node accumulates ~920 GB of block data, growing by ~9 GB per month. About 720 GB of this is `blk*.dat` files - raw block data written once and almost never read again. Once a block file reaches 128 MB and is sealed, it never changes.

SlimNode treats historical block files as what they are: immutable, rarely-accessed archives. They live on an archive server and are fetched on demand via FUSE. Only recently-written active files stay local.

**Full validation.** Every transaction, every signature, every block - validated by bitcoind itself, exactly as on a standard node. SlimNode changes *where* block data is stored, not *how* it is validated.

**No Bitcoin Core modifications.** Bitcoin Core already supports `-blocksdir` to separate block files from other data. SlimNode mounts a FUSE filesystem at that path. bitcoind cannot tell the difference.

**Trustless.** Fetched block data is verified by Bitcoin Core's own mechanisms: proof-of-work validation and block hash comparison on every read. A malicious or corrupted server cannot cause silent data corruption.

**Implementation-agnostic.** SlimNode works with Bitcoin Core, Bitcoin Knots, and any other implementation that supports `-blocksdir` and the standard block file format.

For the full design rationale, trust model, privacy considerations, and Q&A, see [idea](docs/idea.md).

## How It Works

```
┌──────────────┐         ┌──────────────────┐         ┌───────────────┐
│  bitcoind    │  reads  │  slimnode mount  │  HTTP   │  SlimNode     │
│              │ ──────> │  (FUSE)          │ ──────> │  server       │
│ -blocksdir=  │         │                  │         │               │
│  /mnt/blocks │         │  local cache +   │         │  (public or   │
│ -blocksxor=0 │         │  remote fetch    │         │  self-hosted) │
└──────────────┘         └──────────────────┘         └───────────────┘
```

- **Active blocks** (recently written by bitcoind): stored locally as normal files
- **Finalized blocks** (full 128 MB blk/rev files): fetched from archive server on demand, cached on local disk with LRU eviction
- **FUSE mount**: transparently serves both local and remote files to bitcoind

## Prerequisites

- Linux (FUSE3 support required)
- `fusermount3` installed on the client machine

```bash
# Debian/Ubuntu
sudo apt install fuse3

# RHEL/Fedora
sudo dnf install fuse3
```

## Quick Start

A free public mainnet server is available at `https://slimnode.pororo.ro`. The steps below use this server. To run your own, see the [Server Setup Guide](docs/server.md).

### 1. Download

Download the latest binary from [GitHub Releases](https://github.com/asheswook/bitcoin-slimnode/releases):

```bash
curl -LO https://github.com/asheswook/bitcoin-slimnode/releases/latest/download/slimnode-linux-amd64
chmod +x slimnode-linux-amd64
sudo mv slimnode-linux-amd64 /usr/local/bin/slimnode
```

For arm64, replace `amd64` with `arm64` in the commands above. Or [build from source](#building-from-source).

### 2. Configure and Mount

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
server.url = https://slimnode.pororo.ro
server.request-timeout = 30s
server.retry-count = 3

[cache]
cache.max-size-gb = 2
cache.min-keep-recent = 5

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
# creates symlink: ~/.bitcoin/blocks/index -> ~/.slimnode/local/index
slimnode init --config ~/.slimnode/config.conf

# Mount the FUSE filesystem (runs as background daemon)
slimnode mount --config ~/.slimnode/config.conf --background
```

### 3. Run bitcoind

With the FUSE mount active, start bitcoind with `-reindex`. This scans all block files through the FUSE layer - downloading them from the archive server - and rebuilds the local block index and chainstate from scratch.

```bash
bitcoind \
  -blocksdir=/mnt/bitcoin-blocks \
  -blocksxor=0 \
  -reindex \
  -datadir=~/.bitcoin
```

> **Expected time:** Reindex downloads ~720 GB of block data through FUSE. The time depends on both **bandwidth** (total data volume) and **server latency** (each block file triggers HTTP requests). Typical range: 3–14 days. CPU-bound chainstate validation adds additional time on top of that. This is a one-time cost.

```
Once reindex completes, restart bitcoind normally:

```bash
bitcoin-cli stop

bitcoind \
  -blocksdir=/mnt/bitcoin-blocks \
  -blocksxor=0 \
  -datadir=~/.bitcoin
```

To stop the daemon:

```bash
slimnode stop --config ~/.slimnode/config.conf
```

## Building from Source

Requires Go 1.22+.

```bash
git clone https://github.com/asheswook/bitcoin-slimnode.git
cd bitcoin-slimnode
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

## Reference

Full configuration options, CLI commands, storage layout, manifest management, and troubleshooting: [docs/reference.md](docs/reference.md)

## Contributing

SlimNode is under active development. Issues, bug reports, and pull requests are welcome.

Before contributing code, please open an issue to discuss the change. This is an experimental project - major components may shift.

```bash
# Run tests
make test

# Run integration tests (requires FUSE)
make test-integration

# Lint
make lint
```

## License

MIT - see [LICENSE](LICENSE).
