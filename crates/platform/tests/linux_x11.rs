//! X11 backend end-to-end under Xvfb + openbox. Self-skipping when either binary is missing.
//!
//! The test re-executes its own binary as a child with `DISPLAY` pointing at the private Xvfb:
//! the backend reads the environment (which we must not mutate in a threaded test harness),
//! and the rdev hook (Xlib) calls `exit()` when its server goes away, so the servers may only
//! be torn down after the process that used them has exited.
#![cfg(all(target_os = "linux", feature = "native"))]

/// Set in the child process.
const CHILD_ENV: &str = "KALANDRA_TEST_X11_CHILD";

use std::path::Path;
use std::process::{Child, Command, Stdio};
use std::sync::mpsc::Receiver;
use std::time::{Duration, Instant};

use kalandra_platform::backend::{self, x11};
use kalandra_platform::copy::{CopyOptions, copy_item_text};
use kalandra_platform::{
    BackendKind, Chord, GAME_WINDOW_TITLE, HotkeyAction, InputEvent, PlatformError, Rect, WindowEvent,
};
use raw_window_handle::{RawDisplayHandle, RawWindowHandle, XcbDisplayHandle, XcbWindowHandle};
use x11rb::connection::Connection;
use x11rb::protocol::Event;
use x11rb::protocol::shape::{ConnectionExt as _, SK};
use x11rb::protocol::xproto::{
    AtomEnum, ConfigureWindowAux, ConnectionExt as _, CreateWindowAux, EventMask, InputFocus, ModMask, PropMode,
    Window, WindowClass,
};
use x11rb::rust_connection::RustConnection;
use x11rb::wrapper::ConnectionExt as _;

const ITEM: &str = "Item Class: Rings\nRarity: Rare\nGale Loop\nRuby Ring\n--------\n";

fn have(bin: &str) -> bool {
    std::env::var_os("PATH").is_some_and(|p| std::env::split_paths(&p).any(|d| d.join(bin).is_file()))
}

struct Procs(Vec<Child>);

impl Drop for Procs {
    fn drop(&mut self) {
        for c in self.0.iter_mut().rev() {
            let _ = c.kill();
            let _ = c.wait();
        }
    }
}

fn wait_until(what: &str, timeout: Duration, mut f: impl FnMut() -> bool) {
    let start = Instant::now();
    while !f() {
        assert!(start.elapsed() < timeout, "timed out waiting for {what}");
        std::thread::sleep(Duration::from_millis(20));
    }
}

fn next_event(rx: &Receiver<WindowEvent>, what: &str, pred: impl Fn(&WindowEvent) -> bool) -> WindowEvent {
    let deadline = Instant::now() + Duration::from_secs(10);
    loop {
        let left = deadline.saturating_duration_since(Instant::now());
        match rx.recv_timeout(left) {
            Ok(ev) if pred(&ev) => return ev,
            Ok(_) => {}
            Err(e) => panic!("no {what} event: {e}"),
        }
    }
}

fn create_window(conn: &RustConnection, root: Window, title: Option<&str>, x: i16, y: i16) -> Window {
    let win = conn.generate_id().unwrap();
    let aux = CreateWindowAux::new()
        .background_pixel(0x0020_3040)
        .event_mask(EventMask::KEY_PRESS | EventMask::STRUCTURE_NOTIFY | EventMask::FOCUS_CHANGE);
    conn.create_window(0, win, root, x, y, 640, 480, 0, WindowClass::INPUT_OUTPUT, 0, &aux).unwrap();
    if let Some(title) = title {
        let net_wm_name = conn.intern_atom(false, b"_NET_WM_NAME").unwrap().reply().unwrap().atom;
        let utf8 = conn.intern_atom(false, b"UTF8_STRING").unwrap().reply().unwrap().atom;
        conn.change_property8(PropMode::REPLACE, win, net_wm_name, utf8, title.as_bytes()).unwrap();
        conn.change_property8(PropMode::REPLACE, win, AtomEnum::WM_NAME, AtomEnum::STRING, title.as_bytes()).unwrap();
    }
    conn.flush().unwrap();
    win
}

