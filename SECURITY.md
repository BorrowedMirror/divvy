# Security policy

## Supported versions

Only the latest release receives security fixes. Please make sure you are on the newest
version from the [releases page](https://github.com/BorrowedMirror/divvy/releases) before reporting.
Release artefacts are listed in `SHA256SUMS.txt`, and the update artefacts are signed with the
updater's ed25519 key (`update.json`), which installed copies check before installing an update.
The packages are not code-signed yet.

## Reporting a vulnerability

Please **do not** open a public issue. Report privately through
[GitHub security advisories](https://github.com/BorrowedMirror/divvy/security/advisories/new).

Include what you found, how to reproduce it, the affected version and OS, and the impact you
expect. We aim to acknowledge reports within 3 days and to ship a fix or mitigation within 30
days, and will credit you in the advisory unless you prefer otherwise.

## Scope

In scope: the Divvy desktop app, its release artefacts (including the updater manifest
`update.json`), and this repository's CI/release pipeline. Particularly relevant:

- handling of the optional `POESESSID` (it must never be logged or sent anywhere but
  pathofexile.com);
- the IPC surface between the web UI and the Go backend, including `open_trade`, which only
  opens pathofexile.com URLs;
- the global input hook and clipboard handling;
- `divvy --trigger` forwarding to the running instance and the SIGUSR1/SIGUSR2 handlers (Unix);
- the update check and install (signature verification against the built-in key);
- the test hook (a localhost TCP socket enabled by `DIVVY_TEST_HOOK`), which is compiled
  only into builds with the `testhook` build tag and must never be part of a release build.

Out of scope: vulnerabilities in Path of Exile 2, pathofexile.com, poe.ninja or poe2scout
themselves (report those to their operators).
