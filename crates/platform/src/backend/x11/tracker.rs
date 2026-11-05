//! Game window tracking on X11: `_NET_ACTIVE_WINDOW`/`_NET_CLIENT_LIST` PropertyNotify on the
//! root, ConfigureNotify (SubstructureNotify on root catches WM frames moving; StructureNotify
//! on the client catches synthetic ICCCM configures) and title changes on the game window.
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::mpsc::{Receiver, Sender, channel};
use std::time::{Duration, Instant};

use x11rb::connection::Connection;
use x11rb::protocol::Event;
use x11rb::protocol::xproto::{ChangeWindowAttributesAux, ConnectionExt as _, EventMask, Window};

use super::conn::{XConn, x_err};
use crate::{PlatformError, Rect, WindowEvent, WindowInfo, WindowTracker};

/// Rescan interval when no relevant event arrived (titles set after mapping, odd WMs).
const RESCAN: Duration = Duration::from_millis(500);
const IDLE_SLEEP: Duration = Duration::from_millis(10);

#[derive(Debug)]
pub(crate) struct X11WindowTracker {
    conn: XConn,
    display: Option<String>,
    subscribed: AtomicBool,
}

impl X11WindowTracker {
    pub(crate) fn new(display: Option<&str>) -> Result<Self, PlatformError> {
        Ok(Self {
            conn: XConn::connect(display)?,
            display: display.map(str::to_owned),
            subscribed: AtomicBool::new(false),
        })
    }
}

impl WindowTracker for X11WindowTracker {
    fn game_window(&self) -> Option<WindowInfo> {
        self.conn.find_game().and_then(|w| self.conn.info(w))
    }

    fn is_game_focused(&self) -> bool {
        self.conn.find_game().is_some_and(|w| self.conn.is_focused(w))
    }

    fn subscribe(&self) -> Result<Receiver<WindowEvent>, PlatformError> {
        if self.subscribed.swap(true, Ordering::SeqCst) {
            return Err(PlatformError::AlreadyStarted);
        }
        // Own connection: the event loop must not steal replies from `game_window()` callers.
        let conn = match XConn::connect(self.display.as_deref()) {
            Ok(c) => c,
            Err(e) => {
                self.subscribed.store(false, Ordering::SeqCst);
                return Err(e);
            }
        };
        let mask = EventMask::PROPERTY_CHANGE | EventMask::SUBSTRUCTURE_NOTIFY;
        conn.conn
            .change_window_attributes(conn.root, &ChangeWindowAttributesAux::new().event_mask(mask))
            .map_err(x_err)?
            .check()
            .map_err(x_err)?;
        let (tx, rx) = channel();
        std::thread::Builder::new()
            .name("kalandra-x11-tracker".into())
            .spawn(move || run(&conn, &tx))
            .map_err(|e| PlatformError::Backend(format!("spawn tracker thread: {e}")))?;
        Ok(rx)
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
struct Tracked {
    window: Window,
    rect: Rect,
    focused: bool,
}

/// Diff the previous and current state into events.
fn diff(prev: Option<Tracked>, cur: Option<(Tracked, WindowInfo)>) -> Vec<WindowEvent> {
    match (prev, cur) {
        (None, None) => vec![],
        (Some(_), None) => vec![WindowEvent::Lost],
        (None, Some((_, info))) => vec![WindowEvent::Found(info)],
        (Some(p), Some((c, info))) if p.window != c.window => vec![WindowEvent::Lost, WindowEvent::Found(info)],
        (Some(p), Some((c, _))) => {
            let mut out = Vec::new();
            if p.rect != c.rect {
                out.push(WindowEvent::Moved { rect: c.rect });
            }
            if p.focused != c.focused {
                out.push(WindowEvent::FocusChanged { focused: c.focused });
            }
            out
        }
    }
}

fn snapshot(conn: &XConn) -> Option<(Tracked, WindowInfo)> {
    let w = conn.find_game()?;
    let info = conn.info(w)?;
    Some((Tracked { window: w, rect: info.rect, focused: info.focused }, info))
}

fn run(conn: &XConn, tx: &Sender<WindowEvent>) {
    let mut state: Option<Tracked> = None;
    let mut dirty = true;
    let mut last_scan = Instant::now();
    loop {
        loop {
            match conn.conn.poll_for_event() {
                Ok(Some(ev)) => dirty |= relevant(&ev),
                Ok(None) => break,
                Err(e) => {
                    tracing::warn!(error = %e, "x11 tracker connection lost");
                    let _ = tx.send(WindowEvent::Lost);
                    return;
                }
            }
        }
        if dirty || last_scan.elapsed() >= RESCAN {
            dirty = false;
            last_scan = Instant::now();
            let cur = snapshot(conn);
            if let Some((c, _)) = &cur
                && state.is_none_or(|p| p.window != c.window)
            {
                // Watch the client itself for synthetic ConfigureNotify, unmap/destroy, title.
                let mask = EventMask::STRUCTURE_NOTIFY | EventMask::PROPERTY_CHANGE;
                let _ =
                    conn.conn.change_window_attributes(c.window, &ChangeWindowAttributesAux::new().event_mask(mask));
                let _ = conn.conn.flush();
            }
            for ev in diff(state, cur.clone()) {
                if tx.send(ev).is_err() {
                    return;
                }
            }
            state = cur.map(|(c, _)| c);
        }
        std::thread::sleep(IDLE_SLEEP);
    }
}

fn relevant(ev: &Event) -> bool {
    matches!(
        ev,
        Event::PropertyNotify(_)
            | Event::ConfigureNotify(_)
            | Event::MapNotify(_)
            | Event::UnmapNotify(_)
            | Event::DestroyNotify(_)
            | Event::ReparentNotify(_)
    )
}

#[cfg(test)]
mod tests {
    use super::*;

    fn t(window: Window, x: i32, focused: bool) -> Tracked {
        Tracked { window, rect: Rect { x, y: 0, width: 10, height: 10 }, focused }
    }

    fn info(tr: Tracked) -> (Tracked, WindowInfo) {
        (
            tr,
            WindowInfo {
                title: "Path of Exile 2".into(),
                rect: tr.rect,
                focused: tr.focused,
                native_id: tr.window.into(),
            },
        )
    }

    #[test]
    fn diff_covers_all_transitions() {
        assert!(diff(None, None).is_empty());
        assert_eq!(diff(Some(t(1, 0, false)), None), vec![WindowEvent::Lost]);
        assert!(matches!(diff(None, Some(info(t(1, 0, false))))[..], [WindowEvent::Found(_)]));
        assert!(matches!(
            diff(Some(t(1, 0, false)), Some(info(t(2, 0, false))))[..],
            [WindowEvent::Lost, WindowEvent::Found(_)]
        ));
        assert!(diff(Some(t(1, 0, false)), Some(info(t(1, 0, false)))).is_empty());
        assert_eq!(
            diff(Some(t(1, 0, false)), Some(info(t(1, 5, true)))),
            vec![
                WindowEvent::Moved { rect: Rect { x: 5, y: 0, width: 10, height: 10 } },
                WindowEvent::FocusChanged { focused: true }
            ]
        );
    }
}
