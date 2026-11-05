//! System tray: settings, diagnostics, price check, quit.
use std::sync::{Arc, atomic::Ordering};

use tauri::{
    AppHandle, Manager,
    menu::{Menu, MenuItem, PredefinedMenuItem},
    tray::TrayIconBuilder,
};

use crate::{flow, state::AppState};

pub const TRAY_ID: &str = "kalandra-tray";

/// Menu item ids (stable; used by e2e).
pub mod ids {
    pub const SETTINGS: &str = "tray-settings";
    pub const DIAGNOSTICS: &str = "tray-diagnostics";
    pub const PRICE_CHECK: &str = "tray-price-check";
    pub const QUIT: &str = "tray-quit";
}

pub fn handle_menu(app: &AppHandle, id: &str) {
    match id {
        ids::SETTINGS => {
            let _ = flow::show_settings(app, "settings");
        }
        ids::DIAGNOSTICS => {
            let _ = flow::show_settings(app, "diagnostics");
        }
        ids::PRICE_CHECK => flow::trigger(app, true),
        ids::QUIT => {
            app.state::<Arc<AppState>>().quitting.store(true, Ordering::SeqCst);
            app.exit(0);
        }
        _ => {}
    }
}

pub fn build(app: &AppHandle) -> tauri::Result<()> {
    let menu = Menu::with_items(
        app,
        &[
            &MenuItem::with_id(app, ids::PRICE_CHECK, "Price check (pinned)", true, None::<&str>)?,
            &MenuItem::with_id(app, ids::SETTINGS, "Settings…", true, None::<&str>)?,
            &MenuItem::with_id(app, ids::DIAGNOSTICS, "Diagnostics", true, None::<&str>)?,
            &PredefinedMenuItem::separator(app)?,
            &MenuItem::with_id(app, ids::QUIT, "Quit kalandra", true, None::<&str>)?,
        ],
    )?;
    let mut builder = TrayIconBuilder::with_id(TRAY_ID)
        .tooltip("kalandra")
        .menu(&menu)
        .on_menu_event(|app, event| handle_menu(app, event.id().as_ref()));
    if let Some(icon) = app.default_window_icon() {
        builder = builder.icon(icon.clone());
    }
    builder.build(app)?;
    Ok(())
}
