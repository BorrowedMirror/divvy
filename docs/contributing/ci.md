# CI workflows

| Workflow | Trigger | What it does |
| --- | --- | --- |
| **CI** (`ci.yml`) | PR, push to `master`, manual | **Lint**: `task vet bindings:check commitconfig:check lint` (go vet and golangci-lint, which also checks formatting and runs gosec, for Linux, `GOOS=windows` and `GOOS=darwin` with `-tags purego`; generated files up to date), then `task vuln licenses:check` (govulncheck and the dependency licence policy for the same three targets). **Repository checks**: actionlint, editorconfig-checker, shellcheck, zizmor, trufflehog over the whole tree, and osv-scanner over `go.mod` and the web app's production dependencies. **Test (Linux, macOS, Windows)**: native `go vet`, `go build ./cmd/divvy`, and `go test -race` with coverage (`task coverage`; Linux under Xvfb + openbox), uploaded to Codecov over OIDC with flags `core`, `data`, `trade`, `platform`, `config`, `app`, `ipc`. **Windows installer**: the NSIS installer cross-compiled on Linux. **Web**: Biome, sort-package-json, tsc and Vitest coverage (flag `web`). |
| **E2E** (`e2e.yml`) | PR, push to `master`, manual | The app (test-hook build) against the fake game (`e2e/cmd/fake-game`) and a mock trade2 API, driven by the Go tests in `e2e/` over the test hook: X11 (Xvfb + openbox, real hotkey presses), X11 overlay stacking over a fullscreen game, Wayland (headless sway + xdg-desktop-portal-wlr), Windows and macOS. App and fake-game logs are uploaded when a job fails. |
| **Commitlint** (`commitlint.yml`) | PR | Lints every PR commit and the PR title against `.commitlintrc.json`. |
| **PR Labels** (`pr-labels.yml`) | PR (`pull_request_target`) | Labels the PR from its title (`type/<type>`, `area/<scope>`, `type/breaking-change`); the labels group the release notes (`.github/release.yml`). Sets the milestone of the release the PR will ship in. |
| **Data refresh** (`data-refresh.yml`) | daily, manual | `go run ./cmd/datarefresh refresh`, then the core and data tests; BorrowedMirror Bot opens a `build(data): refresh game data` PR only if the assets changed. |
| **Fixtures refresh** (`fixtures-refresh.yml`) | manual | `go run ./cmd/datarefresh fixtures` (2 s between requests), then the trade tests; BorrowedMirror Bot opens a PR with the new trade fixtures. |
| **Crowdin** (`crowdin.yml`) | push to `master` changing the English strings, weekly, manual | Uploads the source strings to Crowdin and has BorrowedMirror Bot open an `l10n(web)` PR with the downloaded translations. Skipped until the Crowdin secrets are set. |
| **Tag release** (`tag-release.yml`) | push to `master`, manual | When the pushed commit is a merged `release: vX.Y.Z (#N)` pull request, BorrowedMirror Bot pushes the `vX.Y.Z` tag, which starts the release. |
| **Release** (`release.yml`) | push of a `v*` tag | Checks the tag is that release commit on `master`, with `VERSION` and `web/package.json` holding its version, then calls **Release SLSA** (`release-slsa.yml`): builds the Windows NSIS installer (on Linux), the Linux AppImage and the macOS universal DMG, signs the update artifacts into `update.json`, creates a draft release with a CycloneDX SBOM, `THIRD-PARTY-NOTICES.html`, `SHA256SUMS.txt` and the notes from `go run ./cmd/release notes`, and attests every asset (SLSA Build Level 3). See [releasing.md](releasing.md). |

## Conventions

- Actions are pinned to commit SHAs. Renovate (`.renovaterc.json`) opens one `build(deps): ...` PR per dependency after
  a 3-day minimum release age and turns on auto-merge for it.
- Every job first runs StepSecurity's harden-runner, which audits its outbound connections.
- Workflows default to `permissions: contents: read`; jobs that write (PRs, tags, releases) widen permissions locally.
- Secrets live in environments: `automation` (BorrowedMirror Bot's app credentials and the Crowdin secrets; deploys only
  from `master`) and `release` (the updater's signing key; deploys only from `v*` tags).
- `concurrency` cancels superseded PR runs; scheduled and release runs are never cancelled.
- Linux system packages come from one composite action, `.github/actions/linux-deps`; the Go module and build caches
  from `.github/actions/go-cache`, one cache per purpose and OS, saved only from `master`.
- Every Go job builds `web/dist` first, because the binary embeds it.

## Running the same checks locally

```sh
make check                     # vet, tests, generated files, golangci-lint, actionlint, editorconfig, shellcheck
make vuln licenses:check
make coverage
pnpm --dir web lint && pnpm --dir web typecheck && pnpm --dir web coverage
```

`make actionlint` lints the workflows on its own, with [kjanat's actionlint](https://github.com/kjanat/actionlint)
pinned in `tools/go.mod`, as CI does. It understands what the workflows use that rhysd's upstream actionlint may not,
such as `$/` paths to the repository's own actions and reusable workflows. zizmor, shellcheck and trufflehog run in the
pre-commit hook when installed, and in CI's **Repository checks**.

The e2e suites can be run locally too; see [CONTRIBUTING.md](../../CONTRIBUTING.md#end-to-end-tests).
