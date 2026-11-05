//! End-to-end check of the Windows backend against `tools/fake-game`: the tracker finds the window
//! and the Ctrl+C copy flow returns the item text. Needs an interactive desktop session (SendInput
//! and foreground windows), so it only runs on Windows with the `native` feature.
//!
//! The fake-game binary lives in another package, so `CARGO_BIN_EXE_*` is unavailable: we look for
//! `<target>/debug/fake-game.exe` and build it with `cargo build -p fake-game` if missing. If it
//! still cannot be found the test is skipped with a message.
#![cfg(all(windows, feature = "native"))]

use std::io::{BufRead, BufReader};
use std::path::PathBuf;
use std::process::{Child, Command, Stdio};
use std::sync::mpsc;
use std::time::{Duration, Instant};

use kalandra_platform::copy::{CopyOptions, copy_item_text};
use kalandra_platform::{BackendKind, GAME_WINDOW_TITLE, WindowEvent, backend};

const ITEM: &str = include_str!("../../testkit/corpus/currency/divine-orb.txt");

fn target_dir() -> PathBuf {
    std::env::var_os("CARGO_TARGET_DIR")
        .map(PathBuf::from)
        .unwrap_or_else(|| PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("../../target"))
}

fn fake_game_exe() -> Option<PathBuf> {
    let exe = target_dir().join("debug").join("fake-game.exe");
    if exe.is_file() {
        return Some(exe);
    }
    let cargo = std::env::var_os("CARGO").unwrap_or_else(|| "cargo".into());
    let status = Command::new(cargo)
        .args(["build", "-p", "fake-game"])
        .current_dir(concat!(env!("CARGO_MANIFEST_DIR"), "/../.."))
        .status();
    match status {
        Ok(s) if s.success() && exe.is_file() => Some(exe),
        other => {
            eprintln!("SKIP: fake-game not available at {} (build: {other:?})", exe.display());
            None
        }
    }
}

/// Kills the fake game even when an assertion fails.
struct Game(Child);

impl Drop for Game {
    fn drop(&mut self) {
        let _ = self.0.kill();
        let _ = self.0.wait();
    }
}

fn wait_until<T>(timeout: Duration, mut f: impl FnMut() -> Option<T>) -> Option<T> {
    let deadline = Instant::now() + timeout;
    loop {
        if let Some(v) = f() {
            return Some(v);
        }
        if Instant::now() >= deadline {
            return None;
        }
        std::thread::sleep(Duration::from_millis(50));
    }
}

#[test]
fn tracks_fake_game_and_copies_item() {
    let Some(exe) = fake_game_exe() else { return };
    let item_path = std::env::temp_dir().join(format!("kalandra-fake-item-{}.txt", std::process::id()));
    std::fs::write(&item_path, ITEM).unwrap();

    let mut child = Command::new(&exe)
        .arg("--item")
        .arg(&item_path)
        .stdout(Stdio::piped())
        .stderr(Stdio::inherit())
        .spawn()
        .expect("spawn fake-game");
    let stdout = child.stdout.take().unwrap();
    let game = Game(child);

    let (line_tx, line_rx) = mpsc::channel();
    std::thread::spawn(move || {
        for line in BufReader::new(stdout).lines().map_while(Result::ok) {
            if line_tx.send(line).is_err() {
                break;
            }
        }
    });
    let ready = line_rx.recv_timeout(Duration::from_secs(30)).expect("fake-game printed nothing");
    assert_eq!(ready.trim(), "READY");

    let platform = backend::create(BackendKind::Windows).expect("windows backend");
    assert!(platform.capabilities.window_tracking);

    let info = wait_until(Duration::from_secs(5), || platform.window_tracker.game_window())
        .expect("tracker did not find the fake game window");
    assert_eq!(info.title, GAME_WINDOW_TITLE);
    assert!(info.rect.width > 0 && info.rect.height > 0, "{info:?}");

    let events = platform.window_tracker.subscribe().expect("subscribe");
    match events.recv_timeout(Duration::from_secs(3)).expect("no window event") {
        WindowEvent::Found(found) => assert_eq!(found.native_id, info.native_id),
        other => panic!("expected Found, got {other:?}"),
    }

    // A window created by a process we just launched normally receives the foreground.
    assert!(
        wait_until(Duration::from_secs(5), || platform.window_tracker.is_game_focused().then_some(())).is_some(),
        "fake game never became the foreground window; run this test from an interactive desktop session"
    );

    let opts = CopyOptions { advanced: false, timeout: Duration::from_secs(3), ..CopyOptions::default() };
    let text =
        copy_item_text(&*platform.input_simulator, &*platform.clipboard, opts, &std::thread::sleep).expect("copy flow");
    assert_eq!(text.replace("\r\n", "\n").trim_end(), ITEM.replace("\r\n", "\n").trim_end());

    let copied = wait_until(Duration::from_secs(2), || line_rx.try_recv().ok().filter(|l| l.starts_with("COPIED")));
    assert!(copied.is_some(), "fake-game did not report COPIED");

    drop(game);
    let _ = std::fs::remove_file(item_path);
}
