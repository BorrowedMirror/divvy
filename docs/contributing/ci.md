# CI workflows

| Workflow | Trigger | What it does |
| --- | --- | --- |
| `ci.yml` | PR, push to `master`, manual | rustfmt; clippy `-D warnings` on ubuntu/windows/macos; cargo-deny; cargo-machete; nextest + llvm-cov on all three OSes via `cargo xtask coverage --check --profile ci` (gates enforced, per-crate lcov uploaded to Codecov with flags `core`, `data`, `trade`, `platform`, `config`); bindings freshness; generated commit docs freshness and xtask tests; web eslint, tsc and Vitest coverage (flag `web`). |
| `commitlint.yml` | PR | Lints every PR commit and the PR title against `.commitlintrc.json`. |
| `e2e.yml` | PR, push to `master`, manual | Real app + `tools/fake-game` on Windows (tauri-driver + msedgedriver), Linux X11 (Xvfb + openbox), Linux Wayland (headless sway + xdg-desktop-portal-wlr) and macOS (Rust harness over the test hook). |
| `contract.yml` | nightly, manual, PRs touching `crates/trade` (not forks) | Live trade2 smoke: `cargo nextest run -p rkl-trade --run-ignored only -E 'test(live_)'`. |
| `data-refresh.yml` | daily, manual | `cargo xtask data refresh`; opens `build(data): refresh game data` PR only if assets changed. |
| `fixtures-refresh.yml` | manual | `cargo xtask fixtures record` (2 s between requests); opens a PR with new trade fixtures. |
| `release.yml` | tag `v*` | Draft release with git-cliff notes; signed bundles via tauri-action (Windows MSI/NSIS, macOS universal DMG, Linux AppImage/deb/rpm), `latest.json`, `SHA256SUMS.txt`, CycloneDX SBOM, third-party notices. |

## Conventions

- Actions are pinned to major versions; Renovate (`.renovaterc.json`) proposes bumps as
  `build(deps): ...` after a 3-day minimum release age.
- Workflows default to `permissions: contents: read`; jobs that write (PRs, releases) widen
  permissions locally.
- `concurrency` cancels superseded PR runs; scheduled and release runs are never cancelled.
- Linux system packages come from one composite action, `.github/actions/linux-deps`.
- Test profiles, retries, JUnit output and the serial display test group are in
  `.config/nextest.toml`.

## Running the same checks locally

```sh
cargo xtask lint
cargo xtask coverage --check
cargo xtask gen && git diff --exit-code
pnpm --dir web coverage
```

Lint workflows with [actionlint](https://github.com/rhysd/actionlint):
`go run github.com/rhysd/actionlint/cmd/actionlint@latest`.

The e2e suites can be run locally too; see
[CONTRIBUTING.md](../../CONTRIBUTING.md#end-to-end-tests).
