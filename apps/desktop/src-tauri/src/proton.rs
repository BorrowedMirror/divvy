//! Proton/Wine mouse capture for Path of Exile 2 (Linux).
//!
//! In fullscreen, Wine turns the game's cursor clipping into an X pointer grab, so clicks never
//! reach the overlay. The Wine X11 driver options `GrabPointer` / `GrabFullscreen` (in the game's
//! Proton prefix `user.reg`) control this. This module reads and toggles them with exactly the
//! same file format and state file as `tools/linux/proton-mouse-grab.sh`, so the two are
//! interchangeable.
use std::path::{Path, PathBuf};

use serde::{Deserialize, Serialize};

use crate::ipc::IpcError;

/// Steam app id of Path of Exile 2.
pub const POE2_APPID: &str = "2694490";
const SECTION: &str = r"[Software\\Wine\\X11 Driver]";
const KEYS: [&str; 2] = ["GrabPointer", "GrabFullscreen"];
const STATE_FILE: &str = ".kalandra-mouse-grab.state";
const BACKUP_SUFFIX: &str = ".kalandra-backup";

/// Values of the two Wine options (`None` = unset, i.e. Wine's default).
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct GrabValues {
    #[serde(rename = "GrabPointer")]
    pub grab_pointer: Option<String>,
    #[serde(rename = "GrabFullscreen")]
    pub grab_fullscreen: Option<String>,
}

impl GrabValues {
    fn get(&self, key: &str) -> Option<&String> {
        if key == KEYS[0] { self.grab_pointer.as_ref() } else { self.grab_fullscreen.as_ref() }
    }
    fn disabled() -> Self {
        Self { grab_pointer: Some("N".into()), grab_fullscreen: Some("N".into()) }
    }
    /// Both options explicitly off.
    pub fn capture_disabled(&self) -> bool {
        self.grab_pointer.as_deref() == Some("N") && self.grab_fullscreen.as_deref() == Some("N")
    }
}

/// What the diagnostics page shows and toggles.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize, specta::Type)]
pub struct ProtonMouseStatus {
    /// Linux build with a PoE2 Proton prefix found.
    pub supported: bool,
    pub prefix: Option<String>,
    pub grab_pointer: Option<String>,
    pub grab_fullscreen: Option<String>,
    /// Mouse capture is turned off (both options `N`).
    pub capture_disabled: bool,
    /// kalandra changed the prefix and remembers the previous values.
    pub can_restore: bool,
    /// The game (or its Wine server) is running; changes are refused until it quits.
    pub game_running: bool,
}

/// Steam library roots listed in `libraryfolders.vdf` (plus the Steam root itself).
pub fn library_folders(vdf: &str, steam_root: &Path) -> Vec<PathBuf> {
    let mut out = vec![steam_root.to_path_buf()];
    for line in vdf.lines() {
        let line = line.trim();
        if let Some(rest) = line.strip_prefix("\"path\"") {
            let value = rest.trim().trim_matches('"').replace("\\\\", "\\");
            if !value.is_empty() {
                out.push(PathBuf::from(value));
            }
        }
    }
    out
}

/// Find the PoE2 Proton prefix in any Steam library under `home`.
pub fn find_prefix(home: &Path) -> Option<PathBuf> {
    let roots = [".local/share/Steam", ".steam/steam", ".var/app/com.valvesoftware.Steam/.local/share/Steam"];
    for root in roots.iter().map(|r| home.join(r)) {
        let Ok(vdf) = std::fs::read_to_string(root.join("steamapps/libraryfolders.vdf")) else { continue };
        for lib in library_folders(&vdf, &root) {
            let pfx = lib.join("steamapps/compatdata").join(POE2_APPID).join("pfx");
            if pfx.join("user.reg").is_file() {
                return Some(pfx);
            }
        }
    }
    None
}

fn section_bounds(lines: &[&str]) -> Option<(usize, usize)> {
    let start = lines.iter().position(|l| *l == SECTION || l.starts_with(&format!("{SECTION} ")))?;
    let end = lines[start + 1..].iter().position(|l| l.starts_with('[')).map_or(lines.len(), |i| start + 1 + i);
    Some((start, end))
}

