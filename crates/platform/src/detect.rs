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
/// there is no X server, or when explicitly requested. GNOME is always X11.
pub fn detect(_probe: &EnvProbe, _settings_override: Option<BackendKind>) -> BackendKind {
    todo!("workstream F (platform common)")
}
