//! Game window tracking via WinEvent hooks.
//!
//! `subscribe` spawns one thread that installs out-of-context hooks for
//! `EVENT_SYSTEM_FOREGROUND` and `EVENT_OBJECT_LOCATIONCHANGE` and runs a message loop (required:
//! out-of-context WinEvents are delivered through the installing thread's queue). A thread timer
//! rescans for the game window every second so `Found`/`Lost` are reported. The hook callback only
//! updates thread-local state and pushes events into a channel.
use std::cell::RefCell;
use std::ffi::c_void;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::mpsc::{self, Receiver, Sender};

use windows::Win32::Foundation::{HWND, LPARAM, RECT};
use windows::Win32::Graphics::Dwm::{DWMWA_EXTENDED_FRAME_BOUNDS, DwmGetWindowAttribute};
use windows::Win32::System::Threading::GetCurrentProcessId;
use windows::Win32::UI::Accessibility::{HWINEVENTHOOK, SetWinEventHook, UnhookWinEvent};
use windows::Win32::UI::WindowsAndMessaging::{
    CHILDID_SELF, DispatchMessageW, EVENT_OBJECT_LOCATIONCHANGE, EVENT_SYSTEM_FOREGROUND, EnumWindows, FindWindowW,
    GetForegroundWindow, GetMessageW, GetWindowRect, GetWindowTextW, GetWindowThreadProcessId, IsWindow,
    IsWindowVisible, KillTimer, MSG, OBJID_WINDOW, PostQuitMessage, SetTimer, TranslateMessage, WINEVENT_OUTOFCONTEXT,
    WM_TIMER,
};
use windows::core::{BOOL, HSTRING, PCWSTR};

use super::logic::{TrackerState, rect_from_ltrb, title_matches};
use crate::{GAME_WINDOW_TITLE, PlatformError, Rect, WindowEvent, WindowInfo, WindowTracker};

const RESCAN_MS: u32 = 1000;

#[derive(Debug, Default)]
pub(super) struct Win32WindowTracker {
    subscribed: AtomicBool,
}

impl WindowTracker for Win32WindowTracker {
    fn game_window(&self) -> Option<WindowInfo> {
        find_game_window().and_then(window_info)
    }

    fn is_game_focused(&self) -> bool {
        // SAFETY: no arguments; returns a possibly-null HWND.
        let fg = unsafe { GetForegroundWindow() };
        !fg.is_invalid() && window_title(fg).is_some_and(|t| title_matches(&t))
    }

    fn subscribe(&self) -> Result<Receiver<WindowEvent>, PlatformError> {
        if self.subscribed.swap(true, Ordering::SeqCst) {
            return Err(PlatformError::AlreadyStarted);
        }
        let (tx, rx) = mpsc::channel();
        let (ready_tx, ready_rx) = mpsc::sync_channel(1);
        std::thread::Builder::new()
            .name("rkl-win-tracker".into())
            .spawn(move || hook_thread(tx, ready_tx))
            .map_err(|e| PlatformError::Backend(format!("spawn tracker thread: {e}")))?;
        ready_rx.recv().map_err(|_| PlatformError::Backend("tracker thread exited during setup".into()))??;
        Ok(rx)
    }
}

pub(super) fn hwnd_id(hwnd: HWND) -> u64 {
    hwnd.0 as usize as u64
}

/// Whether `hwnd` belongs to this process (the settings and price windows, hidden helpers).
fn is_own_window(hwnd: HWND) -> bool {
    let mut pid = 0u32;
    // SAFETY: `pid` is a valid out-pointer; any HWND value is accepted (0 for a stale one).
    unsafe { GetWindowThreadProcessId(hwnd, Some(&raw mut pid)) };
    // SAFETY: no arguments.
    pid == unsafe { GetCurrentProcessId() }
}

/// The title of another process's window; `None` for our own windows, which are never the game.
///
/// Our own windows are skipped because `GetWindowTextW` on a window of this process that
/// another thread owns sends it `WM_GETTEXT` and waits for the reply. While the game is not
/// running, the tracker thread looks at every visible window each second, so it would block on
/// the UI thread, and the UI thread waits for the tracker's first scan during startup.
fn window_title(hwnd: HWND) -> Option<String> {
    if is_own_window(hwnd) {
        return None;
    }
    let mut buf = [0u16; 256];
    // SAFETY: `buf` is a valid writable buffer; the wrapper passes its length. Reading another
    // process's window title does not send messages, so this cannot re-enter our hook callback.
    let len = unsafe { GetWindowTextW(hwnd, &mut buf) };
    let len = usize::try_from(len).ok().filter(|&l| l > 0)?;
    Some(String::from_utf16_lossy(&buf[..len.min(buf.len())]))
}

