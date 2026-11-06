//! What the chat placeholders need from the game's `Client.txt`: the last whisper partner
//! (`@last`) and the current area (`@area`). Read on demand, backwards from the end of the log,
//! when a command uses one; nothing watches the file.

use std::io::{Read, Seek, SeekFrom};
use std::path::{Path, PathBuf};

/// Values found in the log, newest wins.
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct LogVars {
    pub last_whisper: Option<String>,
    pub area: Option<String>,
}

/// The game's message text: everything after the `YYYY/MM/DD HH:MM:SS ticks id [INFO Client n] `
/// prefix.
fn message(line: &str) -> &str {
    line.split_once("] ").map_or(line, |(_, m)| m)
}

/// `@From <GUILD> Name: hi` or `@To Name: hi` (either direction) → `Name`.
pub fn whisper_partner(line: &str) -> Option<&str> {
    let msg = message(line);
    let rest = msg.strip_prefix("@From ").or_else(|| msg.strip_prefix("@To "))?;
    let rest = match rest.strip_prefix('<') {
        Some(tagged) => tagged.split_once("> ")?.1,
        None => rest,
    };
    let (name, _) = rest.split_once(": ")?;
    (!name.is_empty() && !name.contains(char::is_whitespace)).then_some(name)
}

/// `[SCENE] Set Source [Felled Hideout]` → `Felled Hideout`. The game also logs `(null)` and
/// `(unknown)` while loading; those are skipped.
pub fn scene_area(line: &str) -> Option<&str> {
    let area = message(line).strip_prefix("[SCENE] Set Source [")?.strip_suffix(']')?;
    (!area.is_empty() && !area.starts_with('(')).then_some(area)
}

/// Scan `lines` (newest first) until every wanted value is found.
pub fn scan<'a>(lines: impl IntoIterator<Item = &'a str>, want_last: bool, want_area: bool) -> LogVars {
    let mut vars = LogVars::default();
    for line in lines {
        if want_last && vars.last_whisper.is_none() {
            vars.last_whisper = whisper_partner(line).map(str::to_owned);
        }
        if want_area && vars.area.is_none() {
            vars.area = scene_area(line).map(str::to_owned);
        }
        if (!want_last || vars.last_whisper.is_some()) && (!want_area || vars.area.is_some()) {
            break;
        }
    }
    vars
}

/// Read the log from the end in growing windows (256 KiB, 4 MiB, 32 MiB) until the wanted
/// values turn up. A long session writes megabytes of log, so the last whisper can be far back.
pub fn read_vars(path: &Path, want_last: bool, want_area: bool) -> std::io::Result<LogVars> {
    let mut file = std::fs::File::open(path)?;
    let len = file.metadata()?.len();
    let mut vars = LogVars::default();
    for window in [1u64 << 18, 1 << 22, 1 << 25] {
        let start = len.saturating_sub(window);
        file.seek(SeekFrom::Start(start))?;
        let mut buf = Vec::with_capacity(usize::try_from(len - start).unwrap_or(0));
        file.by_ref().take(len - start).read_to_end(&mut buf)?;
        let text = String::from_utf8_lossy(&buf);
        // A window that starts mid-file starts mid-line: drop that partial line.
        let text = if start > 0 { text.split_once('\n').map_or("", |(_, t)| t) } else { &text };
        vars = scan(text.lines().rev(), want_last, want_area);
        let done = (!want_last || vars.last_whisper.is_some()) && (!want_area || vars.area.is_some());
        if done || start == 0 {
            break;
        }
    }
    Ok(vars)
}

/// Library folders listed in Steam's `libraryfolders.vdf` (`"path"  "D:\\SteamLibrary"`).
pub fn library_paths(vdf: &str) -> Vec<PathBuf> {
    vdf.lines()
        .filter_map(|l| {
            let rest = l.trim().strip_prefix("\"path\"")?.trim();
            let quoted = rest.strip_prefix('"')?.strip_suffix('"')?;
            Some(PathBuf::from(quoted.replace("\\\\", "\\")))
        })
        .collect()
}

const GAME_DIR: &str = "Path of Exile 2";

/// Where the log can be for a Steam root or library folder.
fn steam_log(library: &Path) -> PathBuf {
    library.join("steamapps").join("common").join(GAME_DIR).join("logs").join("Client.txt")
}

/// Every place the log usually is: each Steam library (from the Steam roots' library lists) and
/// the standalone client's default install.
pub fn candidates(home: Option<&Path>) -> Vec<PathBuf> {
    let mut roots: Vec<PathBuf> = Vec::new();
    let mut out = Vec::new();
    if cfg!(windows) {
        roots.push(PathBuf::from(r"C:\Program Files (x86)\Steam"));
        out.push(PathBuf::from(r"C:\Program Files (x86)\Grinding Gear Games\Path of Exile 2\logs\Client.txt"));
    } else if let Some(home) = home {
        for root in [".local/share/Steam", ".steam/steam", ".var/app/com.valvesoftware.Steam/.local/share/Steam"] {
            roots.push(home.join(root));
        }
        out.push(
            home.join(".wine/drive_c/Program Files (x86)/Grinding Gear Games").join(GAME_DIR).join("logs/Client.txt"),
        );
    }
    for root in roots {
        let mut libraries = vec![root.clone()];
        if let Ok(vdf) = std::fs::read_to_string(root.join("steamapps").join("libraryfolders.vdf")) {
            libraries.extend(library_paths(&vdf));
        }
        for lib in libraries {
            let log = steam_log(&lib);
            if !out.contains(&log) {
                out.push(log);
            }
        }
    }
    out
}

