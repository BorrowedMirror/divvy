//! macOS overlay styling (workstream E): turn the Tauri `overlay` window into a
//! non-activating `NSPanel` that floats over the full-screen game on every Space.
//!
//! Requires the tauri-nspanel plugin: register [`plugin`] on the builder, before `setup`
//! calls [`prepare`].
use tauri_nspanel::objc2_app_kit::NSWindowStyleMask;
use tauri_nspanel::{CollectionBehavior, ManagerExt, PanelLevel, WebviewPanelManager, WebviewWindowExt, tauri_panel};

tauri_panel! {
    panel!(RklOverlayPanel {
        config: {
            // Key only when a text field is clicked, so the game keeps keyboard input otherwise.
            can_become_key_window: true,
            can_become_main_window: false,
            becomes_key_only_if_needed: true,
            is_floating_panel: true
        }
    })
}

/// Window level for the overlay: one above the menu bar (`NSStatusWindowLevel`, 25). Enough to
/// sit over a full-screen game without covering system UI the way screen-saver level would.
const OVERLAY_LEVEL: PanelLevel = PanelLevel::Status;

/// The tauri-nspanel plugin. Register with `.plugin(overlay::macos::plugin())`.
pub fn plugin<R: tauri::Runtime>() -> tauri::plugin::TauriPlugin<R> {
    tauri_nspanel::init()
}

fn collection_behavior() -> CollectionBehavior {
    CollectionBehavior::new().can_join_all_spaces().full_screen_auxiliary().stationary().ignores_cycle()
}

/// Convert the overlay window into a panel and apply levels/behaviours. Idempotent.
pub fn prepare(window: &tauri::WebviewWindow) -> Result<(), String> {
    use tauri::Manager;
    if window.try_state::<WebviewPanelManager<tauri::Wry>>().is_none() {
        return Err("tauri-nspanel plugin not registered; add .plugin(overlay::macos::plugin())".into());
    }
    let panel = match window.get_webview_panel(window.label()) {
        Ok(panel) => panel,
        Err(_) => window.to_panel::<RklOverlayPanel>().map_err(|e| format!("convert overlay to NSPanel: {e}"))?,
    };
    panel
        .add_style_mask(NSWindowStyleMask::NonactivatingPanel)
        .map_err(|e| format!("set non-activating style: {e:?}"))?;
    panel.set_level(OVERLAY_LEVEL.value());
    panel.set_collection_behavior(collection_behavior().into());
    panel.set_floating_panel(true);
    panel.set_hides_on_deactivate(false);
    panel.set_works_when_modal(true);
    panel.set_has_shadow(false);
    Ok(())
}

/// Let mouse events pass through the overlay to the game (or not).
pub fn set_click_through(window: &tauri::WebviewWindow, enabled: bool) -> Result<(), String> {
    let panel = window.get_webview_panel(window.label()).map_err(|e| format!("overlay panel: {e:?}"))?;
    panel.set_ignores_mouse_events(enabled);
    Ok(())
}
