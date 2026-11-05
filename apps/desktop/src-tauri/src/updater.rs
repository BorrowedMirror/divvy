//! Background auto-update (tauri-plugin-updater).
//!
//! Opt-in by build config: the committed `tauri.conf.json` carries no updater section, so dev
//! builds and forks never register the plugin. Release builds merge
//! `.github/tauri-release.conf.json`, which gets `plugins.updater.pubkey` injected in CI. Only
//! then is the plugin registered and the periodic check started.
use std::{
    sync::{Arc, Mutex},
    time::Duration,
};

use serde::{Deserialize, Serialize};
use tauri::{AppHandle, Manager, Runtime};
use tauri_plugin_updater::{Update, UpdaterExt};
use tauri_specta::Event;

use crate::{ipc::IpcError, state::AppState};

/// Delay before the first check, so startup (hotkeys, league detection) is not slowed down.
const FIRST_CHECK_DELAY: Duration = Duration::from_secs(30);
const CHECK_INTERVAL: Duration = Duration::from_secs(24 * 60 * 60);

/// A newer, signed release is available. `install_update` applies it.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize, specta::Type, tauri_specta::Event)]
pub struct UpdateAvailable {
    pub version: String,
    /// Release notes from the updater manifest.
    pub notes: Option<String>,
    /// Publish date (RFC 3339) as announced by the manifest.
    pub date: Option<String>,
}

impl From<&Update> for UpdateAvailable {
    fn from(u: &Update) -> Self {
        Self {
            version: u.version.clone(),
            notes: u.body.clone().filter(|n| !n.trim().is_empty()),
            date: u.raw_json.get("pub_date").and_then(|d| d.as_str()).map(str::to_owned),
        }
    }
}

/// The last update found by a check; `install_update` installs exactly this one.
#[derive(Default)]
struct Pending(Mutex<Option<Update>>);

/// True when the merged app config enables the updater (`plugins.updater.pubkey` is non-empty).
pub fn enabled(config: &tauri::Config) -> bool {
    config
        .plugins
        .0
        .get("updater")
        .and_then(|u| u.get("pubkey"))
        .and_then(|k| k.as_str())
        .is_some_and(|k| !k.trim().is_empty())
}

/// Register the updater plugin and start the background check, if this build enables it.
pub fn init<R: Runtime>(app: &AppHandle<R>, state: &Arc<AppState>) {
    if !enabled(app.config()) {
        tracing::debug!("updater disabled: no plugins.updater.pubkey in the app config");
        return;
    }
    if let Err(e) = app.plugin(tauri_plugin_updater::Builder::new().build()) {
        state.record_error(format!("updater unavailable: {e}"));
        return;
    }
    app.manage(Pending::default());
    let app = app.clone();
    let state = Arc::clone(state);
    std::thread::Builder::new()
        .name("rkl-updater".into())
        .spawn(move || {
            std::thread::sleep(FIRST_CHECK_DELAY);
            loop {
                if state.settings().ui.check_updates {
                    tauri::async_runtime::block_on(check_and_notify(&app, &state));
                }
                std::thread::sleep(CHECK_INTERVAL);
            }
        })
        .ok();
}

async fn check_and_notify<R: Runtime>(app: &AppHandle<R>, state: &AppState) {
    let found = async { app.updater()?.check().await }.await;
    match found {
        Ok(Some(update)) => {
            let info = UpdateAvailable::from(&update);
            tracing::info!(version = %info.version, "update available");
            if let Some(p) = app.try_state::<Pending>() {
                *p.0.lock().expect("pending update lock") = Some(update);
            }
            let _ = info.emit(app);
        }
        Ok(None) => tracing::debug!("no update available"),
        Err(e) => {
            tracing::warn!("update check failed: {e}");
            state.record_error(format!("update check failed: {e}"));
        }
    }
}

async fn install<R: Runtime>(app: &AppHandle<R>) -> Result<(), IpcError> {
    let update = app
        .try_state::<Pending>()
        .and_then(|p| p.0.lock().expect("pending update lock").clone())
        .ok_or_else(|| IpcError::new("update", "no update is available"))?;
    update
        .download_and_install(|_, _| {}, || {})
        .await
        .map_err(|e| IpcError::new("update", format!("update failed: {e}")))
}

/// Download, verify and install the announced update, then restart into it.
/// On Windows the installer exits the app itself.
#[tauri::command]
#[specta::specta]
pub async fn install_update(app: AppHandle) -> Result<(), IpcError> {
    install(&app).await?;
    app.restart()
}

#[cfg(test)]
mod tests {
    use super::*;

    fn config(plugins: serde_json::Value) -> tauri::Config {
        let mut c: tauri::Config = serde_json::from_value(serde_json::json!({ "identifier": "test.rkl" })).unwrap();
        c.plugins = serde_json::from_value(plugins).unwrap();
        c
    }

    #[test]
    fn enabled_only_with_a_pubkey() {
        assert!(!enabled(&config(serde_json::json!({}))));
        assert!(!enabled(&config(serde_json::json!({ "updater": { "endpoints": [] } }))));
        assert!(!enabled(&config(serde_json::json!({ "updater": { "pubkey": "  " } }))));
        assert!(enabled(&config(serde_json::json!({ "updater": { "pubkey": "dW50cnVzdGVk" } }))));
    }

    #[test]
    fn committed_config_has_no_updater() {
        let text = include_str!("../tauri.conf.json");
        let c: serde_json::Value = serde_json::from_str(text).unwrap();
        assert!(c.pointer("/plugins/updater/pubkey").is_none(), "dev builds must not enable the updater");
    }

    #[test]
    fn release_override_points_at_latest_json_and_leaves_pubkey_to_ci() {
        let text = include_str!("../../../../.github/tauri-release.conf.json");
        let c: serde_json::Value = serde_json::from_str(text).unwrap();
        assert_eq!(
            c.pointer("/plugins/updater/endpoints/0").and_then(|e| e.as_str()),
            Some("https://github.com/nightah/rust-king-ledger/releases/latest/download/latest.json")
        );
        assert!(c.pointer("/plugins/updater/pubkey").is_none(), "release.yml injects the pubkey");
    }

    #[test]
    fn install_without_a_found_update_is_an_error() {
        let app = tauri::test::mock_app();
        let err = tauri::async_runtime::block_on(install(app.handle())).unwrap_err();
        assert_eq!(err.kind, "update");
    }
}
