//! Game window tracking.
//!
//! State always comes from `CGWindowListCopyWindowInfo` (front-to-back on-screen list, no
//! permission needed for bounds/pids; titles need Screen Recording). When Accessibility is
//! granted, an `AXObserver` on the game's pid wakes the tracker thread's CFRunLoop on
//! move/resize/focus notifications so events are immediate; otherwise the thread polls at a
//! low, fixed rate. The thread only pushes into a channel.
use std::ffi::c_void;
use std::ptr::NonNull;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::mpsc::{Receiver, SyncSender, TrySendError, sync_channel};
use std::time::Duration;

use objc2_application_services::{AXError, AXObserver, AXUIElement};
use objc2_core_foundation::{
    CFArray, CFDictionary, CFNumber, CFRetained, CFRunLoop, CFRunLoopRunResult, CFRunLoopSource, CFString, CFType,
    kCFRunLoopDefaultMode,
};
use objc2_core_graphics::{
    CGWindowListCopyWindowInfo, CGWindowListOption, kCGNullWindowID, kCGWindowBounds, kCGWindowLayer, kCGWindowName,
    kCGWindowNumber, kCGWindowOwnerName, kCGWindowOwnerPID,
};

use super::model::{self, RawWindow};
use super::permissions;
use crate::{PlatformError, WindowEvent, WindowInfo, WindowTracker};

/// Poll interval while the game is visible but no AX observer is attached.
const POLL_TRACKING: Duration = Duration::from_millis(250);
/// Poll interval while the game is not running / not on screen.
const POLL_IDLE: Duration = Duration::from_secs(1);
/// Safety-net re-snapshot interval while an AX observer is attached.
const AX_SLICE: Duration = Duration::from_secs(1);
const CHANNEL_CAPACITY: usize = 256;

/// AX notifications that can change the game window's rect, focus or existence.
const AX_NOTIFICATIONS: &[&str] = &[
    "AXWindowMoved",
    "AXWindowResized",
    "AXFocusedWindowChanged",
    "AXMainWindowChanged",
    "AXApplicationActivated",
    "AXApplicationDeactivated",
    "AXApplicationHidden",
    "AXApplicationShown",
    "AXWindowMiniaturized",
    "AXWindowDeminiaturized",
    "AXUIElementDestroyed",
];

#[derive(Debug, Default)]
pub(super) struct MacWindowTracker {
    subscribed: AtomicBool,
}

impl MacWindowTracker {
    pub(super) fn new() -> Self {
        Self::default()
    }
}

impl WindowTracker for MacWindowTracker {
    fn game_window(&self) -> Option<WindowInfo> {
        model::find_game(&snapshot())
    }

    fn is_game_focused(&self) -> bool {
        self.game_window().is_some_and(|w| w.focused)
    }

    fn subscribe(&self) -> Result<Receiver<WindowEvent>, PlatformError> {
        if self.subscribed.swap(true, Ordering::SeqCst) {
            return Err(PlatformError::AlreadyStarted);
        }
        let (tx, rx) = sync_channel(CHANNEL_CAPACITY);
        std::thread::Builder::new()
            .name("kalandra-macos-window".into())
            .spawn(move || track(&tx))
            .map_err(|e| PlatformError::Backend(format!("spawn window tracker: {e}")))?;
        Ok(rx)
    }
}

/// Tracker thread body. Returns when the receiver is dropped.
fn track(tx: &SyncSender<WindowEvent>) {
    let mut prev: Option<WindowInfo> = None;
    let mut watch: Option<AxWatch> = None;
    loop {
        let windows = snapshot();
        let pid = model::find_game_raw(&windows).map(|w| w.pid);
        let next = model::find_game(&windows);
        for event in model::diff(prev.as_ref(), next.as_ref()) {
            match tx.try_send(event) {
                Ok(()) => {}
                Err(TrySendError::Full(_)) => tracing::warn!("window event channel full; dropping event"),
                Err(TrySendError::Disconnected(_)) => return,
            }
        }
        prev = next;

        // (Re)attach the AX observer when the game process changes or AX becomes available.
        if watch.as_ref().map(|w| w.pid) != pid {
            watch = pid.filter(|_| permissions::accessibility()).and_then(AxWatch::new);
        }

        match &watch {
            Some(w) => w.wait(AX_SLICE),
            None if prev.is_some() => std::thread::sleep(POLL_TRACKING),
            None => std::thread::sleep(POLL_IDLE),
        }
    }
}

/// On-screen windows, front to back, reduced to plain values.
pub(super) fn snapshot() -> Vec<RawWindow> {
    let options = CGWindowListOption::OptionOnScreenOnly | CGWindowListOption::ExcludeDesktopElements;
    let Some(list) = CGWindowListCopyWindowInfo(options, kCGNullWindowID) else {
        return Vec::new();
    };
    // SAFETY: CGWindowListCopyWindowInfo is documented to return a CFArray of CFDictionary.
    let list: &CFArray<CFDictionary> = unsafe { list.cast_unchecked() };
    list.iter().map(|dict| raw_window(&dict)).collect()
}

