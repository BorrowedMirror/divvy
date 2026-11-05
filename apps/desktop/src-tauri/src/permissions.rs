//! First-run permission report and platform degradation notes for the onboarding view.
//!
//! On macOS the report comes from live TCC checks (`backend::macos::recheck_permissions`); on
//! every other platform it is derived from the backend's [`Capabilities`]. The web onboarding
//! view polls [`recheck_permissions`] (every 2 s, only while that view is visible) and
//! [`PermissionsChanged`] is emitted whenever the answer changes.
use std::sync::{Arc, Mutex};

use rkl_platform::{BackendKind, Capabilities, EnvProbe};
use serde::{Deserialize, Serialize};
use tauri::{AppHandle, Manager, State};
use tauri_specta::Event;

use crate::{flow, ipc::IpcError, state::AppState};

pub const ACCESSIBILITY: &str = "accessibility";
pub const SCREEN_RECORDING: &str = "screen_recording";
pub const INPUT_MONITORING: &str = "input_monitoring";

/// macOS permissions in the order the onboarding shows them, with "required" flags and the
/// System Settings deep link (see docs/research/macos.md).
const MACOS_PERMISSIONS: [(&str, bool, &str); 3] = [
    (ACCESSIBILITY, true, "x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility"),
    (SCREEN_RECORDING, true, "x-apple.systempreferences:com.apple.preference.security?Privacy_ScreenCapture"),
    (INPUT_MONITORING, false, "x-apple.systempreferences:com.apple.preference.security?Privacy_ListenEvent"),
];

pub const NOTE_WAYLAND_PORTAL: &str =
    "Wayland-native: global hotkeys use the GlobalShortcuts desktop portal when the compositor offers it";
pub const NOTE_WAYLAND_TRIGGER: &str =
    "Wayland-native: without the portal, bind `rkl --trigger` (add `--pinned` for pinned) in your compositor";
pub const NOTE_WAYLAND_LAYER_SHELL: &str =
    "Wayland-native: the overlay is anchored to the output with layer-shell, not to the game window";
pub const NOTE_GNOME: &str = "GNOME: only X11/XWayland is supported; run the game under XWayland";

/// One OS permission and whether it is currently granted.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize, specta::Type)]
pub struct PermissionInfo {
    /// `accessibility`, `screen_recording`, `input_monitoring`.
    pub kind: String,
    pub granted: bool,
    /// Without it the core price-check flow does not work.
    pub required: bool,
    /// System Settings deep link, if the OS has one.
    pub settings_url: Option<String>,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize, specta::Type)]
pub struct PermissionReport {
    pub platform: BackendKind,
    /// Every permission relevant on this platform (granted ones included, so the UI can tick
    /// them off live). Empty where the OS has no permission model (Windows, Linux).
    pub missing: Vec<PermissionInfo>,
    /// Degradation notes (English, for the details list / bug reports).
    pub notes: Vec<String>,
    /// Running under GNOME (`XDG_CURRENT_DESKTOP`), which only supports X11/XWayland.
    pub gnome: bool,
}

impl PermissionReport {
    /// A required permission is still missing.
    pub fn blocked(&self) -> bool {
        self.missing.iter().any(|p| p.required && !p.granted)
    }
}

/// Pushed when a recheck finds the permission state changed.
#[derive(Debug, Clone, Serialize, Deserialize, specta::Type, tauri_specta::Event)]
pub struct PermissionsChanged {
    pub report: PermissionReport,
}

/// Source of live permission state. macOS asks TCC; elsewhere it is the static capability list.
pub trait PermissionProbe: Send + Sync {
    /// Permission kinds currently missing.
    fn missing(&self) -> Vec<String>;
    /// Prompt for / open the settings pane of `kind`.
    fn open_settings(&self, kind: &str);
}

/// Probe that just repeats the backend's start-up capability report.
#[derive(Debug, Clone, Default)]
pub struct StaticProbe(pub Vec<String>);

impl PermissionProbe for StaticProbe {
    fn missing(&self) -> Vec<String> {
        self.0.clone()
    }
    fn open_settings(&self, _kind: &str) {}
}

