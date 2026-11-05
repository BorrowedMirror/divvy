//! Wayland-native backend (no `DISPLAY`, or `KALANDRA_BACKEND=wayland`).
//!
//! What Wayland allows a regular client:
//! - window tracking: impossible (no global window list) -> overlay anchored to the output;
//! - global hotkeys: XDG GlobalShortcuts portal when present, else SIGUSR2 / `kalandra --trigger`;
//! - passive hook: impossible;
//! - keystrokes: zwp_virtual_keyboard_v1 (wlroots: sway, Hyprland, river...) via enigo, else
//!   the `ydotool` CLI, else unsupported;
//! - clipboard: wlr-data-control via arboard;
//! - overlay: gtk-layer-shell `OVERLAY` layer, applied by the desktop app (not GNOME).
mod portal;
mod signal;
mod ydotool;

use std::collections::HashMap;
use std::sync::mpsc::{Receiver, sync_channel};
use std::sync::{Mutex, PoisonError};

use raw_window_handle::{RawDisplayHandle, RawWindowHandle};

pub use signal::sigusr2_trigger;

use crate::backend::Unsupported;
use crate::backend::x11::input::{ArboardClipboard, EnigoSimulator, EnigoTarget};
use crate::detect::{BackendKind, EnvProbe};
use crate::{
    Capabilities, Chord, Clipboard, HotkeyAction, HotkeyEvent, HotkeyHost, InputSimulator, OverlayWindow, Platform,
    PlatformError,
};
use portal::PortalHotkeys;
use ydotool::YdotoolSimulator;

/// Hotkeys: the portal when available; SIGUSR2 always (fallback trigger).
#[derive(Debug)]
pub(crate) struct WaylandHotkeys {
    portal: Option<PortalHotkeys>,
    registered: Mutex<HashMap<HotkeyAction, Chord>>,
    events: Mutex<Option<Receiver<HotkeyEvent>>>,
}

impl WaylandHotkeys {
    pub(crate) fn new(use_portal: bool) -> Result<Self, PlatformError> {
        let (tx, rx) = sync_channel(64);
        signal::spawn_sigusr2(tx.clone())?;
        let portal = if use_portal {
            PortalHotkeys::connect(tx, portal::CONNECT_TIMEOUT)
                .inspect_err(|e| tracing::info!(error = %e, "GlobalShortcuts portal unavailable; SIGUSR2 fallback"))
                .ok()
        } else {
            None
        };
        Ok(Self { portal, registered: Mutex::default(), events: Mutex::new(Some(rx)) })
    }

    pub(crate) fn has_portal(&self) -> bool {
        self.portal.is_some()
    }
}

impl HotkeyHost for WaylandHotkeys {
    fn register(&self, id: HotkeyAction, chord: &Chord) -> Result<(), PlatformError> {
        self.registered.lock().unwrap_or_else(PoisonError::into_inner).insert(id, chord.clone());
        self.portal.as_ref().map_or(Ok(()), |p| p.register(id, chord))
    }

    fn unregister(&self, id: HotkeyAction) -> Result<(), PlatformError> {
        self.registered.lock().unwrap_or_else(PoisonError::into_inner).remove(&id);
        self.portal.as_ref().map_or(Ok(()), |p| p.unregister(id))
    }

    fn unregister_all(&self) -> Result<(), PlatformError> {
        self.registered.lock().unwrap_or_else(PoisonError::into_inner).clear();
        self.portal.as_ref().map_or(Ok(()), PortalHotkeys::unregister_all)
    }

    fn events(&self) -> Result<Receiver<HotkeyEvent>, PlatformError> {
        self.events.lock().unwrap_or_else(PoisonError::into_inner).take().ok_or(PlatformError::AlreadyStarted)
    }
}

/// Build the Wayland-native platform.
pub fn create() -> Result<Platform, PlatformError> {
    create_with(&EnvProbe::from_env(), true)
}