fn parse_value(line: &str) -> Option<(&str, &str)> {
    let rest = line.strip_prefix('"')?;
    let (key, rest) = rest.split_once("\"=\"")?;
    let value = rest.strip_suffix('"')?;
    Some((key, value))
}

/// Read the two options from `user.reg` text.
pub fn read_values(user_reg: &str) -> GrabValues {
    let lines: Vec<&str> = user_reg.split('\n').collect();
    let mut vals = GrabValues::default();
    if let Some((start, end)) = section_bounds(&lines) {
        for line in &lines[start + 1..end] {
            match parse_value(line) {
                Some((k, v)) if k == KEYS[0] => vals.grab_pointer = Some(v.to_owned()),
                Some((k, v)) if k == KEYS[1] => vals.grab_fullscreen = Some(v.to_owned()),
                _ => {}
            }
        }
    }
    vals
}

/// `user.reg` text with the two options set to `vals` (`None` removes the value). Everything
/// else is left byte-for-byte unchanged.
pub fn write_values(user_reg: &str, vals: &GrabValues, now_unix: u64) -> String {
    let mut lines: Vec<String> = user_reg.split('\n').map(str::to_owned).collect();
    let refs: Vec<&str> = lines.iter().map(String::as_str).collect();
    let (start, end) = if let Some(b) = section_bounds(&refs) {
        b
    } else {
        lines.extend([format!("{SECTION} {now_unix}"), format!("#time={now_unix:x}"), String::new()]);
        let refs: Vec<&str> = lines.iter().map(String::as_str).collect();
        section_bounds(&refs).expect("section just added")
    };
    let mut body: Vec<String> = lines[start + 1..end]
        .iter()
        .filter(|l| !parse_value(l).is_some_and(|(k, _)| KEYS.contains(&k)))
        .cloned()
        .collect();
    while body.last().is_some_and(String::is_empty) {
        body.pop();
    }
    for key in KEYS {
        if let Some(v) = vals.get(key) {
            body.push(format!("\"{key}\"=\"{v}\""));
        }
    }
    body.push(String::new());
    lines.splice(start + 1..end, body);
    lines.join("\n")
}

/// Whether PoE2 or a Wine server is running (Wine rewrites `user.reg` on exit).
pub fn game_running(proc_root: &Path) -> bool {
    let Ok(entries) = std::fs::read_dir(proc_root) else { return false };
    entries.flatten().any(|e| {
        let p = e.path();
        let comm = std::fs::read_to_string(p.join("comm")).unwrap_or_default();
        if comm.trim() == "wineserver" {
            return true;
        }
        let cmdline = std::fs::read(p.join("cmdline")).unwrap_or_default();
        String::from_utf8_lossy(&cmdline).split('\0').next().is_some_and(|argv0| {
            let name = argv0.rsplit(['/', '\\']).next().unwrap_or("");
            name.starts_with("PathOfExile") && name.to_ascii_lowercase().ends_with(".exe")
        })
    })
}

fn io(e: impl std::fmt::Display) -> IpcError {
    IpcError::new("config", e.to_string())
}

/// Status of the given prefix (`None` = not found).
pub fn status_of(prefix: Option<&Path>, running: bool) -> ProtonMouseStatus {
    let Some(prefix) = prefix else {
        return ProtonMouseStatus {
            supported: false,
            prefix: None,
            grab_pointer: None,
            grab_fullscreen: None,
            capture_disabled: false,
            can_restore: false,
            game_running: running,
        };
    };
    let vals = std::fs::read_to_string(prefix.join("user.reg")).map(|t| read_values(&t)).unwrap_or_default();
    ProtonMouseStatus {
        supported: true,
        prefix: Some(prefix.display().to_string()),
        capture_disabled: vals.capture_disabled(),
        grab_pointer: vals.grab_pointer,
        grab_fullscreen: vals.grab_fullscreen,
        can_restore: prefix.join(STATE_FILE).is_file(),
        game_running: running,
    }
}

