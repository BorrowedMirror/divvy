# Contributing to RKL

Thanks for helping! RKL is a Path of Exile 2 price-check overlay: a Rust workspace
(`crates/*`), a Tauri 2 shell (`apps/desktop`) and a React 19 + Vite web UI (`web/`).
See [docs/architecture.md](docs/architecture.md) for how the pieces fit together.

By participating you agree to the [Code of Conduct](CODE_OF_CONDUCT.md). Security issues go
through [SECURITY.md](SECURITY.md), not public issues.

## Development setup

All platforms need:

- **Rust** via [rustup](https://rustup.rs). The toolchain is pinned in `rust-toolchain.toml`
  and installed automatically on first `cargo` invocation (or run `rustup toolchain install`).
- **Node.js 24** and **pnpm** (version pinned by `packageManager` in `package.json`; `corepack enable`
  picks it up).
- Cargo tools used by the checks:
  `cargo install --locked cargo-nextest cargo-llvm-cov cargo-deny cargo-machete cargo-about`
  (or `cargo binstall` the same list).

### Linux (Debian/Ubuntu)

```sh
sudo apt-get install -y libwebkit2gtk-4.1-dev libgtk-3-dev libayatana-appindicator3-dev \
  librsvg2-dev libxdo-dev libgtk-layer-shell-dev
# headless display tests (optional locally, used in CI)
sudo apt-get install -y xvfb openbox xdotool
```

Fedora: `webkit2gtk4.1-devel gtk3-devel libappindicator-gtk3-devel librsvg2-devel libxdo-devel gtk-layer-shell-devel`.
Arch: `webkit2gtk-4.1 gtk3 libappindicator-gtk3 librsvg xdotool gtk-layer-shell`.

libei is not needed: the Wayland backend injects keystrokes through the virtual-keyboard
protocol or `ydotool` (see `crates/platform/Cargo.toml`).

### macOS

Install the Xcode command line tools (`xcode-select --install`). Running the overlay needs the
Accessibility permission, and preferably Screen Recording, for your terminal or the built app
(System Settings -> Privacy & Security); see the [README](README.md#macos-13) and
[docs/research/macos.md](docs/research/macos.md). Dev builds that are re-signed on every build
can lose these grants.

### Windows

Install the [Visual Studio Build Tools](https://visualstudio.microsoft.com/visual-cpp-build-tools/)
with the "Desktop development with C++" workload. WebView2 ships with Windows 10/11.

### Windows installer from Linux

`cargo xtask windows-installer` builds the NSIS setup `.exe` without a Windows machine. It needs
`clang`, `lld`, `llvm`, `unzip`, `makensis` (Arch: `nsis` from the AUR) and
`cargo install cargo-xwin --locked`. It runs `cargo tauri build` with `cargo-xwin` and writes
`target/x86_64-pc-windows-msvc/release/bundle/nsis/*-setup.exe`. When the system NSIS lacks the
32-bit stubs Tauri's installer needs, it adds them under `target/nsis-x86/` from the official NSIS
zip (checked against a pinned SHA-256). The installer is not code-signed, and an MSI still needs
Windows (WiX).

### First run

```sh
pnpm install                 # web + e2e deps, commitlint, lefthook
pnpm exec lefthook install   # hooks from .lefthook.yml: rustfmt check + biome on commit, commitlint on commit-msg
cargo test                   # default members (crates/*)
pnpm --dir web dev           # web UI on http://localhost:5173 (keep running for the app)
cargo run -p rkl-desktop  # full app, loading the UI from the Vite dev server
                             # (or `cargo install tauri-cli` and `cargo tauri dev` in apps/desktop)
```

## Everyday commands

| Command | What it does |
| --- | --- |
| `cargo xtask lint` | rustfmt check, clippy `-D warnings` (workspace and `e2e/macos-harness`), cargo-deny, cargo-machete, biome, tsc for web and e2e (`--no-web`, `--no-tools` to skip groups) |
| `cargo xtask coverage [--check] [--no-run] [--profile <p>]` | nextest under llvm-cov; prints per-crate line coverage, writes `target/coverage/<crate>.lcov`, `--check` enforces the gates |
| `cargo xtask gen [--no-bindings]` | regenerates `.commitlintrc.json`, `docs/contributing/commit-message.md` (from `.commit-config.toml`) and `web/src/bindings.ts` |
| `cargo xtask data refresh [--offline]` | re-downloads and rebuilds the bundled game data in `crates/data/assets` |
| `cargo xtask windows-installer [-- <tauri args>]` | cross-compiles the Windows NSIS installer on Linux; see [Windows installer from Linux](#windows-installer-from-linux) |
| `cargo xtask fixtures record` | re-records trade fixtures from the live API (2 s between requests) |
| `cargo nextest run` | tests (profiles in `.config/nextest.toml`) |
| `pnpm --dir web test` / `coverage` / `lint` / `typecheck` | Vitest, biome, tsc |

Coverage gates (CI fails below them): core, trade, data, config >= 90% lines; platform >= 70%;
web >= 80%; patch coverage >= 80%.

### End-to-end tests

The e2e suites drive the real app (built with the test hook) against `tools/fake-game` and a
mock trade2 server; see `e2e/` and [docs/contributing/ci.md](docs/contributing/ci.md).

```sh
bash e2e/scripts/run-x11.sh          # Linux X11: Xvfb + openbox, WebDriver specs
bash e2e/scripts/run-wayland.sh      # Linux Wayland: headless sway + portal, WebDriver specs
bash e2e/scripts/run-hook-x11.sh     # Linux X11: hook harness, no WebDriver
pwsh e2e/scripts/run-windows.ps1     # Windows: tauri-driver + msedgedriver
cargo run --manifest-path e2e/macos-harness/Cargo.toml   # macOS: hook harness
```

The scripts build the web UI, the app (`--features test-hook,tauri/custom-protocol`) and
`fake-game` themselves; `SKIP_BUILD=1` (`-SkipBuild` on Windows) reuses existing binaries. The
macOS harness does not build: run `pnpm --dir web build`, then
`cargo build -p rkl-desktop --features test-hook,tauri/custom-protocol` and
`cargo build -p fake-game` first. Each
script lists the tools it needs in its header comment. The knobs are the `RKL_E2E_*` variables in the
[README](README.md#environment-variables).

## Go build

RKL is being ported to Go and [Wails v3](https://v3.wails.io) (`cmd/rkl`, `internal/*`). Until
the port is finished both builds live in the repository, and the Rust commands above keep
working. The Go build needs the Go version in `go.mod` (the `toolchain` line is downloaded
automatically) plus the same Linux packages, Xcode tools or Windows setup as the Rust build. The
binary embeds the web UI, so build that first:

```sh
pnpm --dir web build
go run -tags gtk3 ./cmd/rkl         # Linux; drop -tags gtk3 on Windows and macOS
```

Everyday work is plain Go: `go build`, `go test ./...`, `go vet ./...`, and `go generate ./...`
to regenerate `web/src/bindings.ts` after changing the IPC service. The rest are
[Task](https://taskfile.dev) tasks in `Taskfile.yml`, run with the Task runner built into the
Wails CLI, which is pinned with golangci-lint in `tools/go.mod`:

```sh
alias task='go tool -modfile=tools/go.mod wails3 task'   # or install Task
task --list
```

| Task | What it does |
| --- | --- |
| `task dev` | `wails3 dev`: Vite with hot reload in the app's windows, and a Go rebuild and restart on every Go change (`build/config.yml`) |
| `task run` | builds the frontend and a development binary (`target/go/rkl`) and runs it |
| `task check` | gofmt; go vet for the host, `GOOS=windows` and `GOOS=darwin` (`-tags purego`, since cgo needs the macOS SDK); `go test -race`; `bindgen -check`; golangci-lint for the same three targets (`.golangci.yml`) |
| `task coverage` | `go test -race` with one coverage profile per internal package in `target/go/coverage/` |
| `task package:windows` | cross-compiles `rkl.exe` (no cgo) with its icon, manifest and version info, then builds the per-user NSIS installer `target/go/rkl-<version>-windows-x64-setup.exe` and the update exe |
| `task package:linux` | builds `target/go/rkl-<version>-linux-x86_64.AppImage` with `wails3 generate appimage` (downloads linuxdeploy on first use) |
| `task package:macos` | universal `Rust King Ledger.app`, `target/go/rkl-<version>-macos-universal.dmg` and the update zip; macOS only |
| `task updater:keygen` | prints a new updater key pair (see docs/contributing/releasing.md) |

Development builds (`go build`, `task run`, `task dev`) are Wails dev builds: the web
inspector is available and `FRONTEND_DEVSERVER_URL` is honoured. Packages are built with the
`production` tag. Packaging steps the `go` command can't do (installers, signing updates) live
in `cmd/release`. The version comes from `apps/desktop/src-tauri/tauri.conf.json` (`-version`
overrides it: `task package:linux -- -version 0.2.0`).

The Windows installer installs for the current user to `%LOCALAPPDATA%\Rust King Ledger` and
uses the Tauri installer's registry keys, so it upgrades a Tauri install in place. It needs
`makensis` (Debian/Ubuntu: `nsis`; Arch: `nsis` from the AUR). Distribution packages ship
either the 64-bit or the 32-bit NSIS stubs; either works. When the system NSIS has neither,
the task adds the 32-bit ones from the official NSIS zip (pinned SHA-256) under
`target/go/nsis/`, as `cargo xtask windows-installer` does. The AppImage needs `file` and
`readelf` (binutils). Nothing is code-signed yet; the macOS bundle is signed ad hoc only.

## Commits and pull requests

We use conventional commits, enforced by commitlint on every commit and on the PR title.
The allowed types and scopes are listed in
[docs/contributing/commit-message.md](docs/contributing/commit-message.md) (generated from
`.commit-config.toml`; edit that file and run `cargo xtask gen` to change them).

- Header: `<type>(<scope>): <summary>`, at most 72 characters, imperative, lowercase, no period.
- Body: explain **why**, not what.
- Keep commits small and atomic; one logical change each.

Before opening a PR, run `cargo xtask lint` and `cargo xtask coverage --check`. If you change
IPC types, run `cargo xtask gen` and commit `web/src/bindings.ts`.

### Reporting a wrong price

Use the "Price check is wrong" issue form and paste the copied item text (Ctrl+Alt+C in game).
A fix should add that text to `crates/testkit/corpus/` so it stays fixed.

## Releases (maintainers)

`cargo xtask release <version>` bumps every version, commits `release: v<version>` and creates an
annotated tag. It never pushes; pushing the tag runs `.github/workflows/release.yml`, which
produces a draft GitHub release whose notes GitHub generates from the merged pull requests,
grouped by the labels `pr-labels.yml` applies from each title. See
[docs/contributing/releasing.md](docs/contributing/releasing.md).