pub(crate) fn create_with(probe: &EnvProbe, use_portal: bool) -> Result<Platform, PlatformError> {
    if probe.wayland_display.is_none() {
        return Err(PlatformError::Unsupported("wayland backend needs WAYLAND_DISPLAY"));
    }
    let gnome = probe.is_gnome();
    let mut caps = Capabilities {
        window_tracking: false,
        overlay_always_on_top: !gnome,
        overlay_click_through: true,
        passive_hook: false,
        ..Capabilities::default()
    };
    caps.notes.push("Wayland-native: game window cannot be tracked; overlay anchored to the output".into());
    caps.notes.push("Wayland-native: no passive input hook".into());
    if gnome {
        caps.notes.push("GNOME has no layer-shell: the overlay cannot stay above the game; use X11/XWayland".into());
    }

    let hotkeys = WaylandHotkeys::new(use_portal)?;
    caps.global_hotkeys = hotkeys.has_portal();
    if !caps.global_hotkeys {
        caps.notes.push(
            "No GlobalShortcuts portal: bind `kalandra --trigger` (or `pkill -USR2 kalandra`) in your compositor"
                .into(),
        );
    }

    let input_simulator: Box<dyn InputSimulator> = match EnigoSimulator::new(EnigoTarget::Wayland) {
        Ok(sim) => {
            caps.keystroke_injection = true;
            Box::new(sim)
        }
        Err(e) => {
            if let Some(y) = YdotoolSimulator::detect() {
                caps.keystroke_injection = true;
                caps.notes.push("Keystrokes via ydotool (needs ydotoold running)".into());
                Box::new(y)
            } else {
                caps.notes.push(format!("No virtual-keyboard protocol and no ydotool: {e}"));
                Box::new(Unsupported("keystroke_injection"))
            }
        }
    };

    let clipboard: Box<dyn Clipboard> = match ArboardClipboard::new() {
        Ok(c) => {
            caps.clipboard = true;
            Box::new(c)
        }
        Err(e) => {
            caps.notes.push(format!("clipboard unavailable (compositor lacks wlr-data-control?): {e}"));
            Box::new(Unsupported("clipboard"))
        }
    };

    Ok(Platform {
        kind: BackendKind::Wayland,
        capabilities: caps,
        window_tracker: Box::new(Unsupported("window_tracking")),
        hotkeys: Box::new(hotkeys),
        input_listener: Box::new(Unsupported("passive_hook")),
        input_simulator,
        clipboard,
    })
}

/// Wayland overlays are layer-shell surfaces configured on the GTK window by the desktop app
/// (`apps/desktop/src-tauri/src/overlay/linux.rs`); there is nothing to do over a raw handle.
pub fn overlay(window: RawWindowHandle, display: RawDisplayHandle) -> Result<Box<dyn OverlayWindow>, PlatformError> {
    let _ = (window, display);
    Err(PlatformError::Unsupported("wayland overlay is applied via gtk-layer-shell in the desktop app"))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn requires_wayland_display() {
        let probe = EnvProbe { os: "linux".into(), ..EnvProbe::default() };
        assert!(matches!(create_with(&probe, false), Err(PlatformError::Unsupported(_))));
    }

    #[test]
    fn overlay_over_raw_handle_is_unsupported() {
        let h = raw_window_handle::WaylandWindowHandle::new(std::ptr::NonNull::dangling());
        let d = raw_window_handle::WaylandDisplayHandle::new(std::ptr::NonNull::dangling());
        assert!(overlay(RawWindowHandle::Wayland(h), RawDisplayHandle::Wayland(d)).is_err());
    }

    #[test]
    fn fallback_hotkeys_accept_registrations_and_hand_out_events_once() {
        let h = WaylandHotkeys::new(false).unwrap();
        assert!(!h.has_portal());
        let chord = Chord::parse("ctrl+d").unwrap();
        h.register(HotkeyAction::PriceCheck, &chord).unwrap();
        h.unregister(HotkeyAction::PriceCheck).unwrap();
        h.unregister_all().unwrap();
        assert!(h.events().is_ok());
        assert!(matches!(h.events(), Err(PlatformError::AlreadyStarted)));
    }
}
