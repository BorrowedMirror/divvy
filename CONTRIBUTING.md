# Contributing to Divvy

Thanks for helping! Divvy is a Path of Exile 2 price-check overlay: a Go app built on
[Wails v3](https://v3.wails.io) (`cmd/divvy`, `internal/*`) and a React 19 + Vite web UI (`web/`)
that the binary embeds. See [docs/architecture.md](docs/architecture.md) for how the pieces fit
together.

By participating you agree to the [Code of Conduct](CODE_OF_CONDUCT.md). Security issues go
through [SECURITY.md](SECURITY.md), not public issues.

## Development setup

All platforms need:

- **Go**, the version in `go.mod`. A newer local Go downloads the pinned `toolchain` on its own.
- **Node.js 24** and **pnpm** (version pinned by `packageManager` in `web/package.json`;
  `corepack enable` picks it up).

The other tools (the Wails CLI with its built-in Task runner, golangci-lint, govulncheck,
go-licenses, and kjanat's actionlint for the GitHub workflows) are pinned in `tools/go.mod` and run through `go tool -modfile=tools/go.mod`; there
is nothing else to install.

### Linux (Debian/Ubuntu)

```sh
sudo apt-get install -y libwebkit2gtk-4.1-dev libgtk-3-dev libgtk-layer-shell-dev \
  libxkbcommon-dev pkg-config
# headless display tests (optional locally, used in CI)
sudo apt-get install -y xvfb openbox xdotool
```

Fedora: `webkit2gtk4.1-devel gtk3-devel gtk-layer-shell-devel libxkbcommon-devel`.
Arch: `webkit2gtk-4.1 gtk3 gtk-layer-shell libxkbcommon xdotool`.

Linux builds use the `gtk3` build tag (GTK 3 and WebKitGTK 4.1); the `Taskfile.yml` tasks add it
for you. Without it the build targets GTK 4.

### macOS

Install the Xcode command line tools (`xcode-select --install`). Running the overlay needs the
Accessibility permission, and preferably Screen Recording, for your terminal or the built app
(System Settings -> Privacy & Security); see the [README](README.md#macos-12) and
[docs/research/macos.md](docs/research/macos.md). Dev builds that are re-signed on every build
can lose these grants.

### Windows

Go, Node.js and pnpm are enough to build and run: the Windows build needs no cgo, and WebView2
ships with Windows 10/11. `go test -race` (used by `make test` and `make coverage`) needs cgo, so
install a gcc toolchain such as MinGW-w64 for it.

### First run

```sh
pnpm --dir web install                 # web deps, commitlint, lefthook
pnpm --dir web exec lefthook install   # hooks from .lefthook.yml: golangci-lint --fix, biome --write and tsc, actionlint and zizmor (workflows; zizmor only if installed) and rustfmt (tools/game-extract) on commit, commitlint on commit-msg
pnpm --dir web build                   # the binary embeds web/dist, so build it first
go run -tags gtk3 ./cmd/divvy            # Linux; drop -tags gtk3 on Windows and macOS
```

## Everyday commands

Everyday work is plain Go: `go build`, `go test ./...`, `go vet ./...`, and `go generate ./...`
to regenerate `web/src/bindings.ts` (after changing the IPC service) and the commit docs (after
changing `.commit-config.toml`). On Linux add `-tags gtk3`. The rest are
[Task](https://taskfile.dev) tasks in `Taskfile.yml`, run with the Task runner built into the
Wails CLI (pinned in `tools/go.mod`, so nothing needs installing). The `Makefile` passes every
target through to it, and `NAME=value` arguments to the task:

```sh
make                 # lists the tasks
make check           # = the pinned wails3 CLI, built without cgo, running `task check`
```

Without `make` (a plain Windows shell), run `go tool -modfile=tools/go.mod wails3 task <task>`
(on Linux, prefix `CGO_ENABLED=0`: a cgo build of the CLI needs GTK4 headers). It runs one
task per invocation.

| Task | What it does |
| --- | --- |
| `make dev` | `wails3 dev`: Vite with hot reload in the app's windows, and a Go rebuild and restart on every Go change (`build/config.yml`) |
| `make run` | builds the frontend and a development binary (`target/go/divvy`) and runs it |
| `make test` | `go test -race ./...` |
| `make fmt` | formats the Go code with golangci-lint's formatters (gofmt) |
| `make check` | go vet for the host, `GOOS=windows` and `GOOS=darwin` (`-tags purego`, since cgo needs the macOS SDK); `go test -race`; `bindgen -check`; `commitconfig -check`; golangci-lint for the same three targets (`.golangci.yml`, which also checks gofmt); actionlint over the workflows |
| `make vuln` | govulncheck for the same three targets |
| `make licenses:check` | fails when a dependency's licence is outside the policy in `cmd/release/notices.go` |
| `make coverage` | `go test -race` with one coverage profile per internal package in `target/go/coverage/` |
| `make generate` | `go generate ./...` |
| `make package:windows` | cross-compiles `divvy.exe` (no cgo) with its icon, manifest and version info, then builds the per-user NSIS installer `target/go/divvy-<version>-windows-x64-setup.exe` and the update exe |
| `make package:linux` | builds `target/go/divvy-<version>-linux-x86_64.AppImage` with `wails3 generate appimage` (downloads linuxdeploy on first use) |
| `make package:macos` | universal `Divvy.app`, `target/go/divvy-<version>-macos-universal.dmg` and the update zip; macOS only |
| `make notices` | writes `target/go/THIRD-PARTY-NOTICES.html` |
| `make updater:keygen` | prints a new updater key pair (see [releasing](docs/contributing/releasing.md#updates)) |
| `make data:refresh` | `go run ./cmd/datarefresh refresh`: downloads the trade2 data and RePoE exports (cached in `target/data-cache`; `OFFLINE=1` reuses the cache) and rebuilds the bundled game data in `internal/data/assets` |
| `make fixtures:record` | `go run ./cmd/datarefresh fixtures`: re-records the trade fixtures in `internal/trade/testdata/fixtures` from the live API, 2 s between requests (`LEAGUE=`, `BASE_TYPE=`) |
| `pnpm --dir web test` / `coverage` / `lint` / `typecheck` | Vitest, biome, tsc |

Coverage targets (Codecov status checks, `.codecov.yml`): core, trade, data, config >= 90%
lines; platform >= 70%; web >= 80%; patch coverage >= 80%.

Development builds (`go build`, `make run`, `make dev`) are Wails dev builds: the web
inspector is available and `FRONTEND_DEVSERVER_URL` is honoured. Packages are built with the
`production` tag. Packaging steps the `go` command can't do (installers, signing updates,
notices) live in `cmd/release` (`go run ./cmd/release -h`). The version comes from the `VERSION`
file (`-version` or `DIVVY_RELEASE_VERSION` overrides it for a package:
`DIVVY_RELEASE_VERSION=0.2.0 task package:linux`); a plain `go build` reports `dev`.

### Packages

The Windows installer is cross-compiled, so it builds on Linux too. It installs for the current
user to `%LOCALAPPDATA%\Divvy` and uses the same registry keys as the installer of
earlier releases, so it upgrades such an install in place. It needs `makensis`
(Debian/Ubuntu: `nsis`; Arch: `nsis` from the AUR). Distribution packages ship either the 64-bit or the 32-bit NSIS
stubs; either works. When the system NSIS has neither, the task adds the 32-bit ones from the
official NSIS zip (pinned SHA-256) under `target/go/nsis/`.

The AppImage needs `file` and `readelf` (binutils). The macOS package needs a Mac. Nothing is
code-signed yet; the macOS bundle is signed ad hoc only.

### Game translations

The translations for non-English game clients come from an installed copy of the game. Its
files are Oodle-compressed, which only the small Rust tool in `tools/game-extract` reads. It is
the one Rust program left in the repository and needs only a Rust toolchain:

```sh
cargo run --release --manifest-path tools/game-extract/Cargo.toml -- l10n <game dir> target/l10n-raw.json
go run ./cmd/datarefresh l10n target/l10n-raw.json
```

`<game dir>` is the folder holding `Bundles2/`. Run it after `make data:refresh`, which keeps
the translations it finds. `game-extract file <game dir> <path> <out>` copies a single game
file out (`list:<text>` as the path lists the matching paths instead).

### End-to-end tests

The e2e tests run the real app against `e2e/cmd/fake-game` (a window titled "Path of Exile 2"
that puts an item on the clipboard when it gets the copy chord; X11, native Wayland, Windows and
macOS) and a mock trade2 API. They are Go tests in `e2e/` behind the `e2e` build tag. They start
a test-hook build of the app (`-tags testhook`) with a fresh `DIVVY_CONFIG_DIR`, point its trade,
poe2scout and poe.ninja URLs at the mock, and drive it over the test hook on 127.0.0.1. The
hook's `dom <window> <selector>` command checks what the overlay and settings pages render,
without WebDriver.

The scripts build the web UI, the test-hook app (`target/go/divvy-e2e`) and the fake game
(`target/e2e/fake-game`), set up the session, then run `go test -tags e2e ./e2e/...`.
`e2e/scripts/fake-game-smoke-{x11,wayland}.sh` check the fake game on its own (copy chords,
clipboard, `--log-keys`).

```sh
make e2e:x11            # Linux: Xvfb + openbox, real Ctrl+D presses (xdotool)
make e2e:wayland        # Linux: headless sway + portal + virtual keyboard (E2E_XWAYLAND=1 for XWayland)
make e2e:stacking       # Linux: overlay stacking over a fullscreen game (needs the binaries above)
make e2e:macos          # macOS desktop (bash e2e/scripts/run-macos.sh)
make e2e:windows        # Windows desktop (pwsh e2e/scripts/run-windows.ps1)
```

`RUN=<regexp>` picks tests (`make e2e:x11 RUN=TestPriceCheck`). `SKIP_BUILD=1` (`-SkipBuild`
for the PowerShell script) reuses the binaries. Each script lists the tools it needs in its
header comment. The `DIVVY_E2E_*` variables (binary paths, how the price check is triggered,
where logs are kept) are listed in `e2e/doc.go`. With a display already running,
`go test -tags e2e ./e2e/...` works on its own, and `-run TestMockTrade` needs no display.

## Commits and pull requests

We use conventional commits, enforced by commitlint on every commit and on the PR title.
The allowed types and scopes are listed in
[docs/contributing/commit-message.md](docs/contributing/commit-message.md) (generated from
`.commit-config.toml`; edit that file and run `go generate ./...` to change them).

- Header: `<type>(<scope>): <summary>`, at most 72 characters, imperative, lowercase, no period.
- Body: explain **why**, not what.
- Keep commits small and atomic; one logical change each.

Before opening a PR, run `make check` and, for web changes, `pnpm --dir web lint`,
`pnpm --dir web typecheck` and `pnpm --dir web coverage`. If you change the IPC service or its types, run `go generate ./...`
and commit `web/src/bindings.ts`.

### Reporting a wrong price

Use the "Price check is wrong" issue form and paste the copied item text (Ctrl+Alt+C in game).
A fix should add that text to `internal/core/testdata/corpus/` so it stays fixed.

## Releases (maintainers)

Bump the `VERSION` file in a `release: v<version>` pull request, then push the tag
`v<version>` on the merged commit. The tag runs `.github/workflows/release.yml`, which produces
a draft GitHub release whose notes GitHub generates from the merged pull requests, grouped by
the labels `pr-labels.yml` applies from each title. See
[docs/contributing/releasing.md](docs/contributing/releasing.md).
