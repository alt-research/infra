# Shared L1 memory cache

This opt-in mode targets several read-only rollup followers sharing one L1 RPC.
It reduces repeated upstream work, rather than treating lower request rate as a
reduction in total provider cost. The legacy cache and forwarding path are
unchanged when `cache.l1.enabled` is false or absent.

## Configuration

```toml
[server]
rpc_host = "0.0.0.0"
rpc_port = 8080
ws_port = 0
max_body_size_bytes = 1048576
max_concurrent_rpcs = 64
timeout_seconds = 30

[cache]
enabled = true

[cache.l1]
enabled = true
chain_id = 1
max_bytes = 268435456
max_inflight = 16
immutable_ttl = "6h"
tag_ttl = "30s"
number_ttl = "30s"
request_timeout = "20s"

[backend]
response_timeout_seconds = 15
max_response_size_bytes = 10485760

[backends.l1]
rpc_url = "$L1_RPC"
max_retries = 0

[backend_groups.ethereum]
backends = ["l1"]
routing_strategy = "fallback"

[rpc_method_mappings]
eth_chainId = "ethereum"
eth_getBlockByHash = "ethereum"
eth_getBlockByNumber = "ethereum"
eth_getBlockReceipts = "ethereum"
debug_getRawReceipts = "ethereum"
eth_getTransactionReceipt = "ethereum"
eth_getStorageAt = "ethereum"
eth_getProof = "ethereum"
eth_getLogs = "ethereum"
```

Use a private listener/network; the example does not configure authentication.
The upstream URL is read from the process environment and must not be committed.
Verify its chain ID before deployment. L1 mode requires exactly one backend,
one ordinary fallback group, no Redis and HTTP only. It rejects write-method
mappings and consensus rewriting, so the cache cannot accidentally merge
responses from different chains or start a consensus polling workload.

The provider must support the selected receipt method with **block-hash**
parameters and an acceptable billing cost. `debug_geth` in op-node selects
`debug_getRawReceipts`; `standard` selects `eth_getBlockReceipts` with possible
per-transaction fallback. This feature does not translate vendor-specific
receipt methods or disable receipt validation. An unsupported-method fallback
can eliminate most savings: inspect actual upstream method counters, not only
client settings.

## Storage and forwarding

One process shares a byte-budgeted LRU. Entries retain JSON result bytes; the
budget charges their payload, normalized key and a 256-byte metadata allowance.
This is **not a process RSS limit**: Go overhead, request parsing and concurrent
responses need additional headroom. `max_inflight` bounds distinct cacheable
misses; backend response-size and frontend concurrency limits remain necessary.
Oversized results are returned but not stored. Expired entries are discarded on
lookup; capacity evicts the least recently used entries. A six-hour TTL is not a
promise that six hours of data fit in the budget. Restarting proxyd loses only
cache contents, not any follower database.

Keys include configured L1 chain ID, backend group, method and normalized
parameters; full-transaction and header-only block requests are distinct.
Request IDs are excluded and restored for each client. Concurrent identical
misses share one bounded operation. Canceling one waiter does not cancel other
waiters; detached upstream work still has `request_timeout` and is not retried
by the cache. Identical elements within an HTTP batch are also combined, even
with duplicate IDs. L1 mode forwards individual misses, rather than upstream
transport batches, enabling per-method coalescing and attribution. Batching
normally does not discount provider CU; benchmark latency separately.

## Cache policies

| Method / selector | Policy |
| --- | --- |
| `eth_getBlockByHash(hash, fullTx)` | Immutable TTL, returned hash must match. |
| `eth_getBlockReceipts(hash)` | Immutable TTL for nonempty, consistently ordered receipts matching the requested block hash. The client still validates completeness/root. |
| `debug_getRawReceipts(hash)` | Immutable TTL for nonempty hex receipt arrays; the client still validates decoding/root. |
| `eth_getBlockByNumber(latest/safe/finalized, fullTx)` | Short tag TTL; labels never become permanent entries. |
| `eth_getBlockByNumber(number, fullTx)` | Short TTL until a successful upstream finalized-label response establishes a boundary; fresh responses at/below it can use immutable TTL. |
| `eth_chainId`, `net_version` | Immutable TTL only when matching configured chain ID. |
| Per-transaction receipts, pending, logs, storage/proofs and other permitted reads | Bypass cache; they are not assumed immutable. |

Raw receipts are decoded and standard receipt fields parsed before retention.
The proxy does not independently reconstruct the receipt trie or check complete
receipt count against a fetched header. Clients retain those checks; if a
well-shaped but invalid upstream result is cached, stop rollout and restart the
proxy to clear it after diagnosing the provider. No chain-data reset is needed.

L1 mode omits endpoint URLs from backend startup logs and redacts transport-error
URLs while preserving cancellation/deadline classification. Request-body logging
should stay disabled for this deployment.

Errors, null, empty receipts and mismatched block/receipt metadata are never
stored. A finalized-height regression or same-height finalized-hash change
invalidates canonical-number/tag entries. In-flight mutable reads are fenced
across finality changes so earlier results cannot be promoted using a newer
boundary. Immutable hash content can remain cached when canonical mappings
change. There is no active finality poller: the cache learns from normal caller
requests. This relies on the upstream's finality statements and does not replace
L1 verification. Fixed confirmation counts are never treated as finality.

## Metrics and acceptance

`proxyd_cache_hits_total` / `proxyd_cache_misses_total` count completed cache
lookups/new cacheable loads. `proxyd_l1_cache_events_total{method,event}` includes
coalescing, bypass, eviction, oversize and finality resets. `proxyd_l1_cache_bytes`
reports accounted retained bytes. A batch HIT header means at least one result
was cached or coalesced, not that the whole batch avoided upstream work.

Existing backend RPC counters must measure actual sends, including retries and
noncacheable methods. Compare provider CU over **the same processed L1 range**
and sync phase. Confirm same-height hashes, safe/finalized progress, memory,
429/error rates and fallback methods; reduced progress is not a cost saving.

Disable `cache.l1.enabled` to return to the existing cache behavior (or disable
all cache). Rollback requires only a proxy restart/config change; no follower
volume resets. Test provider support before changing follower RPC kind.

The manual GAR build workflow now accepts `platforms`; its default remains
`linux/amd64`. Select ARM64 or both platforms for ARM clients, then pin and verify
the resulting image digest. No image is built or deployed by opening this PR.