#[cfg(all(target_os = "macos", feature = "native"))]
#[derive(Debug, Clone, Copy)]
pub struct MacProbe;

#[cfg(all(target_os = "macos", feature = "native"))]
impl PermissionProbe for MacProbe {
    fn missing(&self) -> Vec<String> {
        rkl_platform::backend::macos::recheck_permissions()
    }
    fn open_settings(&self, kind: &str) {
        rkl_platform::backend::macos::open_permission_settings(kind);
    }
}

/// Pure report builder (unit-tested without a backend).
pub fn build_report(platform: BackendKind, missing: &[String], caps_notes: &[String], gnome: bool) -> PermissionReport {
    let has = |k: &str| missing.iter().any(|m| m == k);
    let permissions = match platform {
        BackendKind::Macos => MACOS_PERMISSIONS
            .iter()
            .map(|(kind, required, url)| PermissionInfo {
                kind: (*kind).into(),
                granted: !has(kind),
                required: *required,
                settings_url: Some((*url).into()),
            })
            .collect(),
        _ => missing
            .iter()
            .map(|kind| PermissionInfo { kind: kind.clone(), granted: false, required: false, settings_url: None })
            .collect(),
    };
    let mut notes: Vec<String> = Vec::new();
    if platform == BackendKind::Wayland {
        notes.extend([NOTE_WAYLAND_PORTAL, NOTE_WAYLAND_TRIGGER, NOTE_WAYLAND_LAYER_SHELL].map(String::from));
    }
    if gnome {
        notes.push(NOTE_GNOME.into());
    }
    for n in caps_notes {
        if !notes.contains(n) {
            notes.push(n.clone());
        }
    }
    PermissionReport { platform, missing: permissions, notes, gnome }
}

/// Whether the settings window should open on `#/onboarding` at start-up.
pub fn needs_onboarding(onboarding_done: bool, report: &PermissionReport) -> bool {
    !onboarding_done || report.blocked()
}

/// Tauri-managed permission state: the probe plus the last report seen (for change events).
pub struct Permissions {
    platform: BackendKind,
    gnome: bool,
    caps: Capabilities,
    probe: Box<dyn PermissionProbe>,
    last: Mutex<Option<PermissionReport>>,
}

impl std::fmt::Debug for Permissions {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("Permissions").field("platform", &self.platform).finish_non_exhaustive()
    }
}

impl Permissions {
    pub fn new(platform: BackendKind, caps: Capabilities, gnome: bool, probe: Box<dyn PermissionProbe>) -> Self {
        Self { platform, gnome, caps, probe, last: Mutex::new(None) }
    }

    /// Probe for the running platform.
    pub fn for_state(state: &AppState) -> Self {
        let caps = state.capabilities();
        #[cfg(all(target_os = "macos", feature = "native"))]
        let probe: Box<dyn PermissionProbe> = if state.backend == BackendKind::Macos {
            Box::new(MacProbe)
        } else {
            Box::new(StaticProbe(caps.missing_permissions.clone()))
        };
        #[cfg(not(all(target_os = "macos", feature = "native")))]
        let probe: Box<dyn PermissionProbe> = Box::new(StaticProbe(caps.missing_permissions.clone()));
        Self::new(state.backend, caps, EnvProbe::from_env().is_gnome(), probe)
    }

    fn report(&self) -> PermissionReport {
        build_report(self.platform, &self.probe.missing(), &self.caps.notes, self.gnome)
    }

    /// Current report; remembered as the baseline for [`Self::recheck`].
    pub fn current(&self) -> PermissionReport {
        let report = self.report();
        *self.last.lock().expect("permissions lock") = Some(report.clone());
        report
    }

    /// Re-query the probe. Returns the report and whether it differs from the previous one.
    pub fn recheck(&self) -> (PermissionReport, bool) {
        let report = self.report();
        let mut last = self.last.lock().expect("permissions lock");
        let changed = last.as_ref().is_some_and(|l| *l != report);
        *last = Some(report.clone());
        (report, changed)
    }

