//! X11 / XWayland backend (default on Linux whenever `DISPLAY` is set).
mod conn;
pub(crate) mod input;
mod keystate;
mod overlay;
mod tracker;

pub use overlay::{X11Overlay, overlay};

use crate::backend::Unsupported;
use crate::detect::{BackendKind, EnvProbe};
use crate::{Capabilities, Clipboard, InputListener, InputSimulator, Platform, PlatformError};
use conn::XConn;
use input::{ArboardClipboard, EnigoSimulator, EnigoTarget, GlobalHotkeys, HookListener};
use tracker::X11WindowTracker;

/// Connect to `$DISPLAY` and build the platform. Fails only when there is no X server;
/// individual pieces that fail degrade to [`Unsupported`] with the capability flag cleared.
pub fn create() -> Result<Platform, PlatformError> {
    let probe = EnvProbe::from_env();
    let display = probe.display.clone();
    let x = XConn::connect(display.as_deref())?;
    let mut caps = Capabilities {
        window_tracking: true,
        overlay_always_on_top: true,
        overlay_click_through: x.has_extension("XFIXES") && x.has_extension("SHAPE"),
        global_hotkeys: true,
        ..Capabilities::default()
    };
    let window_tracker = Box::new(X11WindowTracker::new(display.as_deref())?);
    let hotkeys = Box::new(GlobalHotkeys::new()?);

    let input_listener: Box<dyn InputListener> = if x.has_extension("RECORD") {
        caps.passive_hook = true;
        Box::new(HookListener)
    } else {
        caps.notes.push("X server lacks the RECORD extension: passive input hook disabled".into());
        Box::new(Unsupported("passive_hook"))
    };

    let input_simulator: Box<dyn InputSimulator> = if x.has_extension("XTEST") {
        caps.keystroke_injection = true;
        let sim = EnigoSimulator::new(EnigoTarget::X11);
        if let Err(e) = sim.ensure_ready() {
            caps.notes.push(format!("keystroke injection not ready at startup ({e}); retried on each copy"));
        }
        Box::new(sim)
    } else {
        caps.notes.push("X server lacks XTEST: cannot send Ctrl+C to the game".into());
        Box::new(Unsupported("keystroke_injection"))
    };

    let clipboard: Box<dyn Clipboard> = match ArboardClipboard::new() {
        Ok(c) => {
            caps.clipboard = true;
            Box::new(c)
        }
        Err(e) => {
            caps.notes.push(format!("clipboard unavailable: {e}"));
            Box::new(Unsupported("clipboard"))
        }
    };

    caps.notes.extend(session_notes(&probe));
    Ok(Platform {
        kind: BackendKind::X11,
        capabilities: caps,
        window_tracker,
        hotkeys,
        input_listener,
        input_simulator,
        clipboard,
    })
}

/// Diagnostics notes about the session the X11 backend runs in.
pub(crate) fn session_notes(probe: &EnvProbe) -> Vec<String> {
    let mut notes = Vec::new();
    if probe.wayland_display.is_some() {
        notes.push(
            "Running through XWayland; the game must also run under XWayland (the default for Proton/Wine)".into(),
        );
    }
    if probe.is_gnome() {
        notes.push("GNOME has no layer-shell; using X11/XWayland for the overlay".into());
    }
    notes
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn notes_mention_xwayland_and_gnome() {
        let probe = EnvProbe {
            os: "linux".into(),
            wayland_display: Some("wayland-0".into()),
            desktop: Some("GNOME".into()),
            ..EnvProbe::default()
        };
        let notes = session_notes(&probe);
        assert_eq!(notes.len(), 2);
        assert!(session_notes(&EnvProbe::default()).is_empty());
    }
}