#[test]
fn x11_backend_end_to_end() {
    if std::env::var_os(CHILD_ENV).is_some() {
        child();
        return;
    }
    if !have("Xvfb") || !have("openbox") {
        eprintln!("SKIP linux_x11: Xvfb and/or openbox not installed");
        return;
    }
    // Start from a pid-derived number so parallel CI jobs/worktrees do not race for `:99`.
    let base = 300 + std::process::id() % 2000;
    let n = (base..base + 200)
        .find(|n| {
            !Path::new(&format!("/tmp/.X11-unix/X{n}")).exists() && !Path::new(&format!("/tmp/.X{n}-lock")).exists()
        })
        .expect("free display");
    let display = format!(":{n}");
    let mut procs = Procs(Vec::new());
    procs.0.push(
        Command::new("Xvfb")
            .args([display.as_str(), "-screen", "0", "1280x800x24", "-nolisten", "tcp"])
            .stdout(Stdio::null())
            .stderr(Stdio::null())
            .spawn()
            .expect("spawn Xvfb"),
    );
    wait_until("Xvfb socket", Duration::from_secs(10), || Path::new(&format!("/tmp/.X11-unix/X{n}")).exists());
    wait_until("Xvfb accepting", Duration::from_secs(10), || x11rb::connect(Some(&display)).is_ok());

    procs.0.push(
        Command::new("openbox")
            .args(["--sm-disable"])
            .env("DISPLAY", &display)
            .stdout(Stdio::null())
            .stderr(Stdio::null())
            .spawn()
            .expect("spawn openbox"),
    );

    let (conn, screen) = x11rb::connect(Some(&display)).unwrap();
    let root = conn.setup().roots[screen].root;
    let check = conn.intern_atom(false, b"_NET_SUPPORTING_WM_CHECK").unwrap().reply().unwrap().atom;
    wait_until("openbox", Duration::from_secs(10), || {
        conn.get_property(false, root, check, AtomEnum::WINDOW, 0, 1)
            .ok()
            .and_then(|c| c.reply().ok())
            .is_some_and(|r| !r.value.is_empty())
    });
    // openbox publishes the check window slightly before it selects SubstructureRedirect;
    // a MapRequest sent in that gap is lost forever. Give it a moment.
    std::thread::sleep(Duration::from_secs(1));
    drop(conn);

    let status = Command::new(std::env::current_exe().unwrap())
        .args(["--exact", "x11_backend_end_to_end", "--nocapture", "--test-threads=1"])
        .env(CHILD_ENV, "1")
        .env("DISPLAY", &display)
        .env("XDG_CURRENT_DESKTOP", "openbox")
        .env_remove("WAYLAND_DISPLAY")
        .env_remove("KALANDRA_BACKEND")
        .status()
        .expect("run child");
    drop(procs);
    assert!(status.success(), "x11 child test failed: {status}");
}

