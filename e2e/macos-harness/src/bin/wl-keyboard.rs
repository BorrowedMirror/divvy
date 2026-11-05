//! Keeps a `zwp_virtual_keyboard_v1` with a full xkb keymap alive on a headless wlroots
//! compositor, so its seat has a keyboard (and a real keymap) like any desktop session.
//!
//! Headless sway has no input devices: its seat sends clients a `no_keymap` keymap, and enigo
//! (the app's Wayland keystroke injector) then cannot map modifiers, so `Ctrl+Alt+C` reaches the
//! game without modifier state. Used only by `e2e/scripts/run-hook-wayland.sh`.
//!
//!   wl-keyboard <keymap file>     (xkb v1 text, e.g. `xkbcli compile-keymap --layout us`)
//!
//! Prints `READY` once the keymap is uploaded, then blocks until killed.
#[cfg(target_os = "linux")]
fn main() -> anyhow::Result<()> {
    use std::io::Write;
    use std::os::fd::AsFd;

    use anyhow::Context;
    use wayland_client::globals::{GlobalListContents, registry_queue_init};
    use wayland_client::protocol::{wl_registry::WlRegistry, wl_seat::WlSeat};
    use wayland_client::{Connection, Dispatch, delegate_noop};
    use wayland_protocols_misc::zwp_virtual_keyboard_v1::client::{
        zwp_virtual_keyboard_manager_v1::ZwpVirtualKeyboardManagerV1, zwp_virtual_keyboard_v1::ZwpVirtualKeyboardV1,
    };

    struct State;
    impl Dispatch<WlRegistry, GlobalListContents> for State {
        fn event(
            _: &mut Self,
            _: &WlRegistry,
            _: <WlRegistry as wayland_client::Proxy>::Event,
            _: &GlobalListContents,
            _: &Connection,
            _: &wayland_client::QueueHandle<Self>,
        ) {
        }
    }
    delegate_noop!(State: ignore WlSeat);
    delegate_noop!(State: ZwpVirtualKeyboardManagerV1);
    delegate_noop!(State: ZwpVirtualKeyboardV1);

    let path = std::env::args_os().nth(1).context("usage: wl-keyboard <keymap file>")?;
    let mut keymap = std::fs::read(&path).with_context(|| format!("reading {}", path.to_string_lossy()))?;
    if keymap.last() != Some(&0) {
        keymap.push(0); // the compositor reads `size` bytes as a C string
    }
    let mut file = tempfile()?;
    file.write_all(&keymap)?;

    let conn = Connection::connect_to_env().context("connecting to WAYLAND_DISPLAY")?;
    let (globals, mut queue) = registry_queue_init::<State>(&conn)?;
    let qh = queue.handle();
    let seat: WlSeat = globals.bind(&qh, 1..=7, ()).context("wl_seat")?;
    let manager: ZwpVirtualKeyboardManagerV1 =
        globals.bind(&qh, 1..=1, ()).context("compositor lacks zwp_virtual_keyboard_manager_v1")?;
    let keyboard = manager.create_virtual_keyboard(&seat, &qh, ());
    keyboard.keymap(1 /* XKB_V1 */, file.as_fd(), u32::try_from(keymap.len())?);
    queue.roundtrip(&mut State)?;
    println!("READY");
    loop {
        queue.blocking_dispatch(&mut State)?;
    }
}

#[cfg(target_os = "linux")]
fn tempfile() -> anyhow::Result<std::fs::File> {
    let dir = std::env::var_os("XDG_RUNTIME_DIR").map_or_else(std::env::temp_dir, Into::into);
    let path = std::path::Path::new(&dir).join(format!("wl-keyboard-{}.xkb", std::process::id()));
    let file = std::fs::OpenOptions::new().read(true).write(true).create_new(true).open(&path)?;
    std::fs::remove_file(&path)?; // keep only the fd
    Ok(file)
}

#[cfg(not(target_os = "linux"))]
fn main() {
    eprintln!("wl-keyboard is Linux-only");
    std::process::exit(2);
}
