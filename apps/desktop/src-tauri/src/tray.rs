//! System tray: settings, diagnostics, price check, quit.
use std::sync::{Arc, atomic::Ordering};

use tauri::{
    AppHandle, Manager,
    menu::{Menu, MenuItem, PredefinedMenuItem},
    tray::TrayIconBuilder,
};

use crate::{
    flow,
    state::AppState,
    tray_i18n::{self, TrayLabelsOwned},
};

pub const TRAY_ID: &str = "rkl-tray";

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

fn menu(app: &AppHandle, l: &TrayLabelsOwned) -> tauri::Result<Menu<tauri::Wry>> {
    Menu::with_items(
        app,
        &[
            &MenuItem::with_id(app, ids::PRICE_CHECK, &l.price_check, true, None::<&str>)?,
            &MenuItem::with_id(app, ids::SETTINGS, &l.settings, true, None::<&str>)?,
            &MenuItem::with_id(app, ids::DIAGNOSTICS, &l.diagnostics, true, None::<&str>)?,
            &PredefinedMenuItem::separator(app)?,
            &MenuItem::with_id(app, ids::QUIT, &l.quit, true, None::<&str>)?,
        ],
    )
}

/// Rebuild the tray menu in `language` (after `settings.ui.language` changed).
pub fn relabel(app: &AppHandle, language: &str) {
    let Some(tray) = app.tray_by_id(TRAY_ID) else { return };
    let labels = tray_i18n::labels(language);
    let result =
        menu(app, &labels).and_then(|m| tray.set_menu(Some(m))).and_then(|()| tray.set_tooltip(Some(&labels.tooltip)));
    if let Err(e) = result {
        app.state::<Arc<AppState>>().record_error(format!("tray relabel: {e}"));
    }
}

pub fn build(app: &AppHandle, language: &str) -> tauri::Result<()> {
    let labels = tray_i18n::labels(language);
    let menu = menu(app, &labels)?;
    let builder = TrayIconBuilder::with_id(TRAY_ID)
        .tooltip(&labels.tooltip)
        .menu(&menu)
        .on_menu_event(|app, event| handle_menu(app, event.id().as_ref()));
    // Dedicated small-size glyphs (assets/brand/build-icons.sh): the full app tile is too
    // detailed at tray size. macOS gets a black template image that the menu bar recolours.
    #[cfg(target_os = "macos")]
    let builder = builder.icon(tauri::include_image!("icons/tray-template.png")).icon_as_template(true);
    #[cfg(not(target_os = "macos"))]
    let builder = builder.icon(tauri::include_image!("icons/tray.png"));
    builder.build(app)?;
    Ok(())
}
