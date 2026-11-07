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

That rewrites the SVGs, the Tauri icon set in `apps/desktop/src-tauri/icons` (including
`tray.png` and `tray-template.png`) and the web header glyph. It needs `python3`,
`rsvg-convert`, ImageMagick 7 and `cargo tauri`.
