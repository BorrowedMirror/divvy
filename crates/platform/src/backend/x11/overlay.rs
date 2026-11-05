//! Native overlay styling over an X11 window id: EWMH always-on-top / skip-taskbar / sticky,
//! no input focus (WM_HINTS), and click-through via an empty XFixes input shape.
use raw_window_handle::{RawDisplayHandle, RawWindowHandle};
use x11rb::connection::Connection;
use x11rb::properties::WmHints;
use x11rb::protocol::shape::SK;
use x11rb::protocol::xfixes::ConnectionExt as _;
use x11rb::protocol::xproto::{
    AtomEnum, ClientMessageEvent, ConfigureWindowAux, ConnectionExt as _, EventMask, PropMode, Window,
};
use x11rb::wrapper::ConnectionExt as _;

use super::conn::{XConn, x_err};
use crate::{OverlayWindow, PlatformError, Rect};

/// `_NET_WM_STATE_ADD` from the EWMH spec.
const NET_WM_STATE_ADD: u32 = 1;

#[derive(Debug)]
pub struct X11Overlay {
    conn: XConn,
    window: Window,
}

/// Build an overlay styler for an Xlib/Xcb window. Connects to `$DISPLAY` (the raw display
/// pointer is not needed: window ids are server-global).
pub fn overlay(window: RawWindowHandle, display: RawDisplayHandle) -> Result<Box<dyn OverlayWindow>, PlatformError> {
    let _ = display;
    let id = match window {
        RawWindowHandle::Xlib(h) => {
            u32::try_from(h.window).map_err(|_| PlatformError::Backend("x11 window id out of range".into()))?
        }
        RawWindowHandle::Xcb(h) => h.window.get(),
        _ => return Err(PlatformError::Unsupported("x11 overlay needs an Xlib/Xcb window handle")),
    };
    Ok(Box::new(X11Overlay::new(None, id)?))
}

impl X11Overlay {
    pub fn new(display: Option<&str>, window: u32) -> Result<Self, PlatformError> {
        Ok(Self { conn: XConn::connect(display)?, window })
    }

    fn wm_state_atoms(&self) -> [u32; 4] {
        let a = &self.conn.atoms;
        [a._NET_WM_STATE_ABOVE, a._NET_WM_STATE_SKIP_TASKBAR, a._NET_WM_STATE_SKIP_PAGER, a._NET_WM_STATE_STICKY]
    }
}

impl OverlayWindow for X11Overlay {
    fn prepare(&self) -> Result<(), PlatformError> {
        let c = &self.conn.conn;
        let states = self.wm_state_atoms();
        // Before mapping, the WM reads _NET_WM_STATE from the property...
        c.change_property32(PropMode::REPLACE, self.window, self.conn.atoms._NET_WM_STATE, AtomEnum::ATOM, &states)
            .map_err(x_err)?;
        // ...after mapping it only honours client messages to the root.
        for pair in states.chunks(2) {
            let data = [NET_WM_STATE_ADD, pair[0], pair[1], 1, 0];
            let ev = ClientMessageEvent::new(32, self.window, self.conn.atoms._NET_WM_STATE, data);
            c.send_event(false, self.conn.root, EventMask::SUBSTRUCTURE_REDIRECT | EventMask::SUBSTRUCTURE_NOTIFY, ev)
                .map_err(x_err)?;
        }
        // Never take keyboard focus away from the game.
        let mut hints = WmHints::new();
        hints.input = Some(false);
        hints.set(c, self.window).map_err(x_err)?.check().map_err(x_err)
    }

    fn show_at(&self, rect: Rect) -> Result<(), PlatformError> {
        let c = &self.conn.conn;
        let aux = ConfigureWindowAux::new().x(rect.x).y(rect.y).width(rect.width).height(rect.height);
        c.configure_window(self.window, &aux).map_err(x_err)?;
        c.map_window(self.window).map_err(x_err)?.check().map_err(x_err)
    }

