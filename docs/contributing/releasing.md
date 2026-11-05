# Releasing

1. Make sure `master` is green and up to date locally.
2. `cargo xtask release 0.2.0 --dry-run` to see what will change, then
   `cargo xtask release 0.2.0`. This:
   - sets the version in `Cargo.toml` (`[workspace.package]`), `apps/desktop/src-tauri/tauri.conf.json`
     and `web/package.json`, and refreshes `Cargo.lock`;
   - regenerates `CHANGELOG.md` with git-cliff (`.cliff.toml`);
   - commits `release: v0.2.0` and creates the annotated tag `v0.2.0`.
   It never pushes.
3. Review the commit, then `git push origin master v0.2.0`.
4. `.github/workflows/release.yml` creates a **draft** release, builds and signs the bundles
   (Windows MSI + NSIS, macOS universal notarised DMG, Linux AppImage/deb/rpm; no rpm for
   pre-releases, since RPM versions cannot contain `-`), uploads
   `latest.json` for the updater, `SHA256SUMS.txt`, a CycloneDX SBOM and
   `THIRD-PARTY-NOTICES.html`.
5. Check the draft (install one bundle per OS if possible) and publish it. Publishing makes
   `latest.json` visible to the updater.

Pre-releases use a SemVer pre-release suffix (`0.2.0-rc.1`); the workflow marks them as
pre-releases automatically.

## Secrets

Signing secrets exist only in the protected `release` environment (deployment branches/tags:
`v*`; required reviewer). Only the `build` job of `release.yml` uses that environment.

| Secret | Purpose |
| --- | --- |
| `TAURI_SIGNING_PRIVATE_KEY`, `TAURI_SIGNING_PRIVATE_KEY_PASSWORD` | updater signatures |
| `TAURI_UPDATER_PUBKEY` | matching public key; merged into `plugins.updater.pubkey` at build time |
| `APPLE_CERTIFICATE`, `APPLE_CERTIFICATE_PASSWORD`, `APPLE_SIGNING_IDENTITY` | macOS code signing |
| `APPLE_ID`, `APPLE_PASSWORD`, `APPLE_TEAM_ID` | macOS notarisation |

Generate the updater key pair once with `cargo tauri signer generate -w kalandra.key` and store
the private key, its password and the public key (the `.pub` file contents) as the secrets above.
The public key is not checked in: `release.yml` writes it into a copy of
`.github/tauri-release.conf.json`, and the build fails early when it is missing, because
`bundle.createUpdaterArtifacts` cannot sign without it. The secret overrides any
`plugins.updater.pubkey` in `tauri.conf.json`, so both must be the same key.

`CODECOV_TOKEN` is a repository secret used by `ci.yml`.
