# Brand: Chaos Claw

Three gold blades swirl clockwise out of a crowned rust core, and each blade ends in a crab claw.
It riffs on Path of Exile's Chaos Orb and on Ferris, the Rust crab. All of it is original artwork
under the project's licence (Apache-2.0); no Grinding Gear Games or third-party art is used.

| File | Use |
| --- | --- |
| `rkl-mark.svg` | Master mark on a milled coin: README, about screens, splash. 128 px and up. |
| `rkl-app-icon.svg` | App icon tile, 48 px and up. Source for the bundle PNGs, `.ico` and `.icns`. |
| `rkl-app-icon-macos.svg` | The tile with Apple's 824/1024 margin, for `.icns`. |
| `rkl-app-icon-32.svg`, `rkl-app-icon-16.svg` | Flat masters drawn for 32 px and 16 px icon slots. |
| `rkl-glyph.svg` | Ringed colour glyph for in-app headers (copied to `web/src/assets`). |
| `rkl-glyph-16.svg` | Outlined glyph for the Windows/Linux tray; works on light and dark trays. |
| `rkl-glyph-mono.svg` | One-colour (`currentColor`) glyph; the macOS menu-bar template icon. |

Below 48 px use the dedicated small versions, not a scaled-down master.

## Regenerating

The SVGs are generated; edit `generate.py`, not the SVGs. Then run:

```sh
./assets/brand/build-icons.sh
```

It needs python3, `rsvg-convert` (librsvg), ImageMagick 7 (`magick`) and Go (Wails' icon
generator, pinned in `tools/go.mod`, writes the `.icns`). It rewrites the SVGs, the web header
glyph (`web/src/assets/rkl-glyph.svg`) and the app icons, which live in:

| File | Size | From |
| --- | --- | --- |
| `build/appicon.png` | 256 px | `rkl-app-icon.svg` (Linux AppImage icon) |
| `build/windows/icon.ico` | 16-256 px | `rkl-app-icon-16.svg` (16, 24), `rkl-app-icon-32.svg` (32), `rkl-app-icon.svg` (48-256) |
| `build/darwin/icons.icns` | up to 1024 px | `rkl-app-icon-macos.svg` |
| `cmd/rkl/icons/icon.png` | 512 px | `rkl-app-icon.svg` (window icon) |
| `cmd/rkl/icons/tray.png` | 64 px | `rkl-glyph-16.svg` (Windows/Linux tray) |
| `cmd/rkl/icons/tray-template.png` | 44 px | `rkl-glyph-mono.svg` (macOS menu bar, 22 pt @2x) |

`./assets/brand/build-icons.sh <dir>` writes the same tree under `<dir>` instead, to compare
before committing. The PNGs and the `.ico` come out byte-identical for unchanged SVGs; the
`.icns` encoding differs from the committed one (made by an earlier tool), not its artwork.
