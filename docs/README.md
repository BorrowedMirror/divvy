# Divvy documentation

- [Architecture](architecture.md): components, data flow and boundaries.
- [Screenshots](screenshots.md): every view, retaken with `make screenshots`.
- [Contracts](../CONTRACTS.md): package boundaries, IPC, the command line, the test hook and the data files.

## Contributing

- [Contributing guide](../CONTRIBUTING.md): development setup per OS, everyday commands.
- [Translations](i18n.md): languages, rules and the Crowdin sync.
- [Performance](performance.md): latency budgets and benchmarks.
- [Commit message guidelines](contributing/commit-message.md) (generated from `.commit-config.toml`).
- [CI workflows](contributing/ci.md): what runs where and how to reproduce it locally.
- [Releasing](contributing/releasing.md): versioning, tags, the release workflow and updater keys.

## Research

Notes on external systems Divvy depends on (captured live; background, not a spec):

- [trade2 API](research/trade2.md): endpoints, payloads, rate-limit headers.
- [poe.ninja and poe2scout](research/ninja-scout.md): aggregated price APIs.
- [Game data sources](research/data-sources.md): inputs to the data refresh (`go run ./cmd/datarefresh`).
- [macOS](research/macos.md): permissions, window tracking, overlay panel.

## Using Divvy

Install requirements, the Linux session matrix, settings paths and every `DIVVY_*`
environment variable are in the [README](../README.md).

## Policies

- [Security policy](../SECURITY.md)
- [Code of Conduct](../CODE_OF_CONDUCT.md)
