//! End-to-end check of the macOS backend against `tools/fake-game`: the tracker must find the
//! window titled `Path of Exile 2`, and the simulated copy chord must put the item on the
//! clipboard. Skips (with a message) when the TCC permissions it needs are missing, since CI
//! runners cannot grant them non-interactively.
#![cfg(all(target_os = "macos", feature = "native"))]

use std::io::{BufRead, BufReader};
use std::path::PathBuf;
use std::process::{Child, Command, Stdio};
use std::sync::mpsc::{Receiver, channel};
use std::time::{Duration, Instant};

use kalandra_platform::backend::macos::{self, CopyModifier, MacosOptions};
use kalandra_platform::{Chord, GAME_WINDOW_TITLE, KeyModifier, WindowEvent};

fn workspace_root() -> PathBuf {
    PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("../..")
}

fn fake_game_binary() -> PathBuf {
    let target = std::env::var_os("CARGO_TARGET_DIR").map_or_else(|| workspace_root().join("target"), PathBuf::from);
    let bin = target.join("debug/fake-game");
    if !bin.exists() {
        let status = Command::new(env!("CARGO"))
            .args(["build", "-p", "fake-game"])
            .current_dir(workspace_root())
            .status()
            .expect("run cargo build -p fake-game");
        assert!(status.success(), "cargo build -p fake-game failed");
    }
    bin
}

struct Game {
    child: Child,
    lines: Receiver<String>,
}

impl Drop for Game {
    fn drop(&mut self) {
        let _ = self.child.kill();
        let _ = self.child.wait();
    }
}

impl Game {
    fn spawn(item: &std::path::Path) -> Self {
        let mut child = Command::new(fake_game_binary())
            .arg("--item")
            .arg(item)
            .stdout(Stdio::piped())
            .spawn()
            .expect("spawn fake-game");
        let stdout = child.stdout.take().expect("stdout");
        let (tx, lines) = channel();
        std::thread::spawn(move || {
            for line in BufReader::new(stdout).lines().map_while(Result::ok) {
                if tx.send(line).is_err() {
                    break;
                }
            }
        });
        Self { child, lines }
    }

    fn wait_for(&self, prefix: &str, timeout: Duration) -> bool {
        let deadline = Instant::now() + timeout;
        while let Some(left) = deadline.checked_duration_since(Instant::now()) {
            match self.lines.recv_timeout(left) {
                Ok(line) if line.starts_with(prefix) => return true,
                Ok(_) => {}
                Err(_) => return false,
            }
        }
        false
    }
}

#[test]
fn tracks_fake_game_and_copies_item() {
    let missing = macos::recheck_permissions();
    // fake-game's process is not named after the game, so finding it needs titles (Screen
    // Recording); sending the copy chord needs Accessibility.
    if missing.iter().any(|p| p == "accessibility" || p == "screen_recording") {
        eprintln!("SKIP macos_fake_game: missing permissions {missing:?} (grant them to the terminal running cargo)");
        return;
    }

    let item = workspace_root().join("crates/testkit/corpus/currency/divine-orb.txt");
    let expected = std::fs::read_to_string(&item).expect("corpus item");
    let game = Game::spawn(&item);
    assert!(game.wait_for("READY", Duration::from_secs(20)), "fake-game never printed READY");

    // fake-game implements the portable Ctrl+C contract, so do not remap to Cmd.
    let platform = macos::create_with(MacosOptions { copy_modifier: CopyModifier::Control, ..MacosOptions::default() })
        .expect("macos backend");
    assert!(platform.capabilities.keystroke_injection);

    // Tracking: subscription reports Found, direct query agrees.
    let events = platform.window_tracker.subscribe().expect("subscribe");
    let found = loop {
        if let WindowEvent::Found(info) =
            events.recv_timeout(Duration::from_secs(5)).expect("no window event within 5s")
        {
            break info;
        }
    };
    assert_eq!(found.title, GAME_WINDOW_TITLE);
    assert!(found.rect.width > 0 && found.rect.height > 0, "{found:?}");
    let direct = platform.window_tracker.game_window().expect("game_window");
    assert_eq!(direct.native_id, found.native_id);

    // Copy: the fake game must be frontmost to receive the synthetic keystroke.
    let deadline = Instant::now() + Duration::from_secs(5);
    while !platform.window_tracker.is_game_focused() {
        if Instant::now() > deadline {
            eprintln!("SKIP copy half of macos_fake_game: fake-game did not become frontmost");
            return;
        }
        std::thread::sleep(Duration::from_millis(100));
    }
    platform.clipboard.clear().expect("clear clipboard");
    platform.input_simulator.send_chord(&Chord::new(&[KeyModifier::Ctrl], "KeyC")).expect("send Ctrl+C");
    assert!(game.wait_for("COPIED", Duration::from_secs(5)), "fake-game never reported COPIED");
    let deadline = Instant::now() + Duration::from_secs(2);
    let text = loop {
        if let Some(text) = platform.clipboard.get_text().expect("read clipboard").filter(|t| !t.is_empty()) {
            break text;
        }
        assert!(Instant::now() < deadline, "clipboard stayed empty");
        std::thread::sleep(Duration::from_millis(20));
    };
    assert_eq!(text.replace("\r\n", "\n").trim_end(), expected.replace("\r\n", "\n").trim_end());
}