    fn hide(&self) -> Result<(), PlatformError> {
        self.conn.conn.unmap_window(self.window).map_err(x_err)?.check().map_err(x_err)
    }

    fn raise_above(&self, owner: Option<u64>) -> Result<(), PlatformError> {
        let c = &self.conn.conn;
        let owner = owner.and_then(|o| u32::try_from(o).ok()).filter(|o| *o != 0 && *o != self.window);
        if let Some(owner) = owner {
            // Transients are kept above their parent, including a focused fullscreen parent.
            c.change_property32(PropMode::REPLACE, self.window, AtomEnum::WM_TRANSIENT_FOR, AtomEnum::WINDOW, &[owner])
                .map_err(x_err)?;
        }
        // EWMH restack request (source 2 = pager, so WMs honour it), sibling = owner, Above.
        let data = [2, owner.unwrap_or(x11rb::NONE), 0, 0, 0];
        let ev = ClientMessageEvent::new(32, self.window, self.conn.atoms._NET_RESTACK_WINDOW, data);
        c.send_event(false, self.conn.root, EventMask::SUBSTRUCTURE_REDIRECT | EventMask::SUBSTRUCTURE_NOTIFY, ev)
            .map_err(x_err)?;
        // Fallback for WMs without _NET_RESTACK_WINDOW.
        let aux = ConfigureWindowAux::new().stack_mode(x11rb::protocol::xproto::StackMode::ABOVE);
        c.configure_window(self.window, &aux).map_err(x_err)?.check().map_err(x_err)
    }

    fn activate(&self) -> Result<(), PlatformError> {
        let c = &self.conn.conn;
        let mut hints = WmHints::new();
        hints.input = Some(true);
        hints.set(c, self.window).map_err(x_err)?;
        // Source 2 (pager) bypasses focus-stealing prevention; timestamp 0 = current time.
        let data = [2, 0, 0, 0, 0];
        let ev = ClientMessageEvent::new(32, self.window, self.conn.atoms._NET_ACTIVE_WINDOW, data);
        c.send_event(false, self.conn.root, EventMask::SUBSTRUCTURE_REDIRECT | EventMask::SUBSTRUCTURE_NOTIFY, ev)
            .map_err(x_err)?
            .check()
            .map_err(x_err)
    }

    fn set_click_through(&self, enabled: bool) -> Result<(), PlatformError> {
        let c = &self.conn.conn;
        c.xfixes_query_version(5, 0).map_err(x_err)?.reply().map_err(x_err)?;
        if enabled {
            let region = c.generate_id().map_err(x_err)?;
            c.xfixes_create_region(region, &[]).map_err(x_err)?;
            c.xfixes_set_window_shape_region(self.window, SK::INPUT, 0, 0, region).map_err(x_err)?;
            c.xfixes_destroy_region(region).map_err(x_err)?.check().map_err(x_err)
        } else {
            c.xfixes_set_window_shape_region(self.window, SK::INPUT, 0, 0, x11rb::NONE)
                .map_err(x_err)?
                .check()
                .map_err(x_err)
        }
    }
}

/// Give keyboard focus back to another client (the game) with a pager-source
/// `_NET_ACTIVE_WINDOW` request, without touching that client's hints.
pub fn activate_window(window: u64) -> Result<(), PlatformError> {
    let window = u32::try_from(window).map_err(|_| PlatformError::Backend("x11 window id out of range".into()))?;
    let conn = XConn::connect(None)?;
    let ev = ClientMessageEvent::new(32, window, conn.atoms._NET_ACTIVE_WINDOW, [2, 0, 0, 0, 0]);
    conn.conn
        .send_event(false, conn.root, EventMask::SUBSTRUCTURE_REDIRECT | EventMask::SUBSTRUCTURE_NOTIFY, ev)
        .map_err(x_err)?
        .check()
        .map_err(x_err)
}
