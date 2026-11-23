# Releasing

The version lives in the `VERSION` file (SemVer without a leading `v`, e.g. `0.2.0` or
`0.2.0-rc.1`). Packages and the app's diagnostics read it from there.

1. Make sure `master` is green.
2. Open a pull request that changes only `VERSION`, titled `release: v0.2.0`, and squash-merge
   it. The `release` type keeps it out of the release notes.
3. Tag the merged commit and push the tag:

   ```sh
   git switch master && git pull --ff-only
   git tag -a v0.2.0 -m "divvy 0.2.0"
   git push origin v0.2.0
   ```

4. The tag runs `.github/workflows/release.yml`. It fails early unless the tag is `v` plus the
   `VERSION` file at that commit. It then builds the Windows NSIS installer (cross-compiled on
   Linux), the Linux AppImage and the macOS universal DMG with
   `DIVVY_RELEASE_VERSION=<version> task package:<os>`, signs the update artifacts into
   `update.json` (see [Updates](#updates)), and creates a **draft** release with the packages,
   `update.json`, a CycloneDX SBOM, `THIRD-PARTY-NOTICES.html` and `SHA256SUMS.txt`. GitHub
   writes the draft's notes from the pull requests merged since the previous release, grouped by
   label (see [Release notes](#release-notes)).
5. Check the draft (install one package per OS if possible) and publish it. Publishing makes
   `update.json` visible to installed copies.

Pre-releases use a SemVer pre-release suffix (`0.2.0-rc.1`); the workflow marks them as
pre-releases automatically. To discard a run, delete the draft and the tag.

To try the packaging locally, run `make package:<os>` (see
[CONTRIBUTING.md](../../CONTRIBUTING.md#everyday-commands)); it writes to `target/go/`.

Nothing is code-signed yet: no Windows Authenticode signature, and the macOS app is signed ad
hoc only (no Developer ID, no notarisation). Users see the usual SmartScreen and Gatekeeper
warnings on first launch.

## Updates

Installed copies update themselves with Wails' updater (`internal/shell/updater.go`). Every
release build links in the updater's public key, and an install accepts only artifacts whose
SHA-256 digest is signed with the matching private key (raw ed25519). The update artifacts are
the bare Windows exe, the AppImage and the zipped `.app`;
`go run ./cmd/release update-manifest` signs them into `update.json`, whose download URLs point
at the release's tag.

Builds without a key (development builds, forks, local packages without
`DIVVY_UPDATER_PUBKEY`) never check for updates. Release builds read
`https://github.com/BorrowedMirror/divvy/releases/latest/download/update.json`.
GitHub's `latest` skips drafts and pre-releases, so installs only see a release once it is
published, and release-candidate builds update to the next stable release. The first check runs
30 s after start and then every 24 h while **Settings > General > Check for updates
automatically** (`ui.check_updates`) is on. A found update is downloaded only when the user
clicks **Install and restart**, and its signature is checked against the built-in key first.

### Keys

Generate the key pair once:

```sh
make updater:keygen
```

It prints two lines, `DIVVY_UPDATER_PUBKEY=...` and `DIVVY_UPDATER_PRIVATE_KEY=...`. Store both as
secrets of the `release` environment; the release fails without them. Installed builds trust
only the key they were built with, so keep the private key safe: losing or rotating it means
users must reinstall by hand to get later updates.

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

| Secret | Where | Purpose |
| --- | --- | --- |
| `DIVVY_UPDATER_PUBKEY` | `release` environment | updater public key, linked into every package |
| `DIVVY_UPDATER_PRIVATE_KEY` | `release` environment | signs the update artifacts into `update.json` |
| `CODECOV_TOKEN` | repository | coverage uploads from `ci.yml` |
| `CROWDIN_PROJECT_ID`, `CROWDIN_PERSONAL_TOKEN` | repository | translation sync in `crowdin.yml` |

The `release` environment should be protected (deployment tags `v*`, a required reviewer).
Only the `build` and `draft` jobs of `release.yml` use it.
