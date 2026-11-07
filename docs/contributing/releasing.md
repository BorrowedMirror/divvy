# Releasing

1. Make sure `master` is green and up to date locally.
2. `cargo xtask release 0.2.0 --dry-run` to see what will change, then
   `cargo xtask release 0.2.0`. This:
   - sets the version in `Cargo.toml` (`[workspace.package]`), `apps/desktop/src-tauri/tauri.conf.json`
     and `web/package.json`, and refreshes `Cargo.lock`;
   - commits `release: v0.2.0` and creates the annotated tag `v0.2.0`.
   It never pushes.
3. Review the commit, then `git push origin master v0.2.0`.
4. `.github/workflows/release.yml` creates a **draft** release, builds and signs the bundles
   (Windows MSI + NSIS, macOS universal notarised DMG, Linux AppImage/deb/rpm; no rpm for
   pre-releases, since RPM versions cannot contain `-`), uploads
   `latest.json` for the updater, `SHA256SUMS.txt`, a CycloneDX SBOM and
   `THIRD-PARTY-NOTICES.html`.
   GitHub writes the draft's notes from the pull requests merged since the previous release,
   grouped by label (see [Release notes](#release-notes)).
5. Check the draft (install one bundle per OS if possible) and publish it. Publishing makes
   `latest.json` visible to the updater.

Pre-releases use a SemVer pre-release suffix (`0.2.0-rc.1`); the workflow marks them as
pre-releases automatically.

## Go build releases

While the Go/Wails port is in progress, `.github/workflows/release-go.yml` releases the Go
build separately. It is started by hand (Actions -> release-go -> Run workflow) with a version
such as `0.2.0-rc.1`; `release.yml` keeps the `v*` tags.

It builds the Windows NSIS installer (cross-compiled on Linux), the Linux AppImage and the
macOS universal DMG with `RKL_RELEASE_VERSION=<version> task package:<os>`, signs the update artifacts
into `update.json` with `go run ./cmd/release update-manifest`, then creates a
**draft** release for the tag `go-v<version>` at the commit the run used, with GitHub-generated
notes, a CycloneDX SBOM and `SHA256SUMS.txt`. The tag is created only when the draft is
published; delete the draft to discard the run.

Updates: every build links in the updater's public key, and installs only accept artifacts
whose SHA-256 digest is signed with the matching private key (raw ed25519). Generate the pair
once with `task updater:keygen` and store both lines as secrets of the `release` environment
(`RKL_UPDATER_PUBKEY`, `RKL_UPDATER_PRIVATE_KEY`); the release fails without them. Installed
builds trust only the key they were built with, so keep the private key safe.

Not done yet for the Go build: Windows code signing, macOS Developer ID signing and
notarisation (the app is signed ad hoc), and third-party notices.

## Release notes

There is no changelog file. Each release's notes are GitHub's generated release notes: the pull
requests merged since the previous release, grouped into the categories in `.github/release.yml`
by their labels. `.github/workflows/pr-labels.yml` sets those labels from the pull request's
Conventional Commits title whenever it is opened or edited:

- `type/<type>` from the type (`feat` -> Features, `fix` -> Bug fixes, and so on);
- `area/<scope>` from the scope; `area/deps` (Renovate's `build(deps)`) goes under Dependencies;
- `type/breaking-change` for `type!:` or a `BREAKING CHANGE:` footer in the body or a commit.

The same workflow puts each pull request in the milestone of the release it will ship in
(created on first use): the next major for a breaking change, the next minor for `feat`, and
the next patch for anything else, counted from the latest published release (drafts and
pre-releases don't count, so nothing gets a milestone until the first stable release). A
`release: vX.Y.Z` pull request goes in `vX.Y.Z`. A milestone set by hand is left alone; one the
workflow set is recomputed when the title changes.

`type/release` pull requests are left out, and anything unlabelled lands under Other changes.
Pull requests are squash-merged, so the title is the commit message; fix a wrong title before
merging, or relabel the pull request by hand before publishing the draft. Commits pushed
straight to `master` without a pull request do not appear in the notes.

## Secrets

Signing secrets exist only in the protected `release` environment (deployment branches/tags:
`v*`; required reviewer). Only the `build` job of `release.yml` uses that environment.

| Secret | Purpose |
| --- | --- |
| `TAURI_SIGNING_PRIVATE_KEY`, `TAURI_SIGNING_PRIVATE_KEY_PASSWORD` | updater signatures |
| `TAURI_UPDATER_PUBKEY` | matching public key; merged into `plugins.updater.pubkey` at build time |
| `APPLE_CERTIFICATE`, `APPLE_CERTIFICATE_PASSWORD`, `APPLE_SIGNING_IDENTITY` | macOS code signing |
| `APPLE_ID`, `APPLE_PASSWORD`, `APPLE_TEAM_ID` | macOS notarisation |

Generate the updater key pair once with `cargo tauri signer generate -w RKL.key` and store
the private key, its password and the public key (the `.pub` file contents) as the secrets above.
The public key is not checked in: `release.yml` writes it into a copy of
`.github/tauri-release.conf.json`, and the build fails early when it is missing, because
`bundle.createUpdaterArtifacts` cannot sign without it. The secret overrides any
`plugins.updater.pubkey` in `tauri.conf.json`, so both must be the same key.

## Auto-update

The app's updater (`apps/desktop/src-tauri/src/updater.rs`) is opt-in by build config. It is
registered only when the merged config has a non-empty `plugins.updater.pubkey`, which only
release builds get. Dev builds, forks and local `cargo tauri build` runs have no key, so they
never check for updates and record no errors about it. Keep the pubkey out of the committed
`tauri.conf.json`; a unit test enforces this.

Release builds read `plugins.updater.endpoints` from `.github/tauri-release.conf.json`
(`https://github.com/BorrowedMirror/rust-king-ledger/releases/latest/download/latest.json`). GitHub's
`latest` skips drafts and pre-releases, so installs only see a release once it is published,
and rc builds update to the next stable release. The first check runs 30 s after startup and
then every 24 h while **Settings > General > Check for updates automatically**
(`ui.check_updates`) is on. A found update is downloaded only when the user clicks
**Install and restart**, and its signature is checked against the embedded pubkey first.

Keep the private key safe. Installed builds trust only the pubkey they were built with, so
losing or rotating the key means users must reinstall by hand to get later updates.

`CODECOV_TOKEN` is a repository secret used by `ci.yml`.
