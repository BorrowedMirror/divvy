//! Wayland-native backend end-to-end under headless sway. Self-skipping when sway is missing.
//!
//! Like `linux_x11.rs`, the test re-executes itself as a child with a private
//! `XDG_RUNTIME_DIR`/`WAYLAND_DISPLAY`, no `DISPLAY`, and an unreachable session bus (so the
//! GlobalShortcuts portal is deterministically absent and the SIGUSR2 fallback is exercised).
#![cfg(all(target_os = "linux", feature = "native"))]

use std::os::unix::fs::PermissionsExt;
use std::path::{Path, PathBuf};
use std::process::{Child, Command, Stdio};
use std::time::{Duration, Instant};

use kalandra_platform::backend::{self, wayland};
use kalandra_platform::{BackendKind, Chord, EnvProbe, HotkeyAction, PlatformError, detect};

const CHILD_ENV: &str = "KALANDRA_TEST_WAYLAND_CHILD";

fn have(bin: &str) -> bool {
    std::env::var_os("PATH").is_some_and(|p| std::env::split_paths(&p).any(|d| d.join(bin).is_file()))
}

struct Sway {
    child: Child,
    runtime: PathBuf,
}

impl Drop for Sway {
    fn drop(&mut self) {
        let _ = self.child.kill();
        let _ = self.child.wait();
        let _ = std::fs::remove_dir_all(&self.runtime);
    }
}

fn wayland_socket(dir: &Path) -> Option<String> {
    std::fs::read_dir(dir).ok()?.filter_map(Result::ok).find_map(|e| {
        let name = e.file_name().into_string().ok()?;
        (name.starts_with("wayland-") && !name.ends_with(".lock")).then_some(name)
    })
}

#[test]
fn wayland_backend_end_to_end() {
    if std::env::var_os(CHILD_ENV).is_some() {
        child();
        return;
    }
    if !have("sway") {
        eprintln!("SKIP linux_wayland: sway not installed");
        return;
    }
    let runtime = std::env::temp_dir().join(format!("kalandra-wl-{}", std::process::id()));
    std::fs::create_dir_all(&runtime).unwrap();
    std::fs::set_permissions(&runtime, std::fs::Permissions::from_mode(0o700)).unwrap();
    let config = runtime.join("sway.conf");
    std::fs::write(&config, "xwayland disable\n").unwrap();
    let child = Command::new("sway")
        .arg("-c")
        .arg(&config)
        .env("WLR_BACKENDS", "headless")
        .env("WLR_LIBINPUT_NO_DEVICES", "1")
        .env("WLR_RENDERER", "pixman")
        .env("XDG_RUNTIME_DIR", &runtime)
        .env_remove("DISPLAY")
        .env_remove("WAYLAND_DISPLAY")
        .env_remove("SWAYSOCK")
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .spawn()
        .expect("spawn sway");
    let sway = Sway { child, runtime: runtime.clone() };

    let start = Instant::now();
    let socket = loop {
        if let Some(s) = wayland_socket(&runtime) {
            break s;
        }
        if start.elapsed() > Duration::from_secs(15) {
            eprintln!("SKIP linux_wayland: headless sway did not start");
            return;
        }
        std::thread::sleep(Duration::from_millis(50));
    };
    // The socket appears before sway finishes initialising its globals.
    std::thread::sleep(Duration::from_millis(500));

    let status = Command::new(std::env::current_exe().unwrap())
        .args(["--exact", "wayland_backend_end_to_end", "--nocapture", "--test-threads=1"])
        .env(CHILD_ENV, "1")
        .env("XDG_RUNTIME_DIR", &runtime)
        .env("WAYLAND_DISPLAY", &socket)
        .env("XDG_SESSION_TYPE", "wayland")
        .env("XDG_CURRENT_DESKTOP", "sway")
        .env("DBUS_SESSION_BUS_ADDRESS", "unix:path=/nonexistent/kalandra-test-bus")
        .env_remove("DISPLAY")
        .env_remove("KALANDRA_BACKEND")
        .status()
        .expect("run child");
    drop(sway);
    assert!(status.success(), "wayland child test failed: {status}");
}