#[allow(clippy::too_many_lines)]
fn child() {
    let display = std::env::var("DISPLAY").unwrap();
    let (conn, screen) = x11rb::connect(Some(&display)).unwrap();
    let root = conn.setup().roots[screen].root;

    // detect(): DISPLAY set -> X11.
    assert_eq!(kalandra_platform::detect(&kalandra_platform::EnvProbe::from_env(), None), BackendKind::X11);

    let platform = backend::create(BackendKind::X11).expect("x11 backend");
    assert_eq!(platform.kind, BackendKind::X11);
    let caps = &platform.capabilities;
    assert!(caps.window_tracking && caps.global_hotkeys && caps.overlay_always_on_top, "{caps:?}");
    assert!(caps.overlay_click_through && caps.keystroke_injection && caps.clipboard, "{caps:?}");
    assert!(format!("{platform:?}").contains("X11"));

    // No game yet.
    assert!(platform.window_tracker.game_window().is_none());
    assert!(!platform.window_tracker.is_game_focused());
    let events = platform.window_tracker.subscribe().unwrap();
    assert!(matches!(platform.window_tracker.subscribe(), Err(PlatformError::AlreadyStarted)));

    // A decoy window first, then our stand-in for the game.
    let decoy = create_window(&conn, root, Some("Not the game"), 0, 0);
    conn.map_window(decoy).unwrap();
    let game = create_window(&conn, root, Some(GAME_WINDOW_TITLE), 50, 60);
    conn.map_window(game).unwrap();
    conn.flush().unwrap();

    let WindowEvent::Found(found) = next_event(&events, "Found", |e| matches!(e, WindowEvent::Found(_))) else {
        unreachable!()
    };
    assert_eq!(found.title, GAME_WINDOW_TITLE);
    assert_eq!(found.native_id, u64::from(game));
    assert_eq!((found.rect.width, found.rect.height), (640, 480));

    // Move it; the tracker must report root coordinates.
    conn.configure_window(game, &ConfigureWindowAux::new().x(300).y(200)).unwrap();
    conn.flush().unwrap();
    let moved = next_event(&events, "Moved", |e| matches!(e, WindowEvent::Moved { rect } if rect.x != found.rect.x));
    let WindowEvent::Moved { rect } = moved else { unreachable!() };
    wait_until("game_window rect to settle", Duration::from_secs(5), || {
        platform.window_tracker.game_window().is_some_and(|w| w.rect.x >= 300 && w.rect.y >= 200)
    });
    assert!(rect.x >= 300 - 50 && rect.y >= 150, "{rect:?}");

    // Focus the game; the tracker follows _NET_ACTIVE_WINDOW / input focus.
    conn.set_input_focus(InputFocus::PARENT, game, x11rb::CURRENT_TIME).unwrap();
    conn.flush().unwrap();
    wait_until("game focused", Duration::from_secs(5), || platform.window_tracker.is_game_focused());

    // Clipboard roundtrip.
    platform.clipboard.set_text("kalandra x11 clipboard").unwrap();
    assert_eq!(platform.clipboard.get_text().unwrap().as_deref(), Some("kalandra x11 clipboard"));
    platform.clipboard.clear().unwrap();
    assert_eq!(platform.clipboard.get_text().unwrap(), None);

    // Passive hook (XRecord) + keystroke injection (XTEST).
    let input = platform.input_listener.start().expect("hook");
    assert!(matches!(platform.input_listener.start(), Err(PlatformError::AlreadyStarted)));
    assert!(std::env::var_os(backend::KEYBOARD_ONLY_ENV).is_some(), "rdev must hook the keyboard only");
    // start() does not wait for XRecord to be live, so resend until the hook sees the key.
    let deadline = Instant::now() + Duration::from_secs(5);
    'hooked: loop {
        assert!(Instant::now() < deadline, "hook never saw F5");
        platform.input_simulator.send_chord(&Chord::parse("Shift+F5").unwrap()).unwrap();
        while let Ok(ev) = input.recv_timeout(Duration::from_millis(100)) {
            if ev == (InputEvent::KeyDown { key: "F5".into() }) {
                break 'hooked;
            }
        }
    }

    // Our "game" sees the injected Ctrl+C and answers by putting the item on the clipboard.
    let game_clip = spawn_fake_game(&display, game);
    std::thread::sleep(Duration::from_millis(200));
    platform.clipboard.set_text("previous").unwrap();
    let opts = CopyOptions {
        advanced: false,
        timeout: Duration::from_secs(5),
        poll_interval: Duration::from_millis(20),
        restore_clipboard: true,
    };
    let text =
        copy_item_text(&*platform.input_simulator, &*platform.clipboard, opts, &std::thread::sleep).expect("copy flow");
    assert_eq!(text, ITEM);
    assert!(game_clip.join().unwrap(), "game never saw Ctrl+C");
    assert_eq!(platform.clipboard.get_text().unwrap().as_deref(), Some("previous"));

    // Global hotkey fired by an injected chord.
    let hotkeys = platform.hotkeys.events().unwrap();
    assert!(matches!(platform.hotkeys.events(), Err(PlatformError::AlreadyStarted)));
    let chord = Chord::parse("Ctrl+Alt+K").unwrap();
    platform.hotkeys.register(HotkeyAction::PriceCheck, &chord).unwrap();
    platform.hotkeys.register(HotkeyAction::PriceCheck, &chord).unwrap(); // rebinding is idempotent
    assert!(matches!(
        platform.hotkeys.register(HotkeyAction::OpenSettings, &Chord::new(&[], "NoSuchKey")),
        Err(PlatformError::InvalidChord(_))
    ));
    platform.input_simulator.send_chord(&chord).unwrap();
    assert_eq!(hotkeys.recv_timeout(Duration::from_secs(5)).expect("hotkey").action, HotkeyAction::PriceCheck);
    platform.hotkeys.unregister(HotkeyAction::PriceCheck).unwrap();
    platform.hotkeys.unregister(HotkeyAction::PriceCheck).unwrap();
    platform.hotkeys.register(HotkeyAction::PriceCheckPinned, &chord).unwrap();
    platform.hotkeys.unregister_all().unwrap();
    assert!(matches!(
        platform.input_simulator.send_chord(&Chord::new(&[], "NoSuchKey")),
        Err(PlatformError::InvalidChord(_))
    ));

    held_keys_are_respected(&conn, root, &platform);
    hotkey_held_during_copy(&display, &conn, root, game, &platform, &hotkeys);

    // Overlay styling over a raw Xcb handle.
    let ov_win = create_window(&conn, root, None, 0, 0);
    let wh = XcbWindowHandle::new(std::num::NonZeroU32::new(ov_win).unwrap());
    let dh = XcbDisplayHandle::new(None, 0);
    let overlay = x11::overlay(RawWindowHandle::Xcb(wh), RawDisplayHandle::Xcb(dh)).unwrap();
    overlay.prepare().unwrap();
    overlay.show_at(Rect { x: 10, y: 20, width: 300, height: 200 }).unwrap();
    let net_wm_state = conn.intern_atom(false, b"_NET_WM_STATE").unwrap().reply().unwrap().atom;
    let above = conn.intern_atom(false, b"_NET_WM_STATE_ABOVE").unwrap().reply().unwrap().atom;
    wait_until("_NET_WM_STATE_ABOVE", Duration::from_secs(5), || {
        conn.get_property(false, ov_win, net_wm_state, AtomEnum::ATOM, 0, 32)
            .ok()
            .and_then(|c| c.reply().ok())
            .and_then(|r| r.value32().map(|mut v| v.any(|a| a == above)))
            .unwrap_or(false)
    });
    let input_rects = |conn: &RustConnection| conn.shape_get_rectangles(ov_win, SK::INPUT).unwrap().reply().unwrap();
    overlay.set_click_through(true).unwrap();
    assert!(input_rects(&conn).rectangles.is_empty(), "click-through => empty input shape");
    overlay.set_click_through(false).unwrap();
    assert!(!input_rects(&conn).rectangles.is_empty(), "input shape restored");
    overlay.hide().unwrap();
    assert!(matches!(
        x11::overlay(
            RawWindowHandle::Wayland(raw_window_handle::WaylandWindowHandle::new(std::ptr::NonNull::dangling())),
            RawDisplayHandle::Xcb(dh)
        ),
        Err(PlatformError::Unsupported(_))
    ));

    // Destroying the game window -> Lost.
    conn.destroy_window(game).unwrap();
    conn.flush().unwrap();
    next_event(&events, "Lost", |e| matches!(e, WindowEvent::Lost));
    assert!(platform.window_tracker.game_window().is_none());
}