/// Turn Wine's mouse capture off (`disable = true`, remembering the previous values) or put the
/// remembered values back (`disable = false`).
pub fn apply(prefix: &Path, disable: bool, running: bool, now_unix: u64) -> Result<ProtonMouseStatus, IpcError> {
    if running {
        return Err(IpcError::new("busy", "quit Path of Exile 2 first; Wine rewrites its registry when it exits"));
    }
    let reg = prefix.join("user.reg");
    let state = prefix.join(STATE_FILE);
    let text = std::fs::read_to_string(&reg).map_err(io)?;
    let target = if disable {
        if !state.exists() {
            std::fs::write(&state, serde_json::to_string(&read_values(&text)).map_err(io)?).map_err(io)?;
        }
        GrabValues::disabled()
    } else {
        let saved = std::fs::read_to_string(&state).map_err(|_| IpcError::new("config", "nothing to restore"))?;
        serde_json::from_str(&saved).map_err(io)?
    };
    let backup = PathBuf::from(format!("{}{BACKUP_SUFFIX}", reg.display()));
    if !backup.exists() {
        std::fs::copy(&reg, &backup).map_err(io)?;
    }
    let tmp = prefix.join("user.reg.kalandra-tmp");
    std::fs::write(&tmp, write_values(&text, &target, now_unix)).map_err(io)?;
    std::fs::rename(&tmp, &reg).map_err(io)?;
    if !disable {
        std::fs::remove_file(&state).map_err(io)?;
    }
    Ok(status_of(Some(prefix), false))
}

fn home() -> Option<PathBuf> {
    std::env::var_os("HOME").map(PathBuf::from)
}

fn now_unix() -> u64 {
    std::time::SystemTime::now().duration_since(std::time::UNIX_EPOCH).map_or(0, |d| d.as_secs())
}

/// Current state of Wine's mouse capture for PoE2 (Linux only; `supported = false` elsewhere).
#[tauri::command]
#[specta::specta]
pub fn get_proton_mouse_status() -> ProtonMouseStatus {
    if !cfg!(target_os = "linux") {
        return status_of(None, false);
    }
    let prefix = home().and_then(|h| find_prefix(&h));
    status_of(prefix.as_deref(), game_running(Path::new("/proc")))
}

/// Disable (`true`) or restore (`false`) Wine's mouse capture for PoE2. Takes effect the next
/// time the game starts.
#[tauri::command]
#[specta::specta]
pub fn set_proton_mouse_capture(disabled: bool) -> Result<ProtonMouseStatus, IpcError> {
    if !cfg!(target_os = "linux") {
        return Err(IpcError::new("platform", "only needed for Proton on Linux"));
    }
    let prefix = home()
        .and_then(|h| find_prefix(&h))
        .ok_or_else(|| IpcError::new("config", "no Path of Exile 2 Proton prefix found"))?;
    apply(&prefix, disabled, game_running(Path::new("/proc")), now_unix())
}

#[cfg(test)]
mod tests {
    use super::*;

    const REG: &str = "WINE REGISTRY Version 2\n\n[Software\\\\Wine\\\\Fonts] 1\n\"A\"=\"b\"\n\n[Software\\\\Wine\\\\X11 Driver] 1793914892\n#time=1dd6\n\"KeyboardLayoutList\"=str(7):\"x\"\n\n[Software\\\\Zzz] 1\n\"C\"=\"d\"\n";

    #[test]
    fn reads_writes_and_round_trips_exactly() {
        assert_eq!(read_values(REG), GrabValues::default());
        let off = write_values(REG, &GrabValues::disabled(), 1);
        assert!(read_values(&off).capture_disabled());
        assert!(off.contains(
            "\"KeyboardLayoutList\"=str(7):\"x\"\n\"GrabPointer\"=\"N\"\n\"GrabFullscreen\"=\"N\"\n\n[Software\\\\Zzz]"
        ));
        assert_eq!(write_values(&off, &GrabValues::default(), 1), REG);
        let mixed = GrabValues { grab_pointer: Some("Y".into()), grab_fullscreen: None };
        assert_eq!(read_values(&write_values(REG, &mixed, 1)), mixed);
    }

