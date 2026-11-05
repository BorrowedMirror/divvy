//! Debug-only IPC test hook (see CONTRACTS.md). Enabled in debug builds or with the
//! `test-hook` feature, and only when `KALANDRA_TEST_HOOK=<port>` is set. Binds 127.0.0.1 only.
//!
//! Line protocol: `trigger 0|1`, `paste <base64?>` is not supported — use `text <json string>`,
//! `state`, `tray <menu-id>`, `settings`, `quit`. Each command answers one JSON line.
use std::{
    io::{BufRead, BufReader, Write},
    net::{TcpListener, TcpStream},
    sync::{Arc, atomic::Ordering},
};

use tauri::{AppHandle, Manager};

use crate::{flow, state::AppState, tray};

pub fn port_from_env() -> Option<u16> {
    std::env::var("KALANDRA_TEST_HOOK").ok()?.parse().ok()
}

pub fn spawn(app: &AppHandle, port: u16) -> std::io::Result<u16> {
    let listener = TcpListener::bind(("127.0.0.1", port))?;
    let bound = listener.local_addr()?.port();
    let app = app.clone();
    std::thread::Builder::new().name("kalandra-test-hook".into()).spawn(move || {
        for stream in listener.incoming().flatten() {
            let app = app.clone();
            std::thread::spawn(move || serve(&app, stream));
        }
    })?;
    tracing::info!("test hook listening on 127.0.0.1:{bound}");
    Ok(bound)
}

fn serve(app: &AppHandle, stream: TcpStream) {
    let Ok(read) = stream.try_clone() else { return };
    let mut out = stream;
    for line in BufReader::new(read).lines().map_while(Result::ok) {
        let reply = handle(app, line.trim());
        if writeln!(out, "{reply}").is_err() {
            break;
        }
    }
}

pub fn handle(app: &AppHandle, line: &str) -> serde_json::Value {
    let state = app.state::<Arc<AppState>>();
    let (cmd, arg) = line.split_once(' ').unwrap_or((line, ""));
    match cmd {
        "trigger" => {
            flow::trigger(app, arg.trim() == "1");
            serde_json::json!({"ok": true})
        }
        "text" => match serde_json::from_str::<String>(arg) {
            Ok(text) => match flow::handle_item_text(app, &state, &text, false) {
                Ok((item, _)) => serde_json::json!({"ok": true, "base_type": item.base_type}),
                Err(e) => serde_json::json!({"ok": false, "error": e.to_string()}),
            },
            Err(e) => serde_json::json!({"ok": false, "error": e.to_string()}),
        },
        "state" => {
            let last_item = state.last_item.lock().expect("lock").as_ref().map(|(i, _)| i.base_type.clone());
            let last_price = state.last_price.lock().expect("lock").clone();
            serde_json::json!({
                "ok": true,
                "overlay_visible": state.overlay_visible.load(Ordering::SeqCst),
                "pinned": state.is_pinned(),
                "last_item": last_item,
                "last_price": last_price,
                "settings": state.settings(),
            })
        }
        "tray" => {
            tray::handle_menu(app, arg.trim());
            serde_json::json!({"ok": true})
        }
        "quit" => {
            state.quitting.store(true, Ordering::SeqCst);
            app.exit(0);
            serde_json::json!({"ok": true})
        }
        other => serde_json::json!({"ok": false, "error": format!("unknown command `{other}`")}),
    }
}