    /// Open the settings pane for `kind` if it is a permission on this platform.
    pub fn request(&self, kind: &str) -> Result<(), IpcError> {
        let known = self.platform == BackendKind::Macos && MACOS_PERMISSIONS.iter().any(|(k, ..)| *k == kind);
        if !known {
            return Err(IpcError::new("platform", format!("permission `{kind}` cannot be requested on this platform")));
        }
        self.probe.open_settings(kind);
        Ok(())
    }
}

/// Register the managed state and open `#/onboarding` when it is needed. Call from `setup`.
pub fn init(app: &AppHandle, state: &AppState) {
    let perms = Permissions::for_state(state);
    let report = perms.current();
    app.manage(perms);
    if needs_onboarding(state.settings().ui.onboarding_done, &report)
        && let Err(e) = flow::show_settings(app, "onboarding")
    {
        state.record_error(format!("onboarding: {e}"));
    }
}

fn managed(app: &AppHandle) -> Result<State<'_, Permissions>, IpcError> {
    app.try_state::<Permissions>().ok_or_else(|| IpcError::new("internal", "permissions not initialised"))
}

fn fallback(app: &AppHandle) -> PermissionReport {
    let state = app.state::<Arc<AppState>>();
    let caps = state.capabilities();
    build_report(state.backend, &caps.missing_permissions, &caps.notes, EnvProbe::from_env().is_gnome())
}

#[tauri::command]
#[specta::specta]
pub fn get_permissions(app: AppHandle) -> PermissionReport {
    match managed(&app) {
        Ok(p) => p.current(),
        Err(_) => fallback(&app),
    }
}

#[tauri::command]
#[specta::specta]
pub fn request_permission(app: AppHandle, kind: String) -> Result<(), IpcError> {
    managed(&app)?.request(&kind)
}

#[tauri::command]
#[specta::specta]
pub fn recheck_permissions(app: AppHandle) -> PermissionReport {
    let Ok(p) = managed(&app) else { return fallback(&app) };
    let (report, changed) = p.recheck();
    if changed {
        let _ = PermissionsChanged { report: report.clone() }.emit(&app);
    }
    report
}

#[cfg(test)]
mod tests {
    use std::sync::atomic::{AtomicUsize, Ordering};

    use super::*;

    /// Fake TCC: missing kinds can be changed between rechecks; records opened panes.
    #[derive(Default)]
    struct FakeProbe {
        missing: Arc<Mutex<Vec<String>>>,
        opened: Arc<Mutex<Vec<String>>>,
        calls: Arc<AtomicUsize>,
    }

    impl PermissionProbe for FakeProbe {
        fn missing(&self) -> Vec<String> {
            self.calls.fetch_add(1, Ordering::SeqCst);
            self.missing.lock().unwrap().clone()
        }
        fn open_settings(&self, kind: &str) {
            self.opened.lock().unwrap().push(kind.into());
        }
    }

    fn strings(v: &[&str]) -> Vec<String> {
        v.iter().map(|s| (*s).to_owned()).collect()
    }

    #[test]
    fn macos_lists_all_permissions_in_order_with_deep_links() {
        let r = build_report(BackendKind::Macos, &strings(&[SCREEN_RECORDING]), &[], false);
        let kinds: Vec<_> = r.missing.iter().map(|p| (p.kind.as_str(), p.granted, p.required)).collect();
        assert_eq!(
            kinds,
            [(ACCESSIBILITY, true, true), (SCREEN_RECORDING, false, true), (INPUT_MONITORING, true, false)]
        );
        assert!(r.missing.iter().all(|p| p.settings_url.as_deref().unwrap().starts_with("x-apple.systempreferences:")));
        assert!(r.missing[1].settings_url.as_deref().unwrap().ends_with("Privacy_ScreenCapture"));
        assert!(r.blocked());
        assert!(r.notes.is_empty());
    }

    #[test]
    fn optional_permission_missing_does_not_block() {
        let r = build_report(BackendKind::Macos, &strings(&[INPUT_MONITORING]), &[], false);
        assert!(!r.blocked());
        assert!(!needs_onboarding(true, &r));
        assert!(needs_onboarding(false, &r));
    }

