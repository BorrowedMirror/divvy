# Brand: Divine Orb

Divvy's icon is the Path of Exile 2 Divine Orb inventory icon, used as it is, the way Awakened
PoE Trade uses the Awakener's Orb and Exiled Exchange 2 uses the Perfect Jeweller's Orb. The name
riffs on "div", the game's shorthand for a Divine Orb.

The artwork belongs to Grinding Gear Games and is not covered by the project's licence. Divvy is
not affiliated with or endorsed by Grinding Gear Games. A logo of our own may replace it later.

| File | Use |
| --- | --- |
| `divine-orb.png` | Source art: 108 × 108 px with a transparent background, from the poe2wiki copy of the game's `Divine Orb inventory icon.png`. Everything below is rendered from it. |

## Regenerating

```sh
./assets/brand/build-icons.sh
```

It needs ffmpeg and Go (Wails' icon generator, pinned in `tools/go.mod`, writes the `.ico` and
`.icns`; it runs with cgo off, so no GTK headers are needed). It writes:

| File | Size | Notes |
| --- | --- | --- |
| `build/appicon.png` | 256 px | Linux AppImage icon |
| `build/windows/icon.ico` | 16-256 px | Windows executable and installer |
| `build/darwin/icons.icns` | up to 1024 px | macOS bundle, with Apple's 824/1024 margin |
| `cmd/divvy/icons/icon.png` | 512 px | window icon |
| `cmd/divvy/icons/tray.png` | 64 px | Windows/Linux tray |
| `cmd/divvy/icons/tray-template.png` | 44 px | macOS menu bar (22 pt @2x): a black silhouette, since macOS uses only its alpha |
| `web/src/assets/divvy-mark.png` | 108 px | in-app header mark |

The source is small, so the large sizes are upscaled and look soft. `./assets/brand/build-icons.sh
<dir>` writes the same tree under `<dir>` instead, to compare before committing.
