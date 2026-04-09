# SlimNode Reference

## Configuration

### Client Config (`config.conf`)

All options can be set via config file or CLI flags. CLI flags override config file values.

```ini
[general]
general.chain = mainnet              # mainnet | testnet | testnet4 | signet | regtest
general.cache-dir = ~/.slimnode/cache
general.local-dir = ~/.slimnode/local
general.mount-point = /mnt/bitcoin-blocks  # required
general.bitcoin-datadir = ~/.bitcoin       # for blocks/index symlink
general.log-level = info             # debug | info | warn | error

[server]
server.url = http://archive:8080     # required
server.request-timeout = 30s
server.retry-count = 3

[cache]
cache.max-size-gb = 2                # LRU cache limit (1 GB minimum; used as fallback when range fetch fails)
cache.min-keep-recent = 5            # keep at least N recent files

[compaction]
compaction.trigger = auto            # auto | scheduled | manual
compaction.threshold = 85            # auto-trigger at N% disk usage
compaction.pre-download = true       # pre-download before compaction
```

### Server Flags (`slimnode-server serve`)

| Flag | Default | Description |
|---|---|---|
| `--blocks-dir` | *(required)* | Path to Bitcoin `blocks/` directory |
| `--manifest` | `manifest.json` | Path to generated manifest file |
| `--listen` | `:8080` | HTTP listen address |
| `--blockmap-dir` | *(empty)* | Directory containing blockmap files |
| `--snapshot-dir` | *(empty)* | Directory containing snapshot files |
| `--chain` | `mainnet` | Bitcoin chain (used when regenerating manifest) |
| `--scan-interval` | `0` (disabled) | Interval for automatic manifest regeneration (e.g. `10m`) |

### Server Flags (`slimnode-server sync`)

| Flag | Default | Description |
|---|---|---|
| `--blocks-dir` | *(required)* | Path to Bitcoin `blocks/` directory |
| `--chain` | `mainnet` | Bitcoin chain name |
| `--bucket` | *(required)* | S3 bucket name |
| `--endpoint` | *(empty)* | S3-compatible endpoint URL (for R2, DO Spaces, etc.) |
| `--region` | `us-east-1` | S3 region |
| `--base-url` | *(required)* | CDN base URL written into the manifest |
| `--scan-interval` | `10m` | Interval between scans for new finalized files |
| `--blockmap-dir` | *(empty)* | Local blockmap directory to upload alongside blk files |
| `--manifest` | *(empty)* | Also write manifest to this local path (for serve) |
| `--path-style` | `false` | Use path-style S3 addressing |
| `--storage-class` | `STANDARD_IA` | S3 storage class for block files. Use `STANDARD` for Backblaze B2 or Wasabi. |

### Server Flags (`slimnode-server manifest-gen`)

| Flag | Default | Description |
|---|---|---|
| `--blocks-dir` | *(required)* | Path to Bitcoin `blocks/` directory |
| `--output` | `manifest.json` | Output manifest file path |
| `--chain` | `mainnet` | Bitcoin chain name |
| `--blockmap-dir` | *(empty)* | Directory containing blockmap files |
| `--snapshot-dir` | *(empty)* | Directory containing snapshot files |

---

## CLI Commands

### `slimnode`

```
slimnode init      Initialize (fetch manifest, download blockmaps, create directory structure)
slimnode mount     Mount FUSE filesystem (use --background to daemonize)
slimnode stop      Stop the running SlimNode daemon
slimnode status    Show file state summary (local/cached/remote counts)
```

### `slimnode-server`

```
slimnode-server manifest-gen   Generate manifest.json from blocks directory
slimnode-server blockmap-gen   Generate blockmap files from blocks directory
slimnode-server serve          Serve block files and manifest over HTTP
slimnode-server sync           Sync finalized files to S3 and update manifest
slimnode-server snapshot       Create blocks/index snapshot (.tar.zst)
```

---

## Storage Layout

### Client

