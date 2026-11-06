//! Debug-only IPC test hook (see CONTRACTS.md). Enabled in debug builds or with the
//! `test-hook` feature, and only when `RKL_TEST_HOOK=<port>` is set. Binds 127.0.0.1 only.
//!
//! Line protocol (see e2e/lib/hook.ts): `trigger 0|1`, `text <json string>`, `state`, `hide`,
//! `tray open_settings|open_diagnostics|price_check|quit`, `settings` (current settings as JSON),
//! `save_settings <json settings>` (validate + persist, like the IPC command), `quit`. One reply line each:
//! `ok`, `err <message>`, or a JSON object for `state`. When `RKL_TEST_HOOK_TOKEN` is set, the
//! first line must be `auth <token>` (no reply; anything else gets `err unauthorised` and the
//! connection closes). A line that looks like an HTTP request closes the connection.
use std::{
    io::{BufRead, BufReader, Write},
    net::{TcpListener, TcpStream},
    sync::{Arc, atomic::Ordering},
};

use tauri::{AppHandle, Manager};

use crate::{flow, state::AppState, tray};

pub fn port_from_env() -> Option<u16> {
    std::env::var("RKL_TEST_HOOK").ok()?.parse().ok()
}

pub fn spawn(app: &AppHandle, port: u16) -> std::io::Result<u16> {
    let listener = TcpListener::bind(("127.0.0.1", port))?;
    let bound = listener.local_addr()?.port();
    let app = app.clone();
    std::thread::Builder::new().name("rkl-test-hook".into()).spawn(move || {
        for stream in listener.incoming().flatten() {
            let app = app.clone();
            std::thread::spawn(move || serve(&app, stream));
        }
    })?;
    tracing::info!("test hook listening on 127.0.0.1:{bound}");
    Ok(bound)
}

/// Env var holding the shared secret; when set, a connection's first line must be `auth <token>`.
pub const TOKEN_ENV: &str = "RKL_TEST_HOOK_TOKEN";

/// Lines that look like HTTP (a browser page doing `fetch("http://127.0.0.1:<port>")`).
pub fn is_http_request(line: &str) -> bool {
    const METHODS: [&str; 9] = ["GET", "POST", "PUT", "DELETE", "HEAD", "OPTIONS", "PATCH", "CONNECT", "TRACE"];
    let first = line.split_whitespace().next().unwrap_or("");
    METHODS.iter().any(|m| first.eq_ignore_ascii_case(m))
}

/// Whether `line` authenticates against `token` (`None` = no token required).
pub fn authorised(line: &str, token: Option<&str>) -> bool {
    match token {
        None => true,
        Some(t) => line.strip_prefix("auth ").is_some_and(|got| !t.is_empty() && got.trim() == t),
    }
}

fn serve(app: &AppHandle, stream: TcpStream) {
    let Ok(read) = stream.try_clone() else { return };
    let mut out = stream;
    let token = std::env::var(TOKEN_ENV).ok();
    let mut authed = token.is_none();
    for line in BufReader::new(read).lines().map_while(Result::ok) {
        let line = line.trim();
        if is_http_request(line) {
            break;
        }
        if !authed {
            if authorised(line, token.as_deref()) {
                // No reply: clients send `auth <token>` and their command back to back.
                authed = true;
                continue;
            }
            let _ = writeln!(out, "err unauthorised");
            break;
        }
        let reply = handle(app, line);
        if writeln!(out, "{reply}").is_err() {
            break;
        }
    }
}

/// One reply line per command: `ok`, `err <message>`, or JSON for `state`.
pub fn handle(app: &AppHandle, line: &str) -> String {
    let state = app.state::<Arc<AppState>>();
    let (cmd, arg) = line.split_once(' ').unwrap_or((line, ""));
    let arg = arg.trim();
    match cmd {
        "trigger" => {
            flow::trigger(app, arg == "1");
            "ok".into()
        }
        "text" => match serde_json::from_str::<String>(arg) {
            Ok(text) => match flow::handle_item_text(app, &state, &text, false) {
                Ok(_) => "ok".into(),
                Err(e) => format!("err {e}"),
            },
            Err(e) => format!("err {e}"),
        },
        "state" => {
            let last_item = state.last_item.lock().expect("lock").as_ref().map(|(i, _)| i.base_type.clone());
            let last_price = state.last_price.lock().expect("lock").clone();
            let settings_visible =
                app.get_webview_window(flow::SETTINGS).and_then(|w| w.is_visible().ok()).unwrap_or(false);
            serde_json::json!({
                "overlay_visible": state.overlay_visible.load(Ordering::SeqCst),
                "overlay_pinned": state.is_pinned(),
                "settings_visible": settings_visible,
                "tray_present": app.tray_by_id(tray::TRAY_ID).is_some(),
                "last_item": last_item,
                "last_price": last_price,
                "overlay_layout": state.overlay_layout.lock().ok().map(|l| *l),
                "overlay_card": state.overlay_card.lock().ok().map(|c| *c),
            })
            .to_string()
        }
        "settings" => serde_json::to_string(&state.settings()).unwrap_or_else(|e| format!("err {e}")),
        "save_settings" => match serde_json::from_str(arg) {
            Ok(settings) => match flow::apply_settings(app, &state, settings) {
                Ok(_) => "ok".into(),
                Err(e) => format!("err {e}"),
            },
            Err(e) => format!("err {e}"),
        },
        "tray" => {
            let id = match arg {
                "open_settings" => tray::ids::SETTINGS,
                "open_diagnostics" => tray::ids::DIAGNOSTICS,
                "price_check" => tray::ids::PRICE_CHECK,
                "quit" => tray::ids::QUIT,
                other => return format!("err unknown tray item `{other}`"),
            };
            if app.tray_by_id(tray::TRAY_ID).is_none() {
                return "err tray is not available".into();
            }
            tray::handle_menu(app, id);
            "ok".into()
        }
        "hide" => match flow::hide_overlay(app, &state) {
            Ok(()) => "ok".into(),
            Err(e) => format!("err {e}"),
        },
        "quit" => {
            state.quitting.store(true, Ordering::SeqCst);
            let app = app.clone();
            // Reply first, then exit.
            std::thread::spawn(move || {
                std::thread::sleep(std::time::Duration::from_millis(50));
                app.exit(0);
            });
            "ok".into()
        }
        other => format!("err unknown command `{other}`"),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn rejects_http_requests() {
        assert!(is_http_request("GET / HTTP/1.1"));
        assert!(is_http_request("post /x HTTP/1.1"));
        assert!(is_http_request("OPTIONS * HTTP/1.1"));
        assert!(!is_http_request("trigger 1"));
        assert!(!is_http_request("state"));
        assert!(!is_http_request(""));
    }

    #[test]
    fn token_auth() {
        assert!(authorised("state", None));
        assert!(authorised("auth s3cret", Some("s3cret")));
        assert!(!authorised("auth wrong", Some("s3cret")));
        assert!(!authorised("state", Some("s3cret")));
        assert!(!authorised("auth ", Some("")));
    }
}
