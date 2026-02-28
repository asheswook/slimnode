# The SlimNode Idea

> SlimNode is under active development. This document is intended for technical review and community feedback — not as documentation for a production-ready release.

## The Problem

Running a Bitcoin full node used to be within reach of hobbyists. Today, a 2 TB home node kit costs upwards of $700 — and that price keeps climbing as Bitcoin's block archive grows at roughly 9 GB per month. The 920 GB storage requirement is a real barrier for anyone who wants to run their own node on modest hardware.

The common workaround is a pruned node, which deletes old blocks to save space. The trade-off is steep: no Electrum server, no Mempool explorer, no historical transaction queries. You're left with a node that validates new blocks but can't do much else.

## The Core Insight

Bitcoin's block data has an unusual property: **once written, it never changes**. A block file sealed in 2015 is byte-for-byte identical today. These files are written once and almost never read again — only during reorgs (rare, typically 1–2 blocks) or specific historical queries.

SlimNode's premise: keep what's actively needed locally, fetch the historical archive on demand.

- **Local (~14 GB minimum):** chainstate (UTXO set), block index, recently received blocks
- **Remote (~720 GB):** historical block files (`blk*.dat`, `rev*.dat`)

A FUSE filesystem bridges the two. `bitcoind` reads the mount as a normal directory and cannot tell the difference.

## How It Compares

|  | Full node | Pruned node | SlimNode |
|---|---|---|---|
| Local storage | ~920 GB | ~10 GB | ~20–255 GB |
| Full transaction validation | ✅ | ✅ | ✅ |
| Electrum server | ✅ | ❌ | ✅ |
| Mempool explorer | ✅ | ❌ | ✅ |
| Historical block queries | ✅ | ❌ | ✅ (via FUSE) |
| No Bitcoin Core changes needed | ✅ | ✅ | ✅ |

## Storage Requirements by Use Case

| Setup | Approximate storage | Example hardware |
|---|---|---|
| Bitcoin Core only | ~20 GB | 32–64 GB SD card or USB drive |
| Bitcoin Core + Electrs | ~65 GB | 128 GB SSD |
| Bitcoin Core + Mempool + Electrs | ~255 GB | 256 GB SSD |

For reference, a standard full node today requires 920 GB and grows indefinitely.

## A Stepping Stone

Running a self-contained full node — all data stored locally, no external dependencies — is always the ideal. You depend on no one, have instant access to all historical data, and impose no trust assumptions on anyone. If you can afford 1–2 TB of fast storage, that is the right path.

SlimNode is for everyone else: someone setting up their first node on a modest machine, a user on a tight budget, or someone who wants to run a full node without buying 2 TB of storage upfront. Block validation on a SlimNode is still full validation — every transaction, every signature, every block — the only trade-off is that historical block retrieval depends on an archive server.

The idea is to run SlimNode now, learn how Bitcoin nodes work, and move to a standard full node when storage gets cheaper.

## Compatible With Any Bitcoin Core-Compatible Implementation

SlimNode operates entirely at the filesystem layer — it has no knowledge of which Bitcoin implementation is running above it. This means it works with:

- **Bitcoin Core**
- **Bitcoin Knots**
- Any other implementation that uses `-blocksdir` and the standard block file format

SlimNode doesn't care which one you run.

## Who Provides the Archive?

The "archive server" is flexible. It can be:

- A public server run by a community member
- A commercial object storage service (Cloudflare R2, Backblaze B2, Wasabi)
- A friend's home node (another Umbrel, Raspberry Pi, etc.)
- **Your own existing full node**, acting as the archive for a second, leaner machine

For anyone running a public archive server, the economics are favorable. Bitcoin's block files are immutable and CDN-friendly (`Cache-Control: public, immutable`). Services like Cloudflare R2 offer zero egress fees, making it viable to serve many nodes for roughly $12/month in storage costs alone.

---

## Q&A

### Do I need to trust the archive server?

**No.** Bitcoin Core verifies every byte it reads. When SlimNode fetches a block file:

