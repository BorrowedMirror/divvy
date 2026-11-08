# CI workflows

| Workflow | Trigger | What it does |
| --- | --- | --- |
| `ci.yml` | PR, push to `master`, manual | **check**: `task fmt vet bindings:check commitconfig:check lint` (gofmt; go vet and golangci-lint for Linux, `GOOS=windows` and `GOOS=darwin` with `-tags purego`; generated files up to date), then `task vuln licenses:check` (govulncheck and the dependency licence policy for the same three targets). **test** on ubuntu/windows/macos: native `go vet`, `go build ./cmd/rkl`, and `go test -race` with coverage (`task coverage`; Linux under Xvfb + openbox), uploaded to Codecov with flags `core`, `data`, `trade`, `platform`, `config`, `app`, `ipc`. **package-windows**: the NSIS installer cross-compiled on Linux. **web**: Biome, tsc and Vitest coverage (flag `web`). |
| `commitlint.yml` | PR | Lints every PR commit and the PR title against `.commitlintrc.json`. |
| `pr-labels.yml` | PR (`pull_request_target`) | Labels the PR from its title (`type/<type>`, `area/<scope>`, `type/breaking-change`); the labels group the release notes (`.github/release.yml`). Sets the milestone of the release the PR will ship in. |
| `e2e.yml` | PR, push to `master`, manual | The app (test-hook build) against the fake game (`e2e/cmd/fake-game`) and a mock trade2 API, driven by the Go tests in `e2e/` over the test hook: Linux X11 (Xvfb + openbox, real hotkey presses), Linux X11 overlay stacking over a fullscreen game, Linux Wayland (headless sway + xdg-desktop-portal-wlr), Windows and macOS. App and fake-game logs are uploaded when a job fails. |
| `data-refresh.yml` | daily, manual | `go run ./cmd/datarefresh refresh`, then the core and data tests; opens a `build(data): refresh game data` PR only if the assets changed. |
| `fixtures-refresh.yml` | manual | `go run ./cmd/datarefresh fixtures` (2 s between requests), then the trade tests; opens a PR with new trade fixtures. |
| `crowdin.yml` | push to `master` changing the English strings, weekly, manual | Uploads the source strings to Crowdin and opens an `l10n(web)` PR with the downloaded translations. |
| `release.yml` | push of a `v*` tag | Checks the tag against the `VERSION` file, builds the Windows NSIS installer (on Linux), the Linux AppImage and the macOS universal DMG, signs the update artifacts into `update.json`, and creates a draft release with a CycloneDX SBOM, `THIRD-PARTY-NOTICES.html`, `SHA256SUMS.txt` and GitHub-generated notes. See [releasing.md](releasing.md). |

## Conventions

- Actions are pinned to commit SHAs; Renovate (`.renovaterc.json`) proposes bumps as
  `build(deps): ...` after a 3-day minimum release age.
- Workflows default to `permissions: contents: read`; jobs that write (PRs, releases) widen
  permissions locally.
- `concurrency` cancels superseded PR runs; scheduled and release runs are never cancelled.
- Linux system packages come from one composite action, `.github/actions/linux-deps`.
- Every Go job builds `web/dist` first, because the binary embeds it.

## Running the same checks locally

```sh
make check                     # gofmt, vet, tests, generated files, golangci-lint
make vuln licenses:check
make coverage
pnpm --dir web lint && pnpm --dir web typecheck && pnpm --dir web coverage
```

Lint workflows with [actionlint](https://github.com/rhysd/actionlint):
`go run github.com/rhysd/actionlint/cmd/actionlint@latest`.

The e2e suites can be run locally too; see
[CONTRIBUTING.md](../../CONTRIBUTING.md#end-to-end-tests).