```
~/.slimnode/
├── config.conf                # configuration
├── slimnode.pid               # daemon PID (when running with --background)
├── slimnode.log               # daemon log output
├── cache/
│   ├── slimnode.db            # SQLite metadata (file states, access times)
│   ├── manifest.json          # cached server manifest
│   ├── blockmaps/             # block-level index files (for range fetching)
│   ├── blk00000.dat           # cached remote files (LRU evicted)
│   ├── rev00000.dat
│   └── ...
└── local/
    ├── index/                 # blocks/index LevelDB (rebuilt by -reindex)
    │   ├── CURRENT
    │   └── *.ldb
    ├── blk00500.dat           # active files (written by bitcoind)
    └── rev00500.dat

~/.bitcoin/blocks/
└── index -> ~/.slimnode/local/index  # symlink created by slimnode init

/mnt/bitcoin-blocks/           # FUSE mount (union of local + cache + remote)
├── index/                     # loopback to local/index/
├── blk00000.dat
├── blk00001.dat
├── ...
├── xor.dat                    # 8-byte no-op XOR key (bitcoind reads even with -blocksxor=0)
└── .lock                      # fcntl lock file (backed by real file for flock support)
```

### Server (Self-Hosted)

```
~/.bitcoin/blocks/             # standard bitcoind blocks directory
├── blk00000.dat
├── blk00001.dat
├── ...
├── rev00000.dat
└── rev00001.dat

manifest.json                  # generated by manifest-gen or serve --scan-interval
```

### Server (S3+CDN)

```
S3 Bucket:
├── blk00000.dat               # uploaded by slimnode-server sync
├── blk00001.dat
├── ...
├── rev00000.dat
├── rev00001.dat
└── manifest.json              # includes base_url pointing to CDN
```

---

## Updating the Manifest

Three options for keeping the manifest up to date as new block files finalize:

1. **Auto-reload** (self-hosted): `serve --scan-interval 10m` detects new finalized files and regenerates the manifest automatically.
2. **Sync daemon** (S3+CDN): `sync` handles everything - scans, uploads files to S3, and regenerates the manifest on each cycle.
3. **Manual**: Run `manifest-gen` and restart `serve`.

```bash
# Manual regeneration (option 3)
bin/slimnode-server manifest-gen \
  --blocks-dir ~/.bitcoin/blocks \
  --output manifest.json
```

The client's manifest poller checks `GET /v1/manifest` every 10 minutes using ETag-based conditional requests.

---

## Troubleshooting

### P2P peers show `synced_headers: -1`

**Symptom:** All peers show `synced_headers: -1, synced_blocks: -1` and no new blocks are received during reindex.

**Cause:** Normal during `-reindex`. bitcoind is in initial block download mode and does not exchange headers with peers until reindex completes.

**Fix:** Wait for reindex to complete, then restart bitcoind. P2P should normalize after restart.

### `Cannot obtain a lock on data directory`

**Symptom:** bitcoind fails to start with a lock error when using the FUSE mount.

**Cause:** The `.lock` file in the blocks directory needs `flock()` support. FUSE must serve this as a real file, not a virtual stub.

**Fix:** This is handled automatically by SlimNode's FUSE implementation. Ensure you're using the latest version of `slimnode`.

### `AutoFile::read: end of file` (xor.dat)

**Symptom:** bitcoind fails with an EOF error reading `xor.dat` from the FUSE mount.

**Cause:** Even with `-blocksxor=0`, bitcoind reads `xor.dat` and expects exactly 8 bytes.

**Fix:** This is handled automatically by SlimNode's FUSE implementation, which serves `xor.dat` as 8 zero bytes. Ensure you're using the latest version.

### Electrs crashes with `Block not found on disk`

**Symptom:** Electrs panics during initial indexing with `getblock RPC error: Block not found on disk`.

**Cause:** Electrs defaults to 44 precache threads, which overwhelms Bitcoin Core's RPC work queue. Requests that exceed the queue are rejected, and electrs treats the rejection as a fatal error.

**Fix:** Limit electrs precache threads:

```
electrs --precache-threads 4 ...
```

### Mempool backend fails with `No such mempool transaction`

**Symptom:** Mempool backend loops with errors like `Cannot fetch tx <txid>. Reason: No such mempool transaction. Use -txindex`.

**Cause:** Mempool backend calls `getrawtransaction` to look up confirmed transactions by txid. Without `-txindex`, Bitcoin Core can only look up transactions that are currently in the mempool.

**Fix:** Start bitcoind with `-txindex=1`:

```bash
bitcoind \
  -blocksdir=/mnt/bitcoin-blocks \
  -blocksxor=0 \
  -txindex=1 \
  -datadir=~/.bitcoin
```

Note: enabling `-txindex` on an existing node triggers a background index build that may take several minutes to hours depending on chain height. The node remains usable during this process.
