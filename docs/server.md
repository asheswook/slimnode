# Server Setup Guide

This guide covers running your own SlimNode archive server. If you just want to run a SlimNode client using the [public server](https://slimnode.pororo.ro), see the [Quick Start](../README.md#quick-start) in the README.

## Prerequisites

- A **fully synced** Bitcoin node with **`-blocksxor=0`**
- Go 1.22+ (build only)

> **Important:** If your existing node was synced with the default XOR enabled, you must resync with `-blocksxor=0`. Bitcoin Core uses a per-node random XOR key, making existing block data incompatible with XOR-disabled mode.

## Deployment Modes

Two deployment modes are supported: self-hosted and S3+CDN.

### Self-Hosted

The server serves block files directly over HTTP:

```
┌──────────────┐         ┌──────────────────┐         ┌──────────────┐
│  bitcoind    │  reads  │  slimnode mount  │  HTTP   │  slimnode    │
│              │ ──────> │  (FUSE)          │ ──────> │  server      │
│ -blocksdir=  │         │                  │         │  serve       │
│  /mnt/blocks │         │  local cache +   │         │              │
│ -blocksxor=0 │         │  remote fetch    │         │  blocks dir  │
└──────────────┘         └──────────────────┘         └──────────────┘
```

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

### S3+CDN

Block files are uploaded to S3 and served via CDN. The `sync` daemon handles scanning, uploading, and manifest generation. A lightweight `serve` instance provides the manifest API for clients.

```
┌──────────────┐         ┌──────────────────┐  files  ┌──────────────┐
│  bitcoind    │  reads  │  slimnode mount  │ ──────> │  CDN (S3)    │
│              │ ──────> │  (FUSE)          │         └──────────────┘
│ -blocksdir=  │         │                  │ manifest┌──────────────┐
│  /mnt/blocks │         │  local cache +   │ ──────> │  slimnode    │
│ -blocksxor=0 │         │  CDN fetch       │         │  server      │
└──────────────┘         └──────────────────┘         │  serve       │
                                                      └──────────────┘
                         ┌──────────────────┐  upload ┌──────────────┐
                         │  slimnode-server │ ──────> │  S3 bucket   │
                         │  sync (daemon)   │         │  + CDN       │
                         └──────────────────┘         └──────────────┘
```

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

The client auto-detects which mode to use. When the client fetches the manifest and sees a `base_url`, file downloads go directly to the CDN. Otherwise, files are fetched from the self-hosted server.

## API Endpoints

| Endpoint | Description |
|---|---|
| `GET /v1/health` | Health check (`{"status":"ok"}`) |
| `GET /v1/manifest` | Manifest JSON (supports `If-None-Match` / `ETag`) |
| `GET /v1/file/{name}` | Block file download (supports `Range`, returns `X-SHA256` header) |
| `GET /v1/blockmap/{name}` | Blockmap file download (binary) |

## Build

```bash
git clone https://github.com/asheswook/bitcoin-slimnode.git
cd bitcoin-slimnode
make build

# Server binary: bin/slimnode-server
```

## Reference

Full configuration options, CLI commands, storage layout, manifest management, and troubleshooting: [reference.md](reference.md)
