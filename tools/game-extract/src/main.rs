//! Reads files out of an installed Path of Exile 2 client, whose bundles are
//! Oodle-compressed. Only this step needs Rust (the `oozextract` decompressor); the data
//! pipeline itself is `go run ./cmd/datarefresh`.
//!
//! ```text
//! game-extract file <game_dir> <path> <out>        copy one file, e.g. data/balance/german/clientstrings.datc64
//! game-extract file <game_dir> list:<text> <out>   write the paths containing <text>, one per line
//! game-extract l10n <game_dir> <out.json> [folder...]
//!                                                  the tables `datarefresh l10n` builds translations from
//! ```
//!
//! `<game_dir>` is the game's install directory (the one holding `Bundles2/`).
use std::path::Path;

use anyhow::bail;

mod bundle;
mod l10n;

const USAGE: &str = "usage:
  game-extract file <game_dir> <path> <out>         copy one file out of the game's bundles
  game-extract file <game_dir> list:<text> <out>    write the paths containing <text>
  game-extract l10n <game_dir> <out.json> [folder...]
                                                    write the tables for `go run ./cmd/datarefresh l10n`";

/// Copy one file out of an installed client's bundles, or list the paths containing a text.
fn file(game_dir: &Path, path: &str, out: &Path) -> anyhow::Result<()> {
    let files = bundle::GameFiles::open(game_dir)?;
    if let Some(pattern) = path.strip_prefix("list:") {
        let mut found: Vec<&String> = files.paths().iter().filter(|p| p.contains(pattern)).collect();
        found.sort();
        let text = found.iter().fold(String::new(), |mut acc, p| {
            acc.push_str(p);
            acc.push('\n');
            acc
        });
        std::fs::write(out, text)?;
        eprintln!("wrote {} matching paths to {}", found.len(), out.display());
        return Ok(());
    }
    let data = files.read(path)?;
    std::fs::write(out, &data)?;
    eprintln!("wrote {} ({} bytes)", out.display(), data.len());
    Ok(())
}

fn main() -> anyhow::Result<()> {
    let args: Vec<String> = std::env::args().skip(1).collect();
    match args.iter().map(String::as_str).collect::<Vec<_>>().as_slice() {
        ["file", game_dir, path, out] => file(Path::new(game_dir), path, Path::new(out)),
        ["l10n", game_dir, out, ..] => l10n::run(Path::new(game_dir), Path::new(out), &args[3..]),
        ["-h" | "--help" | "help"] => {
            println!("{USAGE}");
            Ok(())
        }
        _ => {
            eprintln!("{USAGE}");
            bail!("unknown command or wrong number of arguments")
        }
    }
}