1. Every block header is checked for valid proof-of-work
2. Each block's content is hashed and compared against the expected hash stored in the local block index
3. All transactions and scripts are validated during chainstate construction

If the archive server provides corrupted or tampered data, Bitcoin Core rejects it immediately. A block's hash changes if even a single byte is altered — there is no way to construct a convincing fake block without redoing its entire proof-of-work.

### What can a malicious archive server actually do?

| Attack | Possible? | Why not |
|---|---|---|
| Fake your balance | ❌ | The UTXO set lives locally |
| Censor your transactions | ❌ | New transactions go via P2P directly |
| Cause you to accept a double-spend | ❌ | Validation happens locally |
| Tamper with historical block data | ❌ | Block hash changes → rejected immediately |
| Deny service (serve garbage data) | ⚠️ | Falls back to "block unavailable" — same as a pruned node. Switch servers. |

**If an attacker tampers with archive data, you will know.** And there is little incentive to try — the UTXO set (the source of truth for balances) lives locally and cannot be influenced by the archive server.

### How does verification work during initial setup?

SlimNode bootstraps by running `bitcoind -reindex`. Bitcoin Core downloads all historical block files through the FUSE layer and independently validates every transaction from genesis. Bad data is rejected as it is encountered — there is no deferred trust.

By the time reindex completes (3–5 days on a typical connection), your node has independently verified the entire blockchain history. From that point on, historical blocks are fetched on demand and verified on each read through Bitcoin Core's normal mechanisms.

### Isn't this similar to a light client?

No. Light clients (SPV wallets) do not validate transactions — they rely on the assumption that the longest chain is valid. SlimNode runs a full Bitcoin node that validates every transaction. The difference is where block files are stored, not how they are validated.

### What about privacy?

SlimNode fetches block data at the individual block level using blockmaps, not as complete 128 MB file units. The archive server can observe which block files are accessed, but not which specific transactions or addresses you care about — a single block file contains thousands of transactions from many users.

The server can see which byte ranges are requested per file. No protocol or P2P changes required.

### What if the archive server goes down?

SlimNode caches recently accessed block files locally. If the server is unavailable:

- ✅ New block validation continues normally (P2P + local UTXO set)
- ✅ Wallet operations work normally
- ✅ Mempool monitoring works
- ⚠️ Historical block queries depend on the local cache

This is the same behavior as a pruned node with respect to historical data — temporarily unavailable, not permanently lost. The node recovers automatically when the archive server comes back online.

### What about long-term storage growth?

As your node receives new blocks from the P2P network, they accumulate locally. SlimNode handles this through periodic **compaction**: old local blocks are handed back to the archive layer, keeping only recent blocks on disk. The local storage footprint stays bounded regardless of how long the node runs.

Compaction can be triggered automatically (when disk usage exceeds a threshold), on a schedule, or manually via `slimnode compact`.

### How does SlimNode relate to Utreexo?

They address different problems and are complementary.

Bitcoin's block integrity rests on a chain of Merkle commitments: each block header contains a Merkle root that commits to every transaction in that block. Altering a single transaction changes the Merkle root, which changes the block hash, which invalidates the proof-of-work. SlimNode's trust model is built entirely on this existing structure — no new cryptography, no new protocol.

Utreexo applies a similar idea to the UTXO set: instead of storing the full set (~10 GB), a node keeps a compact accumulator (~1 KB) that can prove any coin's existence with a Merkle inclusion proof. It is an elegant approach to a real problem — chainstate storage — but it requires nodes to exchange those proofs over a new P2P sub-protocol, which means changes to node software and gradual network adoption.

SlimNode targets a different constraint: block archive storage (~720 GB of `blk*.dat` files), not chainstate storage. It requires no changes to the Bitcoin protocol, no cooperation from peers, and works today with any standard node using only the existing `-blocksdir` flag.

They can coexist — a future node could use Utreexo for a compact UTXO set and SlimNode for the block archive.
