# Security policy

## Supported versions

Only the latest release receives security fixes. kalandra updates itself through the signed
Tauri updater; please make sure you are on the newest version before reporting.

## Reporting a vulnerability

Please **do not** open a public issue. Report privately through
[GitHub security advisories](https://github.com/nightah/kalandra/security/advisories/new).

Include what you found, how to reproduce it, the affected version and OS, and the impact you
expect. We aim to acknowledge reports within 3 days and to ship a fix or mitigation within 30
days, and will credit you in the advisory unless you prefer otherwise.

## Scope

In scope: the kalandra desktop app, its updater and release artefacts, and this repository's
CI/release pipeline. Particularly relevant:

- handling of the optional `POESESSID` (it must never be logged or sent anywhere but
  pathofexile.com);
- the IPC surface between the web UI and the Rust core;
- the global input hook and clipboard handling;
- the debug-only test hook (`test-hook` feature), which must never be enabled in release builds.

Out of scope: vulnerabilities in Path of Exile 2, pathofexile.com, poe.ninja or poe2scout
themselves (report those to their operators).