/// Stand-in for the game: on a Ctrl+<key> KeyPress it puts the item on the clipboard.
fn spawn_fake_game(display: &str, game: Window) -> std::thread::JoinHandle<bool> {
    let display = display.to_owned();
    std::thread::spawn(move || {
        let (gconn, _) = x11rb::connect(Some(&display)).unwrap();
        gconn
            .change_window_attributes(
                game,
                &x11rb::protocol::xproto::ChangeWindowAttributesAux::new().event_mask(EventMask::KEY_PRESS),
            )
            .unwrap();
        gconn.flush().unwrap();
        let mut clip = arboard::Clipboard::new().unwrap();
        let deadline = Instant::now() + Duration::from_secs(10);
        while Instant::now() < deadline {
            match gconn.poll_for_event().unwrap() {
                Some(Event::KeyPress(k)) if u16::from(k.state) & u16::from(ModMask::CONTROL) != 0 => {
                    clip.set_text(ITEM).unwrap();
                    // Keep serving the selection until the reader is done.
                    std::thread::sleep(Duration::from_secs(1));
                    return true;
                }
                Some(_) => {}
                None => std::thread::sleep(Duration::from_millis(5)),
            }
        }
        false
    })
}

// Keycodes of the Xvfb default (evdev) keymap.
const KC_CTRL_L: u8 = 37;
const KC_SHIFT_L: u8 = 50;
const KC_A: u8 = 38;

fn fake_key(conn: &RustConnection, root: Window, keycode: u8, down: bool) {
    use x11rb::protocol::xproto::{KEY_PRESS_EVENT, KEY_RELEASE_EVENT};
    use x11rb::protocol::xtest::ConnectionExt as _;
    let ty = if down { KEY_PRESS_EVENT } else { KEY_RELEASE_EVENT };
    conn.xtest_fake_input(ty, keycode, x11rb::CURRENT_TIME, root, 0, 0, 0).unwrap();
    conn.sync().unwrap();
}

