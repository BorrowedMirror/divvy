//! Minimal CLI: `kalandra [--trigger [--pinned]]`. `--trigger` asks a running instance to
//! price-check (Wayland fallback when global shortcuts are unavailable) via SIGUSR2.
use std::path::{Path, PathBuf};

#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub struct Cli {
    pub trigger: bool,
    pub pinned: bool,
}

pub fn parse(args: impl IntoIterator<Item = String>) -> Cli {
    let mut cli = Cli::default();
    for a in args {
        match a.as_str() {
            "--trigger" => cli.trigger = true,
            "--pinned" => cli.pinned = true,
            _ => {}
        }
    }
    cli
}

pub fn pid_file(cache_dir: &Path) -> PathBuf {
    cache_dir.join("kalandra.pid")
}

pub fn write_pid(cache_dir: &Path) -> std::io::Result<()> {
    std::fs::create_dir_all(cache_dir)?;
    std::fs::write(pid_file(cache_dir), std::process::id().to_string())
}

/// Signal the running instance. Returns a user-facing error message on failure.
pub fn send_trigger(cache_dir: &Path, pinned: bool) -> Result<(), String> {
    let pid = std::fs::read_to_string(pid_file(cache_dir)).map_err(|e| format!("kalandra is not running ({e})"))?;
    let pid = pid.trim();
    if pid.parse::<u32>().is_err() {
        return Err(format!("corrupt pid file: `{pid}`"));
    }
    let signal = if pinned { "-USR1" } else { "-USR2" };
    let status = std::process::Command::new("kill").args([signal, pid]).status().map_err(|e| e.to_string())?;
    if status.success() { Ok(()) } else { Err(format!("kill {signal} {pid} failed")) }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parses_flags() {
        assert_eq!(parse(["kalandra".into()]), Cli::default());
        assert_eq!(parse(["--trigger".into(), "--pinned".into()]), Cli { trigger: true, pinned: true });
    }

    #[test]
    fn pid_roundtrip_and_errors() {
        let dir = std::env::temp_dir().join(format!("kalandra-cli-{}", std::process::id()));
        write_pid(&dir).unwrap();
        assert_eq!(std::fs::read_to_string(pid_file(&dir)).unwrap(), std::process::id().to_string());
        std::fs::write(pid_file(&dir), "nope").unwrap();
        assert!(send_trigger(&dir, false).unwrap_err().contains("corrupt"));
        std::fs::remove_dir_all(&dir).unwrap();
        assert!(send_trigger(&dir, false).unwrap_err().contains("not running"));
    }
}