/// Visible window bounds: DWM extended frame bounds (excludes the invisible resize border on
/// Windows 10+), falling back to `GetWindowRect`.
fn window_rect(hwnd: HWND) -> Option<Rect> {
    let mut r = RECT::default();
    // SAFETY: `r` is a valid RECT and we pass its exact size for DWMWA_EXTENDED_FRAME_BOUNDS.
    let dwm = unsafe {
        DwmGetWindowAttribute(
            hwnd,
            DWMWA_EXTENDED_FRAME_BOUNDS,
            std::ptr::from_mut(&mut r).cast::<c_void>(),
            size_of::<RECT>() as u32,
        )
    };
    if dwm.is_err() {
        // SAFETY: `r` is a valid out-pointer for the call.
        unsafe { GetWindowRect(hwnd, &mut r) }.ok()?;
    }
    Some(rect_from_ltrb(r.left, r.top, r.right, r.bottom))
}

fn window_info(hwnd: HWND) -> Option<WindowInfo> {
    // SAFETY: IsWindow accepts any value and just validates it.
    if !unsafe { IsWindow(Some(hwnd)) }.as_bool() {
        return None;
    }
    // SAFETY: no arguments.
    let focused = unsafe { GetForegroundWindow() } == hwnd;
    Some(WindowInfo {
        title: window_title(hwnd).unwrap_or_else(|| GAME_WINDOW_TITLE.to_owned()),
        rect: window_rect(hwnd)?,
        focused,
        native_id: hwnd_id(hwnd),
    })
}

pub(super) fn find_game_window() -> Option<HWND> {
    let title = HSTRING::from(GAME_WINDOW_TITLE);
    // SAFETY: a null class name and a NUL-terminated title that outlives the call.
    if let Ok(hwnd) = unsafe { FindWindowW(PCWSTR::null(), &title) }
        && !hwnd.is_invalid()
    {
        return Some(hwnd);
    }
    // Fallback for titles with stray whitespace/padding: enumerate top-level windows.
    let mut found: Option<HWND> = None;
    // SAFETY: `enum_proc` matches WNDENUMPROC and only dereferences `lparam` as the
    // `&mut Option<HWND>` passed here, which outlives the synchronous EnumWindows call. EnumWindows
    // returns an error when the callback stops early, which is how we signal "found".
    let _ = unsafe { EnumWindows(Some(enum_proc), LPARAM(std::ptr::from_mut(&mut found) as isize)) };
    found
}

unsafe extern "system" fn enum_proc(hwnd: HWND, lparam: LPARAM) -> BOOL {
    // SAFETY: IsWindowVisible validates any HWND.
    let visible = unsafe { IsWindowVisible(hwnd) }.as_bool();
    if visible && window_title(hwnd).is_some_and(|t| title_matches(&t)) {
        // SAFETY: `lparam` is the `&mut Option<HWND>` from `find_game_window` (see there).
        let out = unsafe { &mut *(lparam.0 as *mut Option<HWND>) };
        *out = Some(hwnd);
        return BOOL(0);
    }
    BOOL(1)
}

struct HookCtx {
    tx: Sender<WindowEvent>,
    state: TrackerState,
}

thread_local! {
    static CTX: RefCell<Option<HookCtx>> = const { RefCell::new(None) };
}

/// Push events; on a dropped receiver, stop the hook thread.
fn emit(events: impl IntoIterator<Item = WindowEvent>) {
    let mut closed = false;
    CTX.with_borrow(|ctx| {
        if let Some(ctx) = ctx {
            for ev in events {
                if ctx.tx.send(ev).is_err() {
                    closed = true;
                    break;
                }
            }
        }
    });
    if closed {
        // SAFETY: posts WM_QUIT to the current (hook) thread's queue.
        unsafe { PostQuitMessage(0) };
    }
}

