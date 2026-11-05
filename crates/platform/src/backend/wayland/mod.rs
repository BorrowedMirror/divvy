//! Wayland-native backend (no `DISPLAY`, or `KALANDRA_BACKEND=wayland`).
//!
//! What Wayland allows a regular client:
//! - window tracking: impossible (no global window list) -> overlay anchored to the output;
//! - global hotkeys: XDG GlobalShortcuts portal when present, else SIGUSR2 (price check) / SIGUSR1 (pinned) / `kalandra --trigger`;
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

/// Hotkeys: the portal when available; SIGUSR2/SIGUSR1 always (fallback trigger).
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
                .inspect_err(|e| tracing::info!(error = %e, "GlobalShortcuts portal unavailable; signal fallback"))
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
            "No GlobalShortcuts portal: bind `kalandra --trigger` (or `pkill -USR2 kalandra`; `-USR1` to pin) in \
             your compositor"
                .into(),
        );
    }

    let enigo = EnigoSimulator::new(EnigoTarget::Wayland);
    let eager = enigo.ensure_ready();
    let ydotool = YdotoolSimulator::detect();
    let plan = injection_plan(eager.as_ref().err(), ydotool.is_some(), virtual_keyboard_plausible(probe));
    caps.keystroke_injection = plan.available;
    caps.notes.extend(plan.note);
    let input_simulator: Box<dyn InputSimulator> = match (plan.available, ydotool) {
        (false, _) => Box::new(Unsupported("keystroke_injection")),
        (true, Some(y)) => Box::new(enigo.with_fallback(Box::new(y))),
        (true, None) => Box::new(enigo),
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

/// Whether the compositor may implement `zwp_virtual_keyboard_v1` (wlroots-based ones do;
/// mutter and KWin do not).
pub(crate) fn virtual_keyboard_plausible(probe: &EnvProbe) -> bool {
    let kde = probe.desktop.as_deref().is_some_and(|d| d.split(':').any(|p| p.eq_ignore_ascii_case("kde")));
    !probe.is_gnome() && !kde
}

/// Keystroke-injection capability and its diagnostics note.
#[derive(Debug, Clone, PartialEq, Eq)]
pub(crate) struct InjectionPlan {
    /// "Available on demand": enigo is created lazily on each send until it works.
    pub(crate) available: bool,
    pub(crate) note: Option<String>,
}

/// `eager_error`: why enigo failed at startup (`None` = it works now).
pub(crate) fn injection_plan(eager_error: Option<&PlatformError>, ydotool: bool, plausible: bool) -> InjectionPlan {
    let (available, note) = match (eager_error, ydotool, plausible) {
        (None, _, _) => (true, None),
        (Some(e), true, _) => (
            true,
            Some(format!(
                "Virtual keyboard unavailable at startup ({e}); retried on each copy, ydotool (needs ydotoold) \
                 used meanwhile"
            )),
        ),
        (Some(e), false, true) => (
            true,
            Some(format!(
                "Virtual keyboard (virtual-keyboard protocol) not ready at startup ({e}); retried on each copy"
            )),
        ),
        (Some(e), false, false) => (false, Some(format!("No virtual-keyboard protocol and no ydotool: {e}"))),
    };
    InjectionPlan { available, note }
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
    fn injection_is_available_on_demand() {
        let e = PlatformError::Backend("enigo: no keymap".into());
        assert_eq!(injection_plan(None, false, false), InjectionPlan { available: true, note: None });
        let p = injection_plan(Some(&e), true, false);
        assert!(p.available && p.note.as_deref().is_some_and(|n| n.contains("ydotool")));
        let p = injection_plan(Some(&e), false, true);
        assert!(p.available && p.note.as_deref().is_some_and(|n| n.contains("retried")));
        let p = injection_plan(Some(&e), false, false);
        assert!(!p.available && p.note.as_deref().is_some_and(|n| n.contains("no ydotool")));
    }

    #[test]
    fn virtual_keyboard_plausibility_by_desktop() {
        let probe = |d: &str| EnvProbe { desktop: Some(d.into()), ..EnvProbe::default() };
        assert!(virtual_keyboard_plausible(&probe("sway")));
        assert!(virtual_keyboard_plausible(&EnvProbe::default()));
        assert!(!virtual_keyboard_plausible(&probe("ubuntu:GNOME")));
        assert!(!virtual_keyboard_plausible(&probe("KDE")));
    }

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