fn key_down(conn: &RustConnection, keycode: u8) -> bool {
    let keys = conn.query_keymap().unwrap().reply().unwrap().keys;
    keys[usize::from(keycode / 8)] & (1 << (keycode % 8)) != 0
}

/// Held chord modifiers stay down; extra held modifiers are released; a held non-modifier key
/// (the hotkey's) delays the chord until it is released.
fn held_keys_are_respected(conn: &RustConnection, root: Window, platform: &kalandra_platform::Platform) {
    let ctrl_f5 = Chord::parse("Ctrl+F5").unwrap();
    fake_key(conn, root, KC_CTRL_L, true);
    fake_key(conn, root, KC_SHIFT_L, true);
    platform.input_simulator.send_chord(&ctrl_f5).unwrap();
    assert!(key_down(conn, KC_CTRL_L), "held Ctrl must not be synthetically released");
    assert!(!key_down(conn, KC_SHIFT_L), "extra held Shift must be released first");
    fake_key(conn, root, KC_CTRL_L, false);
    assert!(!key_down(conn, KC_CTRL_L));

    // Hotkey key held: send_chord waits for its release.
    fake_key(conn, root, KC_A, true);
    let releaser = std::thread::spawn({
        let display = std::env::var("DISPLAY").unwrap();
        move || {
            let (c, s) = x11rb::connect(Some(&display)).unwrap();
            let r = c.setup().roots[s].root;
            std::thread::sleep(Duration::from_millis(150));
            fake_key(&c, r, KC_A, false);
        }
    });
    let start = Instant::now();
    platform.input_simulator.send_chord(&ctrl_f5).unwrap();
    let waited = start.elapsed();
    releaser.join().unwrap();
    assert!(waited >= Duration::from_millis(120), "did not wait for release: {waited:?}");
    assert!(waited < Duration::from_secs(2), "{waited:?}");

    // Never released: capped (~400ms), then sent anyway.
    fake_key(conn, root, KC_A, true);
    let start = Instant::now();
    platform.input_simulator.send_chord(&ctrl_f5).unwrap();
    let waited = start.elapsed();
    fake_key(conn, root, KC_A, false);
    assert!(waited >= Duration::from_millis(350) && waited < Duration::from_secs(2), "{waited:?}");
}

/// The real flow: the user presses a global hotkey (XGrabKey) and still holds it while the copy
/// chord is sent. The game must still receive the chord (not the grabbing client).
fn hotkey_held_during_copy(
    display: &str,
    conn: &RustConnection,
    root: Window,
    game: Window,
    platform: &kalandra_platform::Platform,
    hotkeys: &Receiver<kalandra_platform::HotkeyEvent>,
) {
    // Ctrl+A as the hotkey: its Ctrl is also the copy chord's.
    let hotkey = Chord::parse("Ctrl+A").unwrap();
    platform.hotkeys.register(HotkeyAction::PriceCheck, &hotkey).unwrap();
    conn.set_input_focus(InputFocus::PARENT, game, x11rb::CURRENT_TIME).unwrap();
    conn.flush().unwrap();
    let game_clip = spawn_fake_game(display, game);
    std::thread::sleep(Duration::from_millis(200));
    platform.clipboard.set_text("before hotkey").unwrap();

    fake_key(conn, root, KC_CTRL_L, true);
    fake_key(conn, root, KC_A, true);
    assert_eq!(hotkeys.recv_timeout(Duration::from_secs(5)).expect("hotkey").action, HotkeyAction::PriceCheck);
    let releaser = std::thread::spawn({
        let display = display.to_owned();
        move || {
            let (c, s) = x11rb::connect(Some(&display)).unwrap();
            let r = c.setup().roots[s].root;
            std::thread::sleep(Duration::from_millis(100));
            fake_key(&c, r, KC_A, false);
        }
    });
    let opts = CopyOptions {
        advanced: false,
        timeout: Duration::from_secs(5),
        poll_interval: Duration::from_millis(20),
        restore_clipboard: true,
    };
    let text = copy_item_text(&*platform.input_simulator, &*platform.clipboard, opts, &std::thread::sleep)
        .expect("copy flow with held hotkey");
    releaser.join().unwrap();
    fake_key(conn, root, KC_CTRL_L, false);
    assert_eq!(text, ITEM);
    assert!(game_clip.join().unwrap(), "game never saw Ctrl+C while the hotkey was held");
    assert_eq!(platform.clipboard.get_text().unwrap().as_deref(), Some("before hotkey"));
    platform.hotkeys.unregister_all().unwrap();
}
