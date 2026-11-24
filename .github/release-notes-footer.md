
### Install

| System | Download |
| --- | --- |
| Windows 10/11 | [`divvy-${VERSION}-windows-x64-setup.exe`](https://github.com/${REPOSITORY}/releases/download/${TAG}/divvy-${VERSION}-windows-x64-setup.exe), a per-user installer (no administrator rights) |
| macOS 12+ | [`divvy-${VERSION}-macos-universal.dmg`](https://github.com/${REPOSITORY}/releases/download/${TAG}/divvy-${VERSION}-macos-universal.dmg), Apple silicon and Intel |
| Linux (x86_64) | [`divvy-${VERSION}-linux-x86_64.AppImage`](https://github.com/${REPOSITORY}/releases/download/${TAG}/divvy-${VERSION}-linux-x86_64.AppImage); make it executable and run it |

Installed copies update themselves; the other `.exe`, `.app.zip` and `update.json` assets are
what they download. Divvy is not code-signed yet, so Windows SmartScreen and macOS Gatekeeper
warn on first launch, and macOS asks for the permissions in the
[README](https://github.com/${REPOSITORY}#macos-12).

### Verify

Every asset is listed in `SHA256SUMS.txt` and carries SLSA build provenance:

```sh
gh attestation verify divvy-${VERSION}-linux-x86_64.AppImage --repo ${REPOSITORY} \
  --signer-workflow ${REPOSITORY}/.github/workflows/release-slsa.yml
```