fn rescan() {
    // Query Win32 outside the RefCell borrow so nothing can observe it mid-update.
    let found = find_game_window().and_then(window_info);
    let events = CTX.with_borrow_mut(|ctx| ctx.as_mut().map(|c| c.state.on_scan(found)).unwrap_or_default());
    emit(events);
}

fn game_id() -> Option<u64> {
    CTX.with_borrow(|ctx| ctx.as_ref().and_then(|c| c.state.game_id()))
}

unsafe extern "system" fn win_event_proc(
    _hook: HWINEVENTHOOK,
    event: u32,
    hwnd: HWND,
    id_object: i32,
    id_child: i32,
    _thread: u32,
    _time: u32,
) {
    match event {
        EVENT_SYSTEM_FOREGROUND => {
            if game_id().is_none() {
                // Untracked: the game may just have been launched/focused; don't wait for the timer.
                rescan();
            } else {
                let id = hwnd_id(hwnd);
                let ev = CTX.with_borrow_mut(|ctx| ctx.as_mut().and_then(|c| c.state.on_foreground(id)));
                emit(ev);
            }
        }
        EVENT_OBJECT_LOCATIONCHANGE
            if id_object == OBJID_WINDOW.0 && id_child == CHILDID_SELF as i32 && game_id() == Some(hwnd_id(hwnd)) =>
        {
            if let Some(rect) = window_rect(hwnd) {
                let id = hwnd_id(hwnd);
                let ev = CTX.with_borrow_mut(|ctx| ctx.as_mut().and_then(|c| c.state.on_moved(id, rect)));
                emit(ev);
            }
        }
        _ => {}
    }
}

fn set_hook(event: u32) -> HWINEVENTHOOK {
    // SAFETY: `win_event_proc` has the WINEVENTPROC signature and lives for the whole program.
    // WINEVENT_OUTOFCONTEXT means no DLL injection: events are queued to this thread and the
    // callback runs inside this thread's GetMessageW.
    unsafe { SetWinEventHook(event, event, None, Some(win_event_proc), 0, 0, WINEVENT_OUTOFCONTEXT) }
}

fn hook_thread(tx: Sender<WindowEvent>, ready: mpsc::SyncSender<Result<(), PlatformError>>) {
    CTX.with_borrow_mut(|ctx| *ctx = Some(HookCtx { tx, state: TrackerState::default() }));
    let hooks = [set_hook(EVENT_SYSTEM_FOREGROUND), set_hook(EVENT_OBJECT_LOCATIONCHANGE)];
    if hooks.iter().any(HWINEVENTHOOK::is_invalid) {
        unhook(&hooks);
        let _ = ready
            .send(Err(PlatformError::Backend(format!("SetWinEventHook failed: {}", std::io::Error::last_os_error()))));
        return;
    }
    // SAFETY: a thread timer (no HWND, no TIMERPROC) just posts WM_TIMER to this thread's queue.
    let timer = unsafe { SetTimer(None, 0, RESCAN_MS, None) };
    // Report ready before the first scan: `subscribe` runs on the UI thread and must not wait
    // on Win32 calls made here.
    let _ = ready.send(Ok(()));
    rescan();

    let mut msg = MSG::default();
    // SAFETY: `msg` is a valid out-pointer. GetMessageW returns 0 on WM_QUIT and -1 on error; both
    // end the loop.
    while unsafe { GetMessageW(&mut msg, None, 0, 0) }.0 > 0 {
        if msg.message == WM_TIMER && msg.hwnd.is_invalid() {
            rescan();
        }
        // SAFETY: `msg` was filled by GetMessageW.
        unsafe {
            let _ = TranslateMessage(&msg);
            DispatchMessageW(&msg);
        }
    }

    if timer != 0 {
        // SAFETY: `timer` was returned by SetTimer on this thread with no HWND.
        let _ = unsafe { KillTimer(None, timer) };
    }
    unhook(&hooks);
    CTX.with_borrow_mut(|ctx| *ctx = None);
}

fn unhook(hooks: &[HWINEVENTHOOK]) {
    for &h in hooks.iter().filter(|h| !h.is_invalid()) {
        // SAFETY: `h` was returned by SetWinEventHook on this thread and not yet unhooked.
        let _ = unsafe { UnhookWinEvent(h) };
    }
}
