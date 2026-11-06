//! Start RKL at login: an XDG autostart entry on Linux, the `HKCU\...\Run` key on Windows and a
//! LaunchAgent on macOS. Linux uses `$APPIMAGE` when set, because an AppImage's executable path
//! is a temporary mount that changes on every launch.
use std::path::{Path, PathBuf};

/// Entry name (file stem / registry value / launch agent label).
pub const NAME: &str = "rkl";
pub const LABEL: &str = "app.rkl";

/// The program to start at login.
pub fn launch_path() -> std::io::Result<PathBuf> {
    if let Some(appimage) = std::env::var_os("APPIMAGE").filter(|v| !v.is_empty()) {
        return Ok(PathBuf::from(appimage));
    }
    std::env::current_exe()
}

/// Quote `path` for a desktop entry `Exec` key (spec: double quotes; escape `"`, `` ` ``, `$`, `\`).
pub fn desktop_exec(path: &Path) -> String {
    let mut out = String::from("\"");
    for c in path.to_string_lossy().chars() {
        if matches!(c, '"' | '`' | '$' | '\\') {
            out.push('\\');
        }
        out.push(c);
    }
    out.push('"');
    out
}

/// XDG autostart desktop entry for `path`.
pub fn desktop_entry(path: &Path) -> String {
    format!(
        "[Desktop Entry]\nType=Application\nName=Rust King Ledger\nComment=Path of Exile 2 price-check overlay\nExec={}\nTerminal=false\nX-GNOME-Autostart-enabled=true\n",
        desktop_exec(path)
    )
}

fn xml_escape(s: &str) -> String {
    s.replace('&', "&amp;").replace('<', "&lt;").replace('>', "&gt;").replace('"', "&quot;")
}

/// macOS LaunchAgent plist starting `path` at login.
pub fn launch_agent(path: &Path) -> String {
    format!(
        "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<!DOCTYPE plist PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" \"http://www.apple.com/DTDs/PropertyList-1.0.dtd\">\n<plist version=\"1.0\">\n<dict>\n  <key>Label</key><string>{LABEL}</string>\n  <key>ProgramArguments</key><array><string>{}</string></array>\n  <key>RunAtLoad</key><true/>\n</dict>\n</plist>\n",
        xml_escape(&path.to_string_lossy())
    )
}

/// Where the Linux autostart entry lives (`$XDG_CONFIG_HOME/autostart`, else `~/.config/autostart`).
pub fn linux_entry_path(xdg_config_home: Option<&str>, home: Option<&str>) -> Option<PathBuf> {
    let base = xdg_config_home
        .filter(|v| !v.is_empty())
        .map(PathBuf::from)
        .or_else(|| home.filter(|v| !v.is_empty()).map(|h| Path::new(h).join(".config")))?;
    Some(base.join("autostart").join(format!("{NAME}.desktop")))
}

fn write_file(path: &Path, contents: &str) -> std::io::Result<()> {
    if let Some(dir) = path.parent() {
        std::fs::create_dir_all(dir)?;
    }
    std::fs::write(path, contents)
}

fn remove_file(path: &Path) -> std::io::Result<()> {
    match std::fs::remove_file(path) {
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => Ok(()),
        other => other,
    }
}

/// Turn start-at-login on (refreshing the path) or off.
pub fn set_enabled(enabled: bool) -> std::io::Result<()> {
    let path = launch_path()?;
    #[cfg(target_os = "linux")]
    {
        let env = |k: &str| std::env::var(k).ok();
        let entry = linux_entry_path(env("XDG_CONFIG_HOME").as_deref(), env("HOME").as_deref())
            .ok_or_else(|| std::io::Error::other("no config directory for the autostart entry"))?;
        if enabled { write_file(&entry, &desktop_entry(&path)) } else { remove_file(&entry) }
    }
    #[cfg(target_os = "macos")]
    {
        let home = std::env::var("HOME").map_err(std::io::Error::other)?;
        let agent = Path::new(&home).join("Library/LaunchAgents").join(format!("{LABEL}.plist"));
        if enabled { write_file(&agent, &launch_agent(&path)) } else { remove_file(&agent) }
    }
    #[cfg(target_os = "windows")]
    {
        let key = r"HKCU\Software\Microsoft\Windows\CurrentVersion\Run";
        let mut cmd = std::process::Command::new("reg");
        if enabled {
            cmd.args(["add", key, "/v", NAME, "/t", "REG_SZ", "/d"]).arg(format!("\"{}\"", path.display())).arg("/f");
        } else {
            cmd.args(["delete", key, "/v", NAME, "/f"]);
        }
        let status = cmd.status()?;
        // `reg delete` of a missing value fails; that is "already off".
        if status.success() || !enabled {
            Ok(())
        } else {
            Err(std::io::Error::other(format!("reg exited with {status}")))
        }
    }
    #[cfg(not(any(target_os = "linux", target_os = "macos", target_os = "windows")))]
    {
        let _ = (
            enabled,
            path,
            write_file as fn(&Path, &str) -> std::io::Result<()>,
            remove_file as fn(&Path) -> std::io::Result<()>,
        );
        Err(std::io::Error::other("start at login is not supported on this platform"))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn desktop_entry_quotes_the_path() {
        let e = desktop_entry(Path::new("/home/u/Apps/Rust King Ledger $1.AppImage"));
        assert!(e.contains("Exec=\"/home/u/Apps/Rust King Ledger \\$1.AppImage\"\n"), "{e}");
        assert!(e.starts_with("[Desktop Entry]\nType=Application\n"));
        assert_eq!(desktop_exec(Path::new(r#"/a"b\c`d"#)), r#""/a\"b\\c\`d""#);
    }

    #[test]
    fn launch_agent_escapes_xml() {
        let p = launch_agent(Path::new("/Applications/R&K <L>.app/Contents/MacOS/rkl"));
        assert!(p.contains("<string>/Applications/R&amp;K &lt;L&gt;.app/Contents/MacOS/rkl</string>"), "{p}");
        assert!(p.contains("<key>RunAtLoad</key><true/>"));
    }

    #[test]
    fn entry_path_prefers_xdg_config_home() {
        assert_eq!(linux_entry_path(Some("/x"), Some("/home/u")), Some(PathBuf::from("/x/autostart/rkl.desktop")));
        assert_eq!(
            linux_entry_path(None, Some("/home/u")),
            Some(PathBuf::from("/home/u/.config/autostart/rkl.desktop"))
        );
        assert_eq!(linux_entry_path(Some(""), None), None);
    }

    #[test]
    fn write_and_remove_are_idempotent() {
        let dir = tempfile::tempdir().unwrap();
        let f = dir.path().join("a/b.desktop");
        write_file(&f, "x").unwrap();
        assert_eq!(std::fs::read_to_string(&f).unwrap(), "x");
        remove_file(&f).unwrap();
        remove_file(&f).unwrap();
    }
}
