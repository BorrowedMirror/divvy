# Security policy

## Supported versions

Only the latest release receives security fixes. Please make sure you are on the newest
version from the [releases page](https://github.com/nightah/kalandra/releases) before reporting.
Release artefacts are signed (Tauri updater signatures, Apple Developer ID on macOS) and listed
in `SHA256SUMS.txt`; the app does not update itself yet.

## Reporting a vulnerability

Please **do not** open a public issue. Report privately through
[GitHub security advisories](https://github.com/nightah/kalandra/security/advisories/new).

Include what you found, how to reproduce it, the affected version and OS, and the impact you
expect. We aim to acknowledge reports within 3 days and to ship a fix or mitigation within 30
days, and will credit you in the advisory unless you prefer otherwise.

## Scope

In scope: the kalandra desktop app, its release artefacts (including the updater manifest
`latest.json`), and this repository's CI/release pipeline. Particularly relevant:

- handling of the optional `POESESSID` (it must never be logged or sent anywhere but
  pathofexile.com);
- the IPC surface between the web UI and the Rust core, including `open_trade`, which only
  opens pathofexile.com URLs;
- the global input hook and clipboard handling;
- `kalandra --trigger` forwarding through the single-instance plugin and the SIGUSR1/SIGUSR2 handlers (Unix);
- the test hook (a localhost TCP socket enabled by `KALANDRA_TEST_HOOK`), which is compiled
  only into debug builds and builds with the `test-hook` feature, and must never be part of a
  release build.

Out of scope: vulnerabilities in Path of Exile 2, pathofexile.com, poe.ninja or poe2scout
themselves (report those to their operators).
