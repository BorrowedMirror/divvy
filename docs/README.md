# kalandra documentation

- [Architecture](architecture.md): components, data flow and boundaries.
- [Contracts](../CONTRACTS.md): the fixed public interfaces between crates, IPC and the web UI.

## Contributing

- [Contributing guide](../CONTRIBUTING.md): development setup per OS, everyday commands.
- [Commit message guidelines](contributing/commit-message.md) (generated from `.commit-config.toml`).
- [CI workflows](contributing/ci.md): what runs where and how to reproduce it locally.
- [Releasing](contributing/releasing.md): `cargo xtask release` and the release workflow.

## Research

Notes on external systems kalandra depends on (captured live; background, not a spec):

- [trade2 API](research/trade2.md): endpoints, payloads, rate-limit headers.
- [poe.ninja and poe2scout](research/ninja-scout.md): aggregated price APIs.
- [Game data sources](research/data-sources.md): inputs to `cargo xtask data refresh`.
- [macOS](research/macos.md): permissions, window tracking, overlay panel.

## Using kalandra

Install requirements, the Linux session matrix, settings paths and every `KALANDRA_*`
environment variable are in the [README](../README.md).

## Policies

- [Security policy](../SECURITY.md)
- [Code of Conduct](../CODE_OF_CONDUCT.md)