fn child() {
    // No DISPLAY -> Wayland-native.
    assert_eq!(detect(&EnvProbe::from_env(), None), BackendKind::Wayland);

    let platform = backend::create(BackendKind::Wayland).expect("wayland backend");
    assert_eq!(platform.kind, BackendKind::Wayland);
    let caps = &platform.capabilities;
    assert!(!caps.window_tracking && !caps.passive_hook, "{caps:?}");
    assert!(!caps.global_hotkeys, "no portal on a dead bus: {caps:?}");
    assert!(caps.overlay_always_on_top && caps.overlay_click_through, "{caps:?}");
    assert!(caps.clipboard, "sway implements wlr-data-control: {caps:?}");
    // Headless sway has no keyboard device, so its seat advertises `no_keymap`; enigo 0.6 cannot
    // build a virtual keyboard from that. With a real keyboard (any desktop session) it works.
    // Either injection is available, or the reason is reported on the diagnostics page.
    assert!(
        caps.keystroke_injection || caps.notes.iter().any(|n| n.contains("virtual-keyboard")),
        "keystroke capability must be explained: {caps:?}"
    );
    assert!(caps.notes.iter().any(|n| n.contains("anchored to the output")), "{caps:?}");
    assert!(caps.notes.iter().any(|n| n.contains("--trigger")), "{caps:?}");

    // Unsupported pieces report themselves.
    assert!(platform.window_tracker.game_window().is_none());
    assert!(platform.window_tracker.is_game_focused());
    assert!(matches!(platform.window_tracker.subscribe(), Err(PlatformError::Unsupported(_))));
    assert!(matches!(platform.input_listener.start(), Err(PlatformError::Unsupported(_))));

    // Clipboard roundtrip through wlr-data-control.
    platform.clipboard.set_text("kalandra wayland clipboard").unwrap();
    let deadline = Instant::now() + Duration::from_secs(5);
    loop {
        let got = platform.clipboard.get_text().unwrap();
        if got.as_deref() == Some("kalandra wayland clipboard") {
            break;
        }
        assert!(Instant::now() < deadline, "clipboard never round-tripped: {got:?}");
        std::thread::sleep(Duration::from_millis(50));
    }
    platform.clipboard.clear().unwrap();

    // Keystrokes through the virtual keyboard. enigo is created lazily, so when it failed at
    // startup the capability is still "on demand" and each send retries (and fails here).
    let retrying = caps.notes.iter().any(|n| n.contains("retried on each copy"));
    let sent = platform.input_simulator.send_chord(&Chord::parse("Ctrl+C").unwrap());
    if caps.keystroke_injection && !retrying {
        sent.unwrap();
        assert!(matches!(
            platform.input_simulator.send_chord(&Chord::new(&[], "NoSuchKey")),
            Err(PlatformError::InvalidChord(_))
        ));
    } else if caps.keystroke_injection {
        eprintln!("NOTE linux_wayland: virtual keyboard not ready; send retried: {sent:?}");
        let again = platform.input_simulator.send_chord(&Chord::parse("Ctrl+C").unwrap());
        assert!(again.is_ok() || matches!(again, Err(PlatformError::Backend(_))), "{again:?}");
    } else {
        eprintln!("NOTE linux_wayland: no virtual keyboard in this headless sway; injection not exercised");
        assert!(matches!(sent, Err(PlatformError::Unsupported("keystroke_injection"))), "{sent:?}");
    }

    // Hotkeys: registrations are accepted; SIGUSR2 fires PriceCheck.
    let chord = Chord::parse("Ctrl+Alt+D").unwrap();
    platform.hotkeys.register(HotkeyAction::PriceCheck, &chord).unwrap();
    let events = platform.hotkeys.events().unwrap();
    assert!(matches!(platform.hotkeys.events(), Err(PlatformError::AlreadyStarted)));
    let direct = wayland::sigusr2_trigger().unwrap();
    signal_hook_raise_sigusr2();
    assert_eq!(events.recv_timeout(Duration::from_secs(5)).unwrap().action, HotkeyAction::PriceCheck);
    assert_eq!(direct.recv_timeout(Duration::from_secs(5)).unwrap().action, HotkeyAction::PriceCheck);
    platform.hotkeys.unregister(HotkeyAction::PriceCheck).unwrap();
    platform.hotkeys.unregister_all().unwrap();
}

fn signal_hook_raise_sigusr2() {
    let status = Command::new("kill").args(["-USR2", &std::process::id().to_string()]).status().unwrap();
    assert!(status.success());
}