    #[test]
    fn adds_the_section_when_missing() {
        let reg = "WINE REGISTRY Version 2\n\n[Other] 1\n\"A\"=\"b\"\n";
        let out = write_values(reg, &GrabValues::disabled(), 255);
        assert!(out.contains("[Software\\\\Wine\\\\X11 Driver] 255\n#time=ff\n\"GrabPointer\"=\"N\""));
        assert!(read_values(&out).capture_disabled());
    }

    #[test]
    fn parses_steam_library_folders() {
        let vdf =
            "\"libraryfolders\"\n{\n\t\"0\"\n\t{\n\t\t\"path\"\t\t\"/run/media/u/Game Disk/SteamLibrary\"\n\t}\n}\n";
        let libs = library_folders(vdf, Path::new("/home/u/.local/share/Steam"));
        assert_eq!(
            libs,
            vec![PathBuf::from("/home/u/.local/share/Steam"), PathBuf::from("/run/media/u/Game Disk/SteamLibrary")]
        );
    }

    fn fake_home() -> (tempfile::TempDir, PathBuf) {
        let home = tempfile::tempdir().unwrap();
        let lib = home.path().join("lib");
        let steam = home.path().join(".local/share/Steam/steamapps");
        std::fs::create_dir_all(&steam).unwrap();
        std::fs::write(steam.join("libraryfolders.vdf"), format!("\"path\" \"{}\"\n", lib.display())).unwrap();
        let pfx = lib.join("steamapps/compatdata").join(POE2_APPID).join("pfx");
        std::fs::create_dir_all(&pfx).unwrap();
        std::fs::write(pfx.join("user.reg"), REG).unwrap();
        (home, pfx)
    }

    #[test]
    fn disable_then_restore_leaves_the_prefix_unchanged() {
        let (home, pfx) = fake_home();
        assert_eq!(find_prefix(home.path()).as_deref(), Some(pfx.as_path()));
        let s = status_of(Some(&pfx), false);
        assert!(s.supported && !s.capture_disabled && !s.can_restore);

        let s = apply(&pfx, true, false, 1).unwrap();
        assert!(s.capture_disabled && s.can_restore);
        assert_eq!(s.grab_pointer.as_deref(), Some("N"));
        // A second disable keeps the originally saved values.
        apply(&pfx, true, false, 1).unwrap();

        let s = apply(&pfx, false, false, 1).unwrap();
        assert!(!s.capture_disabled && !s.can_restore);
        assert_eq!(std::fs::read_to_string(pfx.join("user.reg")).unwrap(), REG);
        assert_eq!(std::fs::read_to_string(pfx.join("user.reg.kalandra-backup")).unwrap(), REG);
        assert!(apply(&pfx, false, false, 1).is_err(), "nothing left to restore");
    }

    #[test]
    fn refuses_while_the_game_runs_and_reports_missing_prefix() {
        let (_home, pfx) = fake_home();
        let err = apply(&pfx, true, true, 1).unwrap_err();
        assert_eq!(err.kind, "busy");
        assert!(!status_of(None, false).supported);
    }

    #[test]
    fn detects_wine_and_the_game_in_proc() {
        let proc_root = tempfile::tempdir().unwrap();
        let p = proc_root.path().join("1");
        std::fs::create_dir_all(&p).unwrap();
        std::fs::write(p.join("comm"), "bash\n").unwrap();
        std::fs::write(p.join("cmdline"), "bash\0-c\0compatdata/2694490/pfx\0").unwrap();
        assert!(!game_running(proc_root.path()));
        std::fs::write(p.join("cmdline"), "C:\\Games\\PathOfExileSteam.exe\0--nopatch\0").unwrap();
        assert!(game_running(proc_root.path()));
        std::fs::write(p.join("cmdline"), "x\0").unwrap();
        std::fs::write(p.join("comm"), "wineserver\n").unwrap();
        assert!(game_running(proc_root.path()));
        assert!(!game_running(Path::new("/nonexistent-proc")));
    }
}
