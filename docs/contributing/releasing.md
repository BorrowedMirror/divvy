# Releasing

The version lives in the `VERSION` file (SemVer without a leading `v`, e.g. `0.2.0` or
`0.2.0-rc.1`). Packages and the app's diagnostics read it from there.

1. Make sure `master` is green.
2. Open a pull request that changes only `VERSION`, titled `release: v0.2.0`, and squash-merge
   it. The `release` type keeps it out of the release notes.
3. Merging it tags the release: `.github/workflows/tag-release.yml` sees the squash-merged
   `release: v0.2.0 (#N)` commit on `master` and pushes the `v0.2.0` tag as the BorrowedMirror
   Bot GitHub App (a tag pushed with `GITHUB_TOKEN` would not start the release). Don't tag by
   hand: `release.yml` refuses a tag unless it points at that merged commit, on `master`, with
   `VERSION` holding the same version (`.github/scripts/release-commit.sh` checks both). If the
   tag push failed, run **Tag release** again from the Actions tab.
4. The tag runs `.github/workflows/release.yml`, which checks the tag and calls
   `.github/workflows/release-slsa.yml`. That builds the Windows NSIS installer (cross-compiled
   on Linux), the Linux AppImage and the macOS universal DMG with
   `DIVVY_RELEASE_VERSION=<version> task package:<os>`, signs the update artifacts into
   `update.json` (see [Updates](#updates)), and creates a **draft** release with the packages,
   `update.json`, a CycloneDX SBOM, `THIRD-PARTY-NOTICES.html` and `SHA256SUMS.txt`. Its last
   job attests every asset (see [Provenance](#provenance)) and attaches `divvy.intoto.jsonl`.
   GitHub writes the draft's notes from the pull requests merged since the previous release,
   grouped by label (see [Release notes](#release-notes)).
5. Check the draft (install one package per OS if possible) and publish it. Publishing makes
   `update.json` visible to installed copies.

Pre-releases use a SemVer pre-release suffix (`0.2.0-rc.1`); the workflow marks them as
pre-releases automatically. To discard a run, delete the draft and the tag; a version is
tagged once, so release the fix as the next version.

To try the packaging locally, run `make package:<os>` (see
[CONTRIBUTING.md](../../CONTRIBUTING.md#everyday-commands)); it writes to `target/go/`.

Nothing is code-signed yet: no Windows Authenticode signature, and the macOS app is signed ad
hoc only (no Developer ID, no notarisation). Users see the usual SmartScreen and Gatekeeper
warnings on first launch.

## Provenance

Every release asset carries SLSA Build Level 3 provenance from GitHub's artifact attestations,
signed with Sigstore by the reusable `release-slsa.yml` workflow rather than by the workflow
that called it. Check a download with:

```sh
gh attestation verify divvy-0.2.0-linux-x86_64.AppImage --repo BorrowedMirror/divvy \
  --signer-workflow BorrowedMirror/divvy/.github/workflows/release-slsa.yml
```

The same bundle is attached to each release as `divvy.intoto.jsonl`, for verifiers that work
offline. Attestations need the repository to be public (private repositories need GitHub
Enterprise Cloud), so the **Attest** job fails while it is private. Provenance says where and how
a file was built; it is not code signing, so it does not quiet SmartScreen or Gatekeeper.

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

It prints two lines, `DIVVY_UPDATER_PUBKEY=...` and `DIVVY_UPDATER_PRIVATE_KEY=...`. Store the
public key as a repository variable and the private key as a secret of the `release` environment;
the release fails without them. Installed builds trust
only the key they were built with, so keep the private key safe: losing or rotating it means
users must reinstall by hand to get later updates.

## Release notes

There is no changelog file. Each release's notes are GitHub's generated release notes: the pull
requests merged since the previous release, grouped into the categories in `.github/release.yml`
by their labels. `.github/workflows/pr-labels.yml` sets those labels from the pull request's
Conventional Commits title whenever it is opened or edited:

- `type/<type>` from the type (`feat` -> Features, `fix` -> Bug fixes, `revert` -> Reverts, and
  so on);
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

Revert through a pull request titled `revert: <the reverted title>`, not GitHub's
`Revert "..."` title, which commitlint rejects; name the reverted commit in the body.

After the notes, the **Draft release** job appends `.github/release-notes-footer.md`, with the
version, tag and repository filled in: which asset to download for each system, what to
expect from SmartScreen and Gatekeeper, and how to verify a download's provenance. Edit that
file to change what every release says.

## Secrets and variables

| Name | Kind | Where | Purpose |
| --- | --- | --- | --- |
| `DIVVY_UPDATER_PUBKEY` | variable | repository | updater public key, linked into every package |
| `DIVVY_UPDATER_PRIVATE_KEY` | secret | `release` environment | signs the update artifacts into `update.json` |
| `APP_CLIENT_ID` | variable | `automation` environment | the BorrowedMirror Bot GitHub App, which opens the data-refresh, fixtures and Crowdin pull requests and pushes release tags |
| `APP_PRIVATE_KEY` | secret | `automation` environment | that app's private key |
| `CROWDIN_PROJECT_ID`, `CROWDIN_PERSONAL_TOKEN` | secrets | `automation` environment | translation sync in `crowdin.yml` |

Coverage uploads to Codecov use OIDC, so there is no Codecov token.

The `release` environment deploys only from `v*` tags and should require a reviewer; only the
`Draft release` job of `release-slsa.yml` uses it, so a release asks for one approval, for the job that signs.
The `automation` environment deploys only from `master`. Both rules are enforced once the
repository is public (private repositories on GitHub Free cannot enforce them).
