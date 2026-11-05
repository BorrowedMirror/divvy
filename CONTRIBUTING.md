# Contributing to kalandra

Thanks for helping! kalandra is a Path of Exile 2 price-check overlay: a Rust workspace
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
  `cargo install --locked cargo-nextest cargo-llvm-cov cargo-deny cargo-machete cargo-about git-cliff`
  (or `cargo binstall` the same list).

### Linux (Debian/Ubuntu)

```sh
sudo apt-get install -y libwebkit2gtk-4.1-dev libgtk-3-dev libayatana-appindicator3-dev \
  librsvg2-dev libxdo-dev libgtk-layer-shell-dev libei-dev
# headless display tests (optional locally, used in CI)
sudo apt-get install -y xvfb openbox
```

Fedora: `webkit2gtk4.1-devel gtk3-devel libappindicator-gtk3-devel librsvg2-devel libxdo-devel gtk-layer-shell-devel libei-devel`.
Arch: `webkit2gtk-4.1 gtk3 libappindicator-gtk3 librsvg xdotool gtk-layer-shell libei`.

### macOS

Install the Xcode command line tools (`xcode-select --install`). Running the overlay needs the
Accessibility permission (System Settings -> Privacy & Security -> Accessibility) for your
terminal or the built app.

### Windows

Install the [Visual Studio Build Tools](https://visualstudio.microsoft.com/visual-cpp-build-tools/)
with the "Desktop development with C++" workload. WebView2 ships with Windows 10/11.

### First run

```sh
pnpm install                 # web deps + commitlint + lefthook
pnpm exec lefthook install   # git hooks: rustfmt/eslint on commit, commitlint on commit-msg
cargo test                   # default members (crates/*)
pnpm --dir web dev           # web UI on http://localhost:5173 (keep running for the app)
cargo run -p kalandra-desktop  # full app, loading the UI from the Vite dev server
                             # (or `cargo install tauri-cli` and `cargo tauri dev` in apps/desktop)
```

## Everyday commands

| Command | What it does |
| --- | --- |
| `cargo xtask lint` | rustfmt check, clippy `-D warnings` (workspace and `e2e/macos-harness`), cargo-deny, cargo-machete, eslint, tsc for web and e2e (`--no-web`, `--no-tools` to skip groups) |
| `cargo xtask coverage [--check]` | nextest under llvm-cov; prints per-crate line coverage, writes `target/coverage/<crate>.lcov`, `--check` enforces the gates |
| `cargo xtask gen` | regenerates `.commitlintrc.json`, `docs/contributing/commit-message.md` and `web/src/bindings.ts` |
| `cargo nextest run` | tests (profiles in `.config/nextest.toml`) |
| `pnpm --dir web test` / `coverage` | Vitest |

Coverage gates (CI fails below them): core, trade, data, config >= 90% lines; platform >= 70%;
web >= 80%; patch coverage >= 80%.

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

`cargo xtask release <version>` bumps every version, regenerates `CHANGELOG.md` with git-cliff,
commits `release: v<version>` and creates an annotated tag. It never pushes; pushing the tag runs
`.github/workflows/release.yml`, which produces a draft GitHub release. See
[docs/contributing/releasing.md](docs/contributing/releasing.md).