    #[test]
    fn windows_and_x11_have_no_permissions() {
        for kind in [BackendKind::Windows, BackendKind::X11] {
            let r = build_report(kind, &[], &strings(&["X server lacks XTEST"]), false);
            assert!(r.missing.is_empty());
            assert!(!r.blocked());
            assert_eq!(r.notes, strings(&["X server lacks XTEST"]));
            assert!(!needs_onboarding(true, &r));
        }
    }

    #[test]
    fn non_macos_missing_capabilities_are_listed_but_not_required() {
        let r = build_report(BackendKind::X11, &strings(&["foo"]), &[], false);
        assert_eq!(
            r.missing,
            [PermissionInfo { kind: "foo".into(), granted: false, required: false, settings_url: None }]
        );
        assert!(!r.blocked());
    }

    #[test]
    fn wayland_notes_explain_portal_trigger_and_layer_shell() {
        let caps_notes = strings(&["Wayland-native: no passive input hook", NOTE_WAYLAND_PORTAL]);
        let r = build_report(BackendKind::Wayland, &[], &caps_notes, false);
        assert_eq!(r.notes[..3], strings(&[NOTE_WAYLAND_PORTAL, NOTE_WAYLAND_TRIGGER, NOTE_WAYLAND_LAYER_SHELL]));
        assert!(r.notes.iter().any(|n| n.contains("rkl --trigger")));
        assert_eq!(r.notes.len(), 4, "duplicate capability notes are dropped");
        assert!(!r.gnome);
    }

    #[test]
    fn gnome_note_is_added() {
        let r = build_report(BackendKind::X11, &[], &[], true);
        assert!(r.gnome);
        assert_eq!(r.notes, strings(&[NOTE_GNOME]));
    }

    #[test]
    fn recheck_reports_changes_only_after_baseline() {
        let probe = FakeProbe::default();
        let missing = Arc::clone(&probe.missing);
        let calls = Arc::clone(&probe.calls);
        *missing.lock().unwrap() = strings(&[ACCESSIBILITY, SCREEN_RECORDING]);
        let p = Permissions::new(BackendKind::Macos, Capabilities::default(), false, Box::new(probe));

        let (first, changed) = p.recheck();
        assert!(!changed, "no baseline yet");
        assert!(first.blocked());
        assert!(!p.recheck().1, "unchanged");

        missing.lock().unwrap().retain(|k| k != ACCESSIBILITY);
        let (r, changed) = p.recheck();
        assert!(changed);
        assert!(r.missing[0].granted);
        assert!(r.blocked());

        missing.lock().unwrap().clear();
        let (r, changed) = p.recheck();
        assert!(changed);
        assert!(!r.blocked());
        assert_eq!(p.current(), r);
        assert!(!p.recheck().1);
        assert_eq!(calls.load(Ordering::SeqCst), 6);
    }

    #[test]
    fn request_opens_known_macos_panes_only() {
        let probe = FakeProbe::default();
        let opened = Arc::clone(&probe.opened);
        let p = Permissions::new(BackendKind::Macos, Capabilities::default(), false, Box::new(probe));
        p.request(ACCESSIBILITY).unwrap();
        p.request(INPUT_MONITORING).unwrap();
        let err = p.request("camera").unwrap_err();
        assert_eq!(err.kind, "platform");
        assert_eq!(*opened.lock().unwrap(), strings(&[ACCESSIBILITY, INPUT_MONITORING]));
    }

    #[test]
    fn request_is_unsupported_off_macos() {
        let p =
            Permissions::new(BackendKind::Wayland, Capabilities::default(), false, Box::new(StaticProbe::default()));
        assert_eq!(p.request(ACCESSIBILITY).unwrap_err().kind, "platform");
        assert!(format!("{p:?}").contains("Wayland"));
    }

    #[test]
    fn static_probe_uses_capability_notes() {
        let caps = Capabilities { notes: strings(&["note"]), ..Capabilities::default() };
        let p = Permissions::new(BackendKind::X11, caps, false, Box::new(StaticProbe(strings(&["x"]))));
        let r = p.current();
        assert_eq!(r.notes, strings(&["note"]));
        assert_eq!(r.missing.len(), 1);
        StaticProbe::default().open_settings("x");
    }
}