fn raw_window(dict: &CFDictionary) -> RawWindow {
    // SAFETY: window info dictionaries are keyed by CFString constants; values are CFTypes we
    // downcast checked below.
    let dict: &CFDictionary<CFString, CFType> = unsafe { dict.cast_unchecked() };
    // SAFETY: immutable CFString constants exported by CoreGraphics, valid for the life of
    // the process.
    let (k_bounds, k_number, k_pid, k_layer, k_owner, k_name) = unsafe {
        (kCGWindowBounds, kCGWindowNumber, kCGWindowOwnerPID, kCGWindowLayer, kCGWindowOwnerName, kCGWindowName)
    };
    let bounds = dict.get(k_bounds).and_then(|v| v.downcast::<CFDictionary>().ok());
    let (x, y, width, height) = bounds.map_or((0.0, 0.0, 0.0, 0.0), |b| {
        // SAFETY: kCGWindowBounds is a CGRect dictionary representation: CFString keys
        // (X, Y, Width, Height) to CFNumbers.
        let b: &CFDictionary<CFString, CFType> = unsafe { b.cast_unchecked() };
        let f = |k: &'static str| number(b.get(&CFString::from_static_str(k))).and_then(|n| n.as_f64()).unwrap_or(0.0);
        (f("X"), f("Y"), f("Width"), f("Height"))
    });
    RawWindow {
        number: number(dict.get(k_number)).and_then(|n| n.as_i64()).unwrap_or(0) as u32,
        pid: number(dict.get(k_pid)).and_then(|n| n.as_i32()).unwrap_or(0),
        layer: number(dict.get(k_layer)).and_then(|n| n.as_i64()).unwrap_or(0),
        owner_name: string(dict.get(k_owner)),
        title: string(dict.get(k_name)),
        x,
        y,
        width,
        height,
    }
}

fn number(v: Option<CFRetained<CFType>>) -> Option<CFRetained<CFNumber>> {
    v?.downcast::<CFNumber>().ok()
}

fn string(v: Option<CFRetained<CFType>>) -> Option<String> {
    v?.downcast::<CFString>().ok().map(|s| s.to_string())
}

/// An AXObserver for one pid, attached to the current thread's run loop.
struct AxWatch {
    pid: i32,
    run_loop: CFRetained<CFRunLoop>,
    source: CFRetained<CFRunLoopSource>,
    // Kept alive for as long as the source is scheduled.
    _observer: CFRetained<AXObserver>,
}

/// The callback only exists to wake `CFRunLoopRunInMode`; the loop re-snapshots afterwards.
unsafe extern "C-unwind" fn ax_callback(
    _observer: NonNull<AXObserver>,
    _element: NonNull<AXUIElement>,
    _notification: NonNull<CFString>,
    _refcon: *mut c_void,
) {
}

impl AxWatch {
    fn new(pid: i32) -> Option<Self> {
        let mut raw: *mut AXObserver = std::ptr::null_mut();
        // SAFETY: `ax_callback` matches AXObserverCallback and does not touch its arguments;
        // `raw` is a valid out-pointer.
        let err = unsafe { AXObserver::create(pid, Some(ax_callback), NonNull::from(&mut raw)) };
        let raw = NonNull::new(raw).filter(|_| err == AXError::Success);
        let Some(raw) = raw else {
            tracing::debug!(?err, pid, "AXObserverCreate failed; falling back to polling");
            return None;
        };
        // SAFETY: AXObserverCreate follows the Create rule: we own the +1 reference.
        let observer = unsafe { CFRetained::from_raw(raw) };
        // SAFETY: creating an application element for any pid is valid; it may simply refer to
        // a process that has exited, in which case notifications fail to register below.
        let app = unsafe { AXUIElement::new_application(pid) };
        let mut registered = 0;
        for name in AX_NOTIFICATIONS {
            let name = CFString::from_static_str(name);
            // SAFETY: observer and element are valid; refcon is null and never dereferenced.
            let err = unsafe { observer.add_notification(&app, &name, std::ptr::null_mut()) };
            if err == AXError::Success {
                registered += 1;
            }
        }
        if registered == 0 {
            tracing::debug!(pid, "no AX notifications registered; falling back to polling");
            return None;
        }
        // SAFETY: observer is a valid AXObserver.
        let source = unsafe { observer.run_loop_source() };
        let run_loop = CFRunLoop::current()?;
        // SAFETY: kCFRunLoopDefaultMode is an immutable constant exported by CoreFoundation.
        run_loop.add_source(Some(&source), unsafe { kCFRunLoopDefaultMode });
        Some(Self { pid, run_loop, source, _observer: observer })
    }

    /// Block until an AX notification arrives or `timeout` passes.
    fn wait(&self, timeout: Duration) {
        // SAFETY: see `new`.
        let mode = unsafe { kCFRunLoopDefaultMode };
        let result = CFRunLoop::run_in_mode(mode, timeout.as_secs_f64(), true);
        if result == CFRunLoopRunResult::Finished {
            // No sources (should not happen while attached): never spin.
            std::thread::sleep(POLL_TRACKING);
        }
    }
}

impl Drop for AxWatch {
    fn drop(&mut self) {
        // SAFETY: see `new`.
        self.run_loop.remove_source(Some(&self.source), unsafe { kCFRunLoopDefaultMode });
    }
}
