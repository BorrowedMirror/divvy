//! Game window tracking on X11: `_NET_ACTIVE_WINDOW`/`_NET_CLIENT_LIST` PropertyNotify on the
//! root, ConfigureNotify (SubstructureNotify on root catches WM frames moving; StructureNotify
//! on the client catches synthetic ICCCM configures) and title changes on the game window.
//!
//! The event thread blocks in poll(2) on the X socket (no busy polling). It wakes for relevant
//! events, or every [`RESCAN`] for a full rescan (titles set after mapping, odd WMs), and exits
//! once the receiver is dropped (next send fails) or the tracker is dropped (next wake-up).
use std::os::fd::AsRawFd as _;
use std::sync::Arc;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::mpsc::{Receiver, Sender, channel};
use std::time::{Duration, Instant};

use x11rb::connection::Connection;
use x11rb::protocol::Event;
use x11rb::protocol::xproto::{Atom, AtomEnum, ChangeWindowAttributesAux, ConnectionExt as _, EventMask, Window};

use super::conn::{XConn, x_err};
use crate::{PlatformError, Rect, WindowEvent, WindowInfo, WindowTracker};

/// Rescan interval when no relevant event arrived; also the poll(2) timeout.
const RESCAN: Duration = Duration::from_secs(1);

#[derive(Debug)]
pub(crate) struct X11WindowTracker {
    conn: XConn,
    display: Option<String>,
    subscribed: AtomicBool,
    stop: Arc<AtomicBool>,
}

impl Drop for X11WindowTracker {
    fn drop(&mut self) {
        self.stop.store(true, Ordering::SeqCst);
    }
}

impl X11WindowTracker {
    pub(crate) fn new(display: Option<&str>) -> Result<Self, PlatformError> {
        Ok(Self {
            conn: XConn::connect(display)?,
            display: display.map(str::to_owned),
            subscribed: AtomicBool::new(false),
            stop: Arc::default(),
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
        let stop = Arc::clone(&self.stop);
        std::thread::Builder::new()
            .name("rkl-x11-tracker".into())
            .spawn(move || run(&conn, &tx, &stop))
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

/// Block until the X socket is readable or `timeout` passes. `false` on timeout.
fn wait_readable(fd: std::os::fd::RawFd, timeout: Duration) -> bool {
    let mut pfd = libc::pollfd { fd, events: libc::POLLIN, revents: 0 };
    let ms = libc::c_int::try_from(timeout.as_millis()).unwrap_or(libc::c_int::MAX);
    // SAFETY: `pfd` is a valid, exclusively borrowed pollfd array of length 1 for the call.
    let n = unsafe { libc::poll(&raw mut pfd, 1, ms) };
    n > 0
}

fn run(conn: &XConn, tx: &Sender<WindowEvent>, stop: &AtomicBool) {
    let fd = conn.conn.stream().as_raw_fd();
    let mut state: Option<Tracked> = None;
    let mut dirty = true;
    let mut last_scan = Instant::now();
    let watched = |game: Option<Window>| Watched {
        root: conn.root,
        root_atoms: [conn.atoms._NET_ACTIVE_WINDOW, conn.atoms._NET_CLIENT_LIST],
        game,
        title_atoms: [conn.atoms._NET_WM_NAME, AtomEnum::WM_NAME.into()],
    };
    loop {
        let w = watched(state.map(|s| s.window));
        loop {
            match conn.conn.poll_for_event() {
                Ok(Some(ev)) => dirty |= w.relevant(&ev),
                Ok(None) => break,
                Err(e) => {
                    tracing::warn!(error = %e, "x11 tracker connection lost");
                    let _ = tx.send(WindowEvent::Lost);
                    return;
                }
            }
        }
        if stop.load(Ordering::SeqCst) {
            return;
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
            }
            for ev in diff(state, cur.clone()) {
                if tx.send(ev).is_err() {
                    return;
                }
            }
            state = cur.map(|(c, _)| c);
        }
        if conn.conn.flush().is_err() {
            let _ = tx.send(WindowEvent::Lost);
            return;
        }
        let left = RESCAN.saturating_sub(last_scan.elapsed());
        wait_readable(fd, left.max(Duration::from_millis(1)));
    }
}

/// Which events should trigger a rescan.
#[derive(Debug, Clone, Copy)]
struct Watched {
    root: Window,
    /// `_NET_ACTIVE_WINDOW`, `_NET_CLIENT_LIST` on the root.
    root_atoms: [Atom; 2],
    game: Option<Window>,
    /// `_NET_WM_NAME`, `WM_NAME` on the game window.
    title_atoms: [Atom; 2],
}

impl Watched {
    /// PropertyNotify only for the atoms we read (not e.g. `_NET_WM_USER_TIME`, which changes on
    /// every keypress in the game); structure changes always.
    fn relevant(&self, ev: &Event) -> bool {
        match ev {
            Event::PropertyNotify(p) if p.window == self.root => self.root_atoms.contains(&p.atom),
            Event::PropertyNotify(p) => Some(p.window) == self.game && self.title_atoms.contains(&p.atom),
            Event::ConfigureNotify(_)
            | Event::MapNotify(_)
            | Event::UnmapNotify(_)
            | Event::DestroyNotify(_)
            | Event::ReparentNotify(_) => true,
            _ => false,
        }
    }
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
    fn only_watched_properties_trigger_rescans() {
        use x11rb::protocol::xproto::{MapNotifyEvent, PropertyNotifyEvent};
        const ROOT: Window = 1;
        const GAME: Window = 7;
        let (active, clients, net_name, wm_name, user_time) = (10, 11, 12, 39, 99);
        let w =
            Watched { root: ROOT, root_atoms: [active, clients], game: Some(GAME), title_atoms: [net_name, wm_name] };
        let prop = |window, atom| Event::PropertyNotify(PropertyNotifyEvent { window, atom, ..Default::default() });
        assert!(w.relevant(&prop(ROOT, active)));
        assert!(w.relevant(&prop(ROOT, clients)));
        assert!(!w.relevant(&prop(ROOT, user_time)));
        assert!(w.relevant(&prop(GAME, net_name)));
        assert!(w.relevant(&prop(GAME, wm_name)));
        assert!(!w.relevant(&prop(GAME, user_time)), "_NET_WM_USER_TIME is ignored");
        assert!(!w.relevant(&prop(3, net_name)), "other windows' titles are ignored");
        assert!(!Watched { game: None, ..w }.relevant(&prop(GAME, net_name)));
        assert!(w.relevant(&Event::MapNotify(MapNotifyEvent::default())));
        assert!(!w.relevant(&Event::KeymapNotify(x11rb::protocol::xproto::KeymapNotifyEvent::default())));
    }

    #[test]
    fn wait_readable_times_out_and_wakes() {
        use std::io::Write as _;
        let (mut a, b) = std::os::unix::net::UnixStream::pair().unwrap();
        let start = Instant::now();
        assert!(!wait_readable(b.as_raw_fd(), Duration::from_millis(30)));
        assert!(start.elapsed() >= Duration::from_millis(25));
        a.write_all(b"x").unwrap();
        assert!(wait_readable(b.as_raw_fd(), Duration::from_secs(5)));
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
