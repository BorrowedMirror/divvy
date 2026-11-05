//! Thin x11rb helpers shared by the tracker and the overlay styler.
use x11rb::connection::Connection;
use x11rb::protocol::xproto::{AtomEnum, ConnectionExt as _, MapState, Window};
use x11rb::rust_connection::RustConnection;

use crate::{GAME_WINDOW_TITLE, PlatformError, Rect, WindowInfo};

x11rb::atom_manager! {
    pub(crate) Atoms: AtomsCookie {
        _NET_ACTIVE_WINDOW,
        _NET_CLIENT_LIST,
        _NET_WM_NAME,
        _NET_WM_STATE,
        _NET_WM_STATE_ABOVE,
        _NET_WM_STATE_SKIP_TASKBAR,
        _NET_WM_STATE_SKIP_PAGER,
        _NET_WM_STATE_STICKY,
        UTF8_STRING,
    }
}

pub(crate) fn x_err(e: impl std::fmt::Display) -> PlatformError {
    PlatformError::Backend(format!("x11: {e}"))
}

/// An X connection plus the atoms we need.
pub(crate) struct XConn {
    pub(crate) conn: RustConnection,
    pub(crate) root: Window,
    pub(crate) atoms: Atoms,
}

impl std::fmt::Debug for XConn {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("XConn").field("root", &self.root).finish_non_exhaustive()
    }
}

impl XConn {
    pub(crate) fn connect(display: Option<&str>) -> Result<Self, PlatformError> {
        let (conn, screen) = x11rb::connect(display).map_err(x_err)?;
        let root = conn.setup().roots[screen].root;
        let atoms = Atoms::new(&conn).map_err(x_err)?.reply().map_err(x_err)?;
        Ok(Self { conn, root, atoms })
    }

    pub(crate) fn has_extension(&self, name: &str) -> bool {
        self.conn.query_extension(name.as_bytes()).ok().and_then(|c| c.reply().ok()).is_some_and(|r| r.present)
    }

    /// `_NET_WM_NAME` (UTF-8), falling back to ICCCM `WM_NAME`.
    pub(crate) fn title(&self, w: Window) -> Option<String> {
        let get = |prop: u32, ty: u32| {
            let reply = self.conn.get_property(false, w, prop, ty, 0, 1024).ok()?.reply().ok()?;
            (!reply.value.is_empty()).then(|| String::from_utf8_lossy(&reply.value).into_owned())
        };
        get(self.atoms._NET_WM_NAME, self.atoms.UTF8_STRING)
            .or_else(|| get(AtomEnum::WM_NAME.into(), AtomEnum::STRING.into()))
    }

    fn is_game(&self, w: Window) -> bool {
        self.title(w).as_deref() == Some(GAME_WINDOW_TITLE)
    }

    /// Find the game window: EWMH client list first, then a shallow tree walk (no WM, or a WM
    /// without `_NET_CLIENT_LIST`; depth 2 covers reparenting frames).
    pub(crate) fn find_game(&self) -> Option<Window> {
        let clients = self
            .conn
            .get_property(false, self.root, self.atoms._NET_CLIENT_LIST, AtomEnum::WINDOW, 0, 4096)
            .ok()
            .and_then(|c| c.reply().ok())
            .and_then(|r| r.value32().map(Iterator::collect::<Vec<_>>))
            .unwrap_or_default();
        if let Some(w) = clients.into_iter().find(|w| self.is_game(*w)) {
            return Some(w);
        }
        self.walk(self.root, 2)
    }

    fn walk(&self, parent: Window, depth: u8) -> Option<Window> {
        let tree = self.conn.query_tree(parent).ok()?.reply().ok()?;
        for child in tree.children.iter().rev() {
            let viewable = self
                .conn
                .get_window_attributes(*child)
                .ok()
                .and_then(|c| c.reply().ok())
                .is_some_and(|a| a.map_state == MapState::VIEWABLE);
            if !viewable {
                continue;
            }
            if self.is_game(*child) {
                return Some(*child);
            }
            if depth > 1
                && let Some(w) = self.walk(*child, depth - 1)
            {
                return Some(w);
            }
        }
        None
    }

    /// `_NET_ACTIVE_WINDOW`, falling back to the input focus (no EWMH WM).
    pub(crate) fn active(&self) -> Option<Window> {
        let ewmh = self
            .conn
            .get_property(false, self.root, self.atoms._NET_ACTIVE_WINDOW, AtomEnum::WINDOW, 0, 1)
            .ok()
            .and_then(|c| c.reply().ok())
            .and_then(|r| r.value32().and_then(|mut v| v.next()))
            .filter(|w| *w != 0);
        ewmh.or_else(|| self.conn.get_input_focus().ok()?.reply().ok().map(|r| r.focus).filter(|w| *w > 1))
    }

    /// Whether `w` (or a descendant of it) is focused.
    pub(crate) fn is_focused(&self, w: Window) -> bool {
        let mut cur = self.active();
        for _ in 0..8 {
            match cur {
                Some(c) if c == w => return true,
                Some(c) if c != self.root && c != 0 => {
                    cur = self.conn.query_tree(c).ok().and_then(|q| q.reply().ok()).map(|r| r.parent);
                }
                _ => return false,
            }
        }
        false
    }

    /// Window geometry in root coordinates.
    pub(crate) fn rect(&self, w: Window) -> Option<Rect> {
        let geo = self.conn.get_geometry(w).ok()?.reply().ok()?;
        let pos = self.conn.translate_coordinates(w, self.root, 0, 0).ok()?.reply().ok()?;
        Some(Rect { x: pos.dst_x.into(), y: pos.dst_y.into(), width: geo.width.into(), height: geo.height.into() })
    }

    pub(crate) fn info(&self, w: Window) -> Option<WindowInfo> {
        Some(WindowInfo {
            title: self.title(w)?,
            rect: self.rect(w)?,
            focused: self.is_focused(w),
            native_id: w.into(),
        })
    }
}
