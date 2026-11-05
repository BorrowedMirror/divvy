# Performance

Budget: the hotkey-to-panel path must feel instant.

| Target | Budget | Measured (after) |
| --- | --- | --- |
| parse + match + filters + query, per item, p95 | < 1 ms | **11.0 µs** |
| bundled game data ready (cold, first call) | < 150 ms | **~7 ms** |

Both budgets are met with roughly two orders of magnitude to spare. On a hotkey the
local work is noise next to the trade2 round trips, so network latency (see
[Trade client](#trade-client)) is where the user-visible time goes.

## Machine

- CPU: AMD Ryzen 9 9950X3D 16-Core Processor (`/proc/cpuinfo`)
- `nproc`: 32
- Linux 7.2, Rust 1.98.1, `bench` profile (inherits `[profile.release]`: LTO, 1 codegen unit, opt-level 3)

The machine was shared with other builds while measuring (load average 10-60), so
criterion reported +-10-40 % swings between runs of unchanged code. Treat
differences under ~15 % as noise; the large changes below were stable across runs.

## Running

```sh
cargo bench -p kalandra-core --bench pipeline   # parse / match / filters / full pipeline + p95 report
cargo bench -p kalandra-data --bench bundled    # cold bundled(), zstd, sha256, serde_json, index build
```

`pipeline` prints a per-item latency distribution (every corpus item x200) before the
criterion groups. `bundled` times the first `bundled()` call in its fresh process (the
result is cached in a `OnceLock`) and then measures decode and index build through the
public API, which bypasses the cache.

## Results

Corpus: the 80 items in `crates/testkit/corpus/index.toml`. Times are criterion medians
for the whole corpus unless noted.

### kalandra-core (`benches/pipeline.rs`)

| Bench | Before | After | Change |
| --- | --- | --- | --- |
| `parse_item/corpus` (80 items) | 167.6 µs | 154.3 µs | -8 % |
| `match_item/corpus` (80 items) | 61.5 µs | 33.2 µs | -46 % |
| `build_filters+to_trade_query/corpus` | 186 µs | 104-208 µs | unchanged code; noise |
| `pipeline/corpus` (all of the above) | 647 µs | 369 µs | -43 % |
| per-item pipeline p50 | 4.19 µs | 3.88 µs | |
| per-item pipeline p95 | 11.26 µs | 11.02 µs | |
| per-item pipeline p99 | 13.18 µs | 13.01 µs | |
| first call in process (max) | 335 µs | 353 µs | lazy regex compilation |

### kalandra-data (`benches/bundled.rs`)

| Bench | Before | After | Change |
| --- | --- | --- | --- |
| `bundled()` cold, first call | 6.9 ms | 7.1-7.4 ms | noise |
| `zstd/stats` (decompress only) | - | 0.32 ms | |
| `sha256/stats` (verify) | - | 0.55 ms | |
| `serde_json/stats` (8232 stats) | - | 1.9 ms | |
| `decode_asset/all` (4 assets) | 3.96 ms | 3.74 ms | noise |
| `index_build` | 1.92 ms | 1.66 ms | -14 % |
| `decode+index` (uncached `bundled()`) | 5.95 ms | 6.17 ms | noise |

Startup is dominated by JSON deserialisation of the stats asset; zstd decode is
~0.3 ms and is not worth replacing.

## Changes

- **Magic base lookup** (`GameData::base_in_magic_name`): normalised the name once and
  tries word runs as `&str` slices instead of `join`ing a new `String` for each of the
  O(n²) candidates. This was the bulk of `match_item` on magic items.
- **Parser**: sections hold borrowed `&str` lines instead of one `String` per line; the
  `(augmented)` suffix and roll-range regexes are skipped for lines without `(`, the
  only literal both patterns require.
- **Index build**: per-kind template maps are pre-sized and keys are looked up by
  `&str` before allocating, so only new kinds/templates allocate.
- **Trade fetch**: listing chunks (`FETCH_CHUNK` = 10) are now requested concurrently.
  The default `fetch_count` of 20 used to cost two sequential round trips after the
  search; it now costs one. Each chunk still reserves its own rate-limiter slot, and
  the seeded fetch policy (12 requests / 4 s) allows both at once.

Already fine and left alone: every regex is a `LazyLock` static (none compiled per
call); `templatize` is a single `replace_all` (~80 ns per line); catalog lookups are
`HashMap`s (no linear scans); `bundled()` decodes once and shares the index behind an
`Arc`, so later calls are a cheap clone.

## Trade client

- **Connection reuse**: each client (`TradeClient`, `NinjaSource`, scout) builds one
  `reqwest::Client` in its constructor and reuses it, so requests share the connection
  pool.
- **HTTP/2 and gzip**: the workspace `reqwest` features include `http2` (negotiated
  through rustls ALPN) and `gzip` (automatic `Accept-Encoding` and decompression).
- **Rate limiting**: `send` reserves a slot before awaiting, so concurrent requests
  cannot overrun a policy. Requests the limiter would delay longer than
  `MAX_QUEUE_WAIT` fail fast with `RateLimited`.
- **Responses** are cached per query (`moka`, `cache_ttl`), so repeat lookups of the
  same item skip the network.

Possible follow-ups (not done here; they need app-side changes):

- Warm the parser regexes once at startup (for example by parsing a fixed item text) so
  the first hotkey does not pay the ~0.35 ms lazy compilation.
- Pre-connect to `www.pathofexile.com` at startup (a cheap `data/leagues` request
  through the shared client) so the first search reuses a warm TLS connection.

## Release profile

`[profile.release]`: `lto = true`, `codegen-units = 1`, `opt-level = 3`,
`strip = true`, `panic = "abort"`. This matches Tauri's size-optimisation advice except
for `opt-level` (Tauri suggests `"s"`); 3 is kept on purpose for latency.

`panic = "abort"` is safe: no workspace code uses `catch_unwind`, `resume_unwind`, a
panic hook that expects to continue, or tokio `JoinError::is_panic`. Cargo ignores the
`panic` setting for test and bench targets, so `#[should_panic]` tests still unwind.

One soft dependency on unwinding: `apps/desktop/src-tauri/src/flow.rs` maps the
`JoinError` of its clipboard-copy `spawn_blocking` task to an `internal` IPC error. In
release builds a panic inside that task aborts the process instead. Such a panic would
be a bug either way, so this is acceptable, but that error branch only runs in dev builds.