/// The log to read: the configured path when set, otherwise the most recently written
/// candidate that exists.
pub fn resolve(configured: &str) -> Option<PathBuf> {
    let configured = configured.trim();
    if !configured.is_empty() {
        let p = PathBuf::from(configured);
        return p.is_file().then_some(p);
    }
    let home = std::env::var_os("HOME").or_else(|| std::env::var_os("USERPROFILE")).map(PathBuf::from);
    candidates(home.as_deref())
        .into_iter()
        .filter_map(|p| Some((std::fs::metadata(&p).ok()?.modified().ok()?, p)))
        .max_by_key(|(t, _)| *t)
        .map(|(_, p)| p)
}

#[cfg(test)]
mod tests {
    use super::*;

    const LOG: &str = "\
2026/11/06 11:15:34 172089589 2caa229f [DEBUG Client 348] Generating level 65 area \"HideoutFelled\" with seed 1
2026/11/06 11:15:34 172089622 7fbd1225 [INFO Client 348] [SCENE] Set Source [Felled Hideout]
2026/11/06 11:16:00 172089700 3ef232c2 [INFO Client 348] @From <GUILD> Seller_One: Hi, I would like to buy your ring
2026/11/06 11:17:00 172089800 3ef232c2 [INFO Client 348] @To Buyer: ty
2026/11/06 11:18:00 172089900 3ef232c2 [INFO Client 348] #Chatter: @To Fake: not a whisper
2026/11/06 13:30:10 180165376 7fbd1225 [INFO Client 480] [SCENE] Set Source [(null)]
2026/11/06 13:30:10 180165393 7fbd1225 [INFO Client 480] [SCENE] Set Source [(unknown)]
";

    #[test]
    fn parses_whispers_and_scenes() {
        let lines: Vec<&str> = LOG.lines().collect();
        assert_eq!(whisper_partner(lines[2]), Some("Seller_One"));
        assert_eq!(whisper_partner(lines[3]), Some("Buyer"));
        assert_eq!(whisper_partner(lines[4]), None);
        assert_eq!(scene_area(lines[1]), Some("Felled Hideout"));
        assert_eq!((scene_area(lines[0]), scene_area(lines[5]), scene_area(lines[6])), (None, None, None));
        let vars = scan(LOG.lines().rev(), true, true);
        assert_eq!(vars, LogVars { last_whisper: Some("Buyer".into()), area: Some("Felled Hideout".into()) });
        assert_eq!(scan(LOG.lines().rev(), false, true).last_whisper, None);
    }

    #[test]
    fn reads_backwards_past_the_first_window() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("Client.txt");
        // The whisper sits before 300 KiB of other lines, outside the first 256 KiB window.
        let filler = "2026/11/06 12:00:00 1 2 [INFO Client 1] something else\n".repeat(6000);
        std::fs::write(&path, format!("{LOG}{filler}")).unwrap();
        let vars = read_vars(&path, true, true).unwrap();
        assert_eq!(vars.last_whisper.as_deref(), Some("Buyer"));
        assert_eq!(vars.area.as_deref(), Some("Felled Hideout"));
        std::fs::write(&path, "").unwrap();
        assert_eq!(read_vars(&path, true, true).unwrap(), LogVars::default());
    }

    #[test]
    fn finds_steam_libraries() {
        let vdf = "\"libraryfolders\"\n{\n\t\"0\"\n\t{\n\t\t\"path\"\t\t\"/home/u/.local/share/Steam\"\n\t}\n\t\"1\"\n\t{\n\t\t\"path\"\t\t\"D:\\\\SteamLibrary\"\n\t}\n}\n";
        assert_eq!(
            library_paths(vdf),
            vec![PathBuf::from("/home/u/.local/share/Steam"), PathBuf::from(r"D:\SteamLibrary")]
        );
        let dir = tempfile::tempdir().unwrap();
        let home = dir.path();
        let root = home.join(".local/share/Steam/steamapps");
        std::fs::create_dir_all(&root).unwrap();
        let other = home.join("Games");
        std::fs::write(root.join("libraryfolders.vdf"), format!("\"path\" \"{}\"", other.display())).unwrap();
        if !cfg!(windows) {
            assert!(candidates(Some(home)).contains(&steam_log(&other)));
        }
        assert_eq!(resolve(&home.join("missing.txt").display().to_string()), None);
    }
}
