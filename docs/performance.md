# Performance

Budget: the hotkey-to-panel path must feel instant.

| Target | Budget | Measured |
| --- | --- | --- |
| parse + match + filters + query, per item, p95 | < 1 ms | **26 µs** |
| bundled game data decoded (English data + index) | < 150 ms | **~8 ms** |
| bundled game data decoded, translations included | < 150 ms | **~100 ms** |

Both budgets are met. The game data is decoded once, on a background goroutine at start-up
(`State.DecodeData`), so it is normally ready before the first hotkey. On a hotkey the local
work is noise next to the trade2 round trips, so network latency (see
[Trade client](#trade-client)) is where the user-visible time goes.

## Machine

- CPU: AMD Ryzen 9 9950X3D 16-Core Processor, 32 threads
- Linux 7.2, Go 1.27.3, `go test -bench` defaults

The machine was shared with other work while measuring (load average around 8), so treat
differences under ~15 % between runs as noise.

## Running

```sh
go test -run '^$' -bench . ./internal/core   # parse / match / filters / full pipeline + p95 report
go test -run '^$' -bench . ./internal/data   # zstd, sha256, JSON, index build, full uncached load
```

`BenchmarkPipelineP95` logs a per-item latency distribution (every corpus item x200) and
reports p50/p95/p99/max as metrics. The core benchmarks match against a synthetic catalog built
from the corpus (every corpus template known) rather than the bundled data, which exercises the
same code paths with cheaper lookups. `data.Bundled` caches its result for the process, so the
data benchmarks measure the same work through the decode and index functions, which bypass
the cache.

## Results

Corpus: the 81 items in `internal/core/testdata/corpus/index.toml`. Times are per benchmark
operation, for the whole corpus unless noted.

### internal/core (`bench_test.go`)

| Bench | Time |
| --- | --- |
| `ParseItemCorpus` | 645 µs |
| `TemplatizeLine` (one line) | 0.72 µs |
| `MatchItemCorpus` | 19 µs |
| `BuildFiltersToTradeQueryCorpus` | 171 µs |
| `PipelineCorpus` (all of the above) | 884 µs |
| per-item pipeline p50 / p95 / p99 | 8.8 µs / 26.5 µs / 50.6 µs |
| per-item pipeline max | 183 µs (an advanced-copy rare ring) |

### internal/data (`bench_test.go`)

| Bench | Time |
| --- | --- |
| `ZstdStats` (decompress only) | 0.94 ms |
| `Sha256Stats` (verify) | 0.53 ms |
| `JSONStats` (stats asset) | 3.6 ms |
| `DecodeAssetStats` (zstd + sha256 + JSON) | 5.2 ms |
| `DecodeAssetAll` (4 English assets) | 7.1 ms |
| `IndexBuild` | 0.77 ms |
| `DecodeIndex` (English decode + index) | 7.9 ms |
| `BundledUncached` (everything, translations included) | 100 ms |

Start-up is dominated by JSON decoding; the translation tables are most of the full load.

## Trade client

- **Connection reuse**: each client (`trade.Client`, `trade.NinjaSource`, `trade.ScoutSource`) builds
  one `http.Client` with its own transport in its constructor and reuses it, so requests share
  the connection pool.
- **HTTP/2 and gzip**: the transport forces an HTTP/2 attempt, and Go's transport asks for and
  decompresses gzip on its own.
- **Concurrent fetches**: listing chunks (`FetchChunk` = 10) are requested concurrently, so the
  default 20 listings cost one round trip after the search, not two. Each chunk still reserves
  its own rate-limiter slot, and the seeded fetch policy (12 requests / 4 s) allows both at once.
- **Rate limiting**: a request reserves a slot before it is sent, so concurrent requests cannot
  overrun a policy. Requests the limiter would delay longer than `MaxQueueWait` (10 s) fail fast
  as rate-limited.
- **Responses** are cached per query (otter, `CacheTTL`, 5 minutes by default), so repeat
  lookups of the same item skip the network.

## Release builds

Packages are built by `cmd/release` with `-tags production -trimpath -ldflags "-s -w ..."`
(no symbol table or DWARF), with the Go compiler's default optimisations.
