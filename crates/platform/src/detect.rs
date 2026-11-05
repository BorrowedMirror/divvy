use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize, specta::Type)]
#[serde(rename_all = "snake_case")]
pub enum BackendKind {
    Windows,
    Macos,
    /// X11 or XWayland (default on Linux).
    X11,
    /// Wayland-native (layer-shell + portals).
    Wayland,
    /// In-process fakes (tests, `KALANDRA_BACKEND=fake`).
    Fake,
}

/// Environment snapshot used by [`detect`] (injectable for tests).
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct EnvProbe {
    pub os: String,
    pub xdg_session_type: Option<String>,
    pub wayland_display: Option<String>,
    pub display: Option<String>,
    /// `KALANDRA_BACKEND` value.
    pub override_backend: Option<String>,
    pub desktop: Option<String>,
}

impl EnvProbe {
    pub fn from_env() -> Self {
        let var = |k: &str| std::env::var(k).ok().filter(|v| !v.is_empty());
        Self {
            os: std::env::consts::OS.to_owned(),
            xdg_session_type: var("XDG_SESSION_TYPE"),
            wayland_display: var("WAYLAND_DISPLAY"),
            display: var("DISPLAY"),
            override_backend: var("KALANDRA_BACKEND"),
            desktop: var("XDG_CURRENT_DESKTOP"),
        }
    }
}

/// Pick a backend. Order: explicit override (settings) > `KALANDRA_BACKEND` > OS defaults.
/// On Linux, X11/XWayland is preferred whenever `DISPLAY` is set; Wayland-native only when
/// there is no X server, or when explicitly requested. GNOME with `DISPLAY` is X11 (XWayland);
/// GNOME without `DISPLAY` is Wayland-native, since there is no X server for GTK to open.
pub fn detect(probe: &EnvProbe, settings_override: Option<BackendKind>) -> BackendKind {
    if let Some(kind) = settings_override {
        return kind;
    }
    if let Some(kind) = probe.override_backend.as_deref().and_then(BackendKind::parse) {
        return kind;
    }
    match probe.os.as_str() {
        "windows" => BackendKind::Windows,
        "macos" => BackendKind::Macos,
        _ => linux_default(probe),
    }
}

fn linux_default(probe: &EnvProbe) -> BackendKind {
    // GNOME (mutter) implements neither layer-shell nor wlr virtual-keyboard, so the
    // Wayland-native backend is crippled there: prefer XWayland whenever `DISPLAY` is set.
    // Without `DISPLAY` (XWayland disabled) the X11 backend, and GTK's X11 init, would fail.
    if probe.display.is_some() {
        return BackendKind::X11;
    }
    if probe.wayland_display.is_some() || probe.xdg_session_type.as_deref() == Some("wayland") {
        return BackendKind::Wayland;
    }
    BackendKind::X11
}

impl BackendKind {
    /// Parse a `KALANDRA_BACKEND` value (`x11|wayland|windows|macos|fake`, case-insensitive).
    pub fn parse(s: &str) -> Option<Self> {
        match s.trim().to_ascii_lowercase().as_str() {
            "x11" | "xwayland" => Some(Self::X11),
            "wayland" => Some(Self::Wayland),
            "windows" => Some(Self::Windows),
            "macos" => Some(Self::Macos),
            "fake" => Some(Self::Fake),
            _ => None,
        }
    }
}

impl EnvProbe {
    /// `XDG_CURRENT_DESKTOP` contains `GNOME` (it is a colon-separated list, e.g. `ubuntu:GNOME`).
    pub fn is_gnome(&self) -> bool {
        self.desktop.as_deref().is_some_and(|d| d.split(':').any(|p| p.eq_ignore_ascii_case("gnome")))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn linux() -> EnvProbe {
        EnvProbe { os: "linux".into(), ..EnvProbe::default() }
    }

    #[test]
    fn settings_override_wins_over_everything() {
        let probe = EnvProbe { override_backend: Some("wayland".into()), display: Some(":0".into()), ..linux() };
        assert_eq!(detect(&probe, Some(BackendKind::Fake)), BackendKind::Fake);
    }

    #[test]
    fn env_override_wins_over_os() {
        for (v, k) in [
            ("x11", BackendKind::X11),
            ("XWayland", BackendKind::X11),
            ("Wayland", BackendKind::Wayland),
            ("windows", BackendKind::Windows),
            ("macos", BackendKind::Macos),
            (" fake ", BackendKind::Fake),
        ] {
            let probe = EnvProbe { override_backend: Some(v.into()), display: Some(":0".into()), ..linux() };
            assert_eq!(detect(&probe, None), k, "{v}");
        }
    }

    #[test]
    fn unknown_env_override_is_ignored() {
        let probe = EnvProbe { override_backend: Some("bogus".into()), display: Some(":0".into()), ..linux() };
        assert_eq!(detect(&probe, None), BackendKind::X11);
    }

    #[test]
    fn os_defaults() {
        assert_eq!(detect(&EnvProbe { os: "windows".into(), ..EnvProbe::default() }, None), BackendKind::Windows);
        assert_eq!(detect(&EnvProbe { os: "macos".into(), ..EnvProbe::default() }, None), BackendKind::Macos);
    }

    #[test]
    fn linux_prefers_x11_whenever_display_is_set() {
        let probe = EnvProbe {
            display: Some(":0".into()),
            wayland_display: Some("wayland-1".into()),
            xdg_session_type: Some("wayland".into()),
            desktop: Some("sway".into()),
            ..linux()
        };
        assert_eq!(detect(&probe, None), BackendKind::X11);
    }

    #[test]
    fn linux_wayland_native_without_display() {
        let probe = EnvProbe { wayland_display: Some("wayland-1".into()), ..linux() };
        assert_eq!(detect(&probe, None), BackendKind::Wayland);
        let probe = EnvProbe { xdg_session_type: Some("wayland".into()), ..linux() };
        assert_eq!(detect(&probe, None), BackendKind::Wayland);
    }

    #[test]
    fn gnome_is_x11_when_xwayland_is_available() {
        let probe = EnvProbe {
            display: Some(":0".into()),
            wayland_display: Some("wayland-0".into()),
            desktop: Some("ubuntu:GNOME".into()),
            ..linux()
        };
        assert!(probe.is_gnome());
        assert_eq!(detect(&probe, None), BackendKind::X11);
        // Without DISPLAY there is no X server: Wayland-native (X11 would fail GTK init).
        let probe = EnvProbe { wayland_display: Some("wayland-0".into()), desktop: Some("GNOME".into()), ..linux() };
        assert_eq!(detect(&probe, None), BackendKind::Wayland);
        let probe = EnvProbe { xdg_session_type: Some("wayland".into()), desktop: Some("GNOME".into()), ..linux() };
        assert_eq!(detect(&probe, None), BackendKind::Wayland);
    }

    #[test]
    fn explicit_wayland_request_is_honoured_on_gnome() {
        let probe = EnvProbe { desktop: Some("GNOME".into()), display: Some(":0".into()), ..linux() };
        assert_eq!(detect(&probe, Some(BackendKind::Wayland)), BackendKind::Wayland);
    }

    #[test]
    fn bare_linux_falls_back_to_x11() {
        assert_eq!(detect(&linux(), None), BackendKind::X11);
        assert!(!linux().is_gnome());
    }

    #[test]
    fn from_env_reads_os() {
        assert_eq!(EnvProbe::from_env().os, std::env::consts::OS);
    }
}
