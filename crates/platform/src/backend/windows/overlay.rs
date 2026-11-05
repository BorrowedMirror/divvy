//! Overlay styling on a raw HWND.
//!
//! `prepare` adds `WS_EX_NOACTIVATE | WS_EX_TOOLWINDOW | WS_EX_TOPMOST` so showing or clicking the
//! overlay never steals focus from the game and it has no taskbar button. Click-through adds
//! `WS_EX_LAYERED | WS_EX_TRANSPARENT` (layered with full opacity, as required for transparent
//! hit-testing). All calls are safe from any thread of the owning process.
use std::ffi::c_void;
use std::sync::atomic::{AtomicBool, Ordering};

use windows::Win32::Foundation::{COLORREF, HWND};
use windows::Win32::UI::WindowsAndMessaging::{
    GWL_EXSTYLE, GetWindowLongPtrW, HWND_TOPMOST, IsWindow, LWA_ALPHA, SW_HIDE, SW_SHOWNOACTIVATE, SWP_FRAMECHANGED,
    SWP_NOACTIVATE, SWP_NOMOVE, SWP_NOSIZE, SWP_SHOWWINDOW, SetLayeredWindowAttributes, SetWindowLongPtrW,
    SetWindowPos, ShowWindow, WS_EX_LAYERED, WS_EX_NOACTIVATE, WS_EX_TOOLWINDOW, WS_EX_TOPMOST, WS_EX_TRANSPARENT,
};

use super::logic;
use crate::{OverlayWindow, PlatformError, Rect};

// The pure style logic uses its own constants; pin them to the `windows` crate's.
const _: () = {
    assert!(logic::WS_EX_TOPMOST == WS_EX_TOPMOST.0);
    assert!(logic::WS_EX_TRANSPARENT == WS_EX_TRANSPARENT.0);
    assert!(logic::WS_EX_TOOLWINDOW == WS_EX_TOOLWINDOW.0);
    assert!(logic::WS_EX_LAYERED == WS_EX_LAYERED.0);
    assert!(logic::WS_EX_NOACTIVATE == WS_EX_NOACTIVATE.0);
};

#[derive(Debug)]
pub(super) struct Win32Overlay {
    /// HWND stored as an integer so the struct is `Send + Sync`; HWNDs are process-global handles.
    hwnd: isize,
    added_layered: AtomicBool,
}

impl Win32Overlay {
    pub(super) fn new(hwnd: isize) -> Self {
        Self { hwnd, added_layered: AtomicBool::new(false) }
    }

    fn hwnd(&self) -> Result<HWND, PlatformError> {
        let hwnd = HWND(self.hwnd as *mut c_void);
        // SAFETY: IsWindow validates any value.
        if unsafe { IsWindow(Some(hwnd)) }.as_bool() {
            Ok(hwnd)
        } else {
            Err(PlatformError::Backend(format!("overlay HWND {:#x} is not a window", self.hwnd)))
        }
    }

    fn ex_style(hwnd: HWND) -> u32 {
        // SAFETY: `hwnd` was validated; reading GWL_EXSTYLE has no side effects. The value is a
        // 32-bit style mask stored in a LONG_PTR.
        unsafe { GetWindowLongPtrW(hwnd, GWL_EXSTYLE) as u32 }
    }

    fn set_ex_style(hwnd: HWND, style: u32) -> Result<(), PlatformError> {
        // SAFETY: `hwnd` is a window of this process; GWL_EXSTYLE takes a style mask.
        unsafe { SetWindowLongPtrW(hwnd, GWL_EXSTYLE, style as isize) };
        // SAFETY: SWP_FRAMECHANGED applies the new styles without moving, sizing or activating.
        unsafe {
            SetWindowPos(
                hwnd,
                Some(HWND_TOPMOST),
                0,
                0,
                0,
                0,
                SWP_NOMOVE | SWP_NOSIZE | SWP_NOACTIVATE | SWP_FRAMECHANGED,
            )
        }
        .map_err(|e| PlatformError::Backend(format!("SetWindowPos: {e}")))
    }
}

impl OverlayWindow for Win32Overlay {
    fn prepare(&self) -> Result<(), PlatformError> {
        let hwnd = self.hwnd()?;
        Self::set_ex_style(hwnd, logic::prepared_ex_style(Self::ex_style(hwnd)))
    }

    fn show_at(&self, rect: Rect) -> Result<(), PlatformError> {
        let hwnd = self.hwnd()?;
        let w = i32::try_from(rect.width).unwrap_or(i32::MAX);
        let h = i32::try_from(rect.height).unwrap_or(i32::MAX);
        // SAFETY: valid HWND; HWND_TOPMOST + SWP_NOACTIVATE keeps the game focused.
        unsafe { SetWindowPos(hwnd, Some(HWND_TOPMOST), rect.x, rect.y, w, h, SWP_NOACTIVATE | SWP_SHOWWINDOW) }
            .map_err(|e| PlatformError::Backend(format!("SetWindowPos: {e}")))?;
        // SAFETY: valid HWND. The return value is the previous visibility, not an error.
        let _ = unsafe { ShowWindow(hwnd, SW_SHOWNOACTIVATE) };
        Ok(())
    }

    fn hide(&self) -> Result<(), PlatformError> {
        let hwnd = self.hwnd()?;
        // SAFETY: valid HWND. The return value is the previous visibility, not an error.
        let _ = unsafe { ShowWindow(hwnd, SW_HIDE) };
        Ok(())
    }

    fn set_click_through(&self, enabled: bool) -> Result<(), PlatformError> {
        let hwnd = self.hwnd()?;
        let current = Self::ex_style(hwnd);
        let we_added = self.added_layered.load(Ordering::SeqCst);
        let (style, now_added) = logic::click_through_ex_style(current, enabled, we_added);
        Self::set_ex_style(hwnd, style)?;
        self.added_layered.store(now_added, Ordering::SeqCst);
        if enabled && now_added && !we_added {
            // A freshly layered window is invisible until its attributes are set: full opacity.
            // Not done when the owner made it layered (it may use UpdateLayeredWindow).
            // SAFETY: valid HWND that now has WS_EX_LAYERED.
            unsafe { SetLayeredWindowAttributes(hwnd, COLORREF(0), 255, LWA_ALPHA) }
                .map_err(|e| PlatformError::Backend(format!("SetLayeredWindowAttributes: {e}")))?;
        }
        Ok(())
    }
}
