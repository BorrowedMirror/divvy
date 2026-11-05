//! macOS backend (workstream E).
//!
//! - Window tracking: `CGWindowListCopyWindowInfo` snapshots, made event-driven by an
//!   `AXObserver` on the game's pid running on a dedicated CFRunLoop thread ([`window`]).
//! - Hotkeys: `global-hotkey` (Carbon `RegisterEventHotKey`, no permission needed).
//! - Passive hook: `rdev` listen-only event tap (Input Monitoring or Accessibility).
//! - Keystroke injection: `enigo` (Accessibility). The copy chord's Ctrl becomes Cmd by default.
//! - Clipboard: `arboard`.
//!
//! Missing permissions never fail [`create`]; they turn capabilities off and are listed in
//! [`crate::Capabilities::missing_permissions`]. See `docs/research/macos.md` for the UX flow.
mod clipboard;
mod hotkeys;
mod input;
mod keymap;
mod model;
mod permissions;
mod window;

pub use model::CopyModifier;

use crate::{BackendKind, Platform, PlatformError};

/// Env var selecting the copy chord modifier (`cmd` or `ctrl`), read by [`create`].
pub const COPY_MODIFIER_ENV: &str = "KALANDRA_MACOS_COPY_MODIFIER";

/// Tunables for the macOS backend.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub struct MacosOptions {
    /// Modifier the game's copy shortcut uses (Cmd for the native client, Ctrl under Wine).
    pub copy_modifier: CopyModifier,
    /// Show the system Accessibility prompt during [`create_with`] if not yet trusted.
    pub prompt_for_accessibility: bool,
}

impl MacosOptions {
    /// Defaults, with `KALANDRA_MACOS_COPY_MODIFIER` applied when set and valid.
    pub fn from_env() -> Self {
        let copy_modifier = std::env::var(COPY_MODIFIER_ENV)
            .ok()
            .and_then(|v| {
                let parsed = CopyModifier::parse(&v);
                if parsed.is_none() {
                    tracing::warn!(value = %v, "ignoring invalid {COPY_MODIFIER_ENV}");
                }
                parsed
            })
            .unwrap_or_default();
        Self { copy_modifier, ..Self::default() }
    }
}

/// Build the macOS platform with options from the environment.
pub fn create() -> Result<Platform, PlatformError> {
    create_with(MacosOptions::from_env())
}

/// Build the macOS platform. Call on the main thread: Carbon hotkeys are delivered through the
/// application event target, which the Tauri/AppKit main run loop drives.
pub fn create_with(options: MacosOptions) -> Result<Platform, PlatformError> {
    if options.prompt_for_accessibility {
        permissions::request(model::PERM_ACCESSIBILITY);
    }
    let perms = permissions::current();
    let mut capabilities = model::capabilities(perms);
    let hotkeys: Box<dyn crate::HotkeyHost> = match hotkeys::MacHotkeyHost::new() {
        Ok(host) => Box::new(host),
        Err(e) => {
            tracing::warn!(error = %e, "global hotkeys unavailable");
            capabilities.global_hotkeys = false;
            capabilities.notes.push(format!("global hotkeys unavailable: {e}"));
            Box::new(hotkeys::DisabledHotkeys)
        }
    };
    tracing::info!(missing = ?capabilities.missing_permissions, "macos backend ready");
    Ok(Platform {
        kind: BackendKind::Macos,
        capabilities,
        window_tracker: Box::new(window::MacWindowTracker::new()),
        hotkeys,
        input_listener: Box::new(input::MacInputListener),
        input_simulator: Box::new(input::MacInputSimulator::new(options.copy_modifier)),
        clipboard: Box::new(clipboard::MacClipboard),
    })
}

/// Re-query TCC and return the permissions still missing (`accessibility`, `screen_recording`,
/// `input_monitoring`). Cheap; call when the settings/diagnostics page gains focus.
pub fn recheck_permissions() -> Vec<String> {
    permissions::current().missing()
}

/// Register the app for permission `kind` (shows the one-time system prompt where macOS has one)
/// and open the matching System Settings pane. Unknown kinds are logged and ignored.
pub fn open_permission_settings(kind: &str) {
    let Some(url) = model::settings_url(kind) else {
        tracing::warn!(kind, "unknown permission kind");
        return;
    };
    permissions::request(kind);
    match std::process::Command::new("/usr/bin/open").arg(url).spawn() {
        // Reap the child off-thread so callers (UI) never block and no zombie is left.
        // `std::thread::spawn` would panic if the thread cannot be created; log instead.
        Ok(mut child) => {
            if let Err(e) =
                std::thread::Builder::new().name("kalandra-open-settings".into()).spawn(move || child.wait())
            {
                tracing::warn!(error = %e, "failed to spawn reaper thread for System Settings");
            }
        }
        Err(e) => tracing::warn!(error = %e, url, "failed to open System Settings"),
    }
}
