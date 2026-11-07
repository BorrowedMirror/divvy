# CI workflows

| Workflow | Trigger | What it does |
| --- | --- | --- |
| `ci.yml` | PR, push to `master`, manual | rustfmt; clippy `-D warnings` on ubuntu/windows/macos; cargo-deny; cargo-machete; nextest + llvm-cov on all three OSes via `cargo xtask coverage --check --profile ci` (gates enforced, per-crate lcov uploaded to Codecov with flags `core`, `data`, `trade`, `platform`, `config`); bindings freshness; generated commit docs freshness and xtask tests; web Biome, tsc and Vitest coverage (flag `web`). |
| `commitlint.yml` | PR | Lints every PR commit and the PR title against `.commitlintrc.json`. |
| `pr-labels.yml` | PR (`pull_request_target`) | Labels the PR from its title (`type/<type>`, `area/<scope>`, `type/breaking-change`); the labels group the release notes (`.github/release.yml`). Sets the milestone of the release the PR will ship in. |
| `e2e.yml` | PR, push to `master`, manual | Real app + `tools/fake-game` on Windows (tauri-driver + msedgedriver), Linux X11 (Xvfb + openbox), Linux Wayland (headless sway + xdg-desktop-portal-wlr) and macOS (Rust harness over the test hook). |
| `contract.yml` | nightly, manual, PRs touching `crates/trade` (not forks) | Live trade2 smoke: `cargo nextest run -p rkl-trade --run-ignored only -E 'test(live_)'`. |
| `data-refresh.yml` | daily, manual | `cargo xtask data refresh`; opens `build(data): refresh game data` PR only if assets changed. |
| `fixtures-refresh.yml` | manual | `cargo xtask fixtures record` (2 s between requests); opens a PR with new trade fixtures. |
| `ci-go.yml` | PR, push to `master`, manual | Go build: `task fmt vet bindings:check lint` (gofmt; go vet and golangci-lint for Linux, `GOOS=windows` and `GOOS=darwin` with `-tags purego`; `bindgen -check`); on ubuntu/windows/macos a native `go vet`, `go build ./cmd/rkl` and `go test -race` with coverage (`task coverage`, uploaded to Codecov with flags `go-core`, `go-data`, `go-trade`, `go-platform`, `go-config`, `go-app`, `go-ipc`); the Windows NSIS installer cross-compiled on Linux. |
| `release-go.yml` | manual (version input) | Go build release: draft release for tag `go-v<version>` at the run's commit with GitHub-generated notes; Windows NSIS installer (built on Linux), Linux AppImage, macOS universal DMG, CycloneDX SBOM, `SHA256SUMS.txt`. Nothing signed yet. The tag is created when the draft is published. |
| `release.yml` | tag `v*` | Draft release with GitHub-generated notes grouped by PR labels (`.github/release.yml`); signed bundles via tauri-action (Windows MSI/NSIS, macOS universal DMG, Linux AppImage/deb/rpm), `latest.json`, `SHA256SUMS.txt`, CycloneDX SBOM, third-party notices. |

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
# Go build (Task through the pinned wails3: go tool -modfile=tools/go.mod wails3 task ...)
go tool -modfile=tools/go.mod wails3 task check
go tool -modfile=tools/go.mod wails3 task coverage
```

Lint workflows with [actionlint](https://github.com/rhysd/actionlint):
`go run github.com/rhysd/actionlint/cmd/actionlint@latest`.

The e2e suites can be run locally too; see
[CONTRIBUTING.md](../../CONTRIBUTING.md#end-to-end-tests).
