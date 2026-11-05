//! TCC permission checks. All calls are cheap, non-blocking preflights unless `request` is used.
use objc2_application_services::{AXIsProcessTrusted, AXIsProcessTrustedWithOptions, kAXTrustedCheckOptionPrompt};
use objc2_core_foundation::{CFBoolean, CFDictionary, CFString};
use objc2_core_graphics::{
    CGPreflightListenEventAccess, CGPreflightScreenCaptureAccess, CGRequestListenEventAccess,
    CGRequestScreenCaptureAccess,
};

use super::model::{PERM_ACCESSIBILITY, PERM_INPUT_MONITORING, PERM_SCREEN_RECORDING, Permissions};

/// Whether this process is a trusted Accessibility client (no prompt).
pub(super) fn accessibility() -> bool {
    // SAFETY: AXIsProcessTrusted takes no arguments and only reads TCC state.
    unsafe { AXIsProcessTrusted() }
}

/// Current permission state without prompting.
pub(super) fn current() -> Permissions {
    Permissions {
        accessibility: accessibility(),
        screen_recording: CGPreflightScreenCaptureAccess(),
        input_monitoring: CGPreflightListenEventAccess(),
    }
}

/// Ask macOS for `kind`. Shows the system prompt the first time and adds the app to the
/// relevant System Settings list; later calls return the current state without UI.
pub(super) fn request(kind: &str) -> bool {
    match kind {
        PERM_ACCESSIBILITY => {
            // SAFETY: kAXTrustedCheckOptionPrompt is an immutable CFString constant exported by
            // HIServices, valid for the life of the process.
            let key: &CFString = unsafe { kAXTrustedCheckOptionPrompt };
            let options = CFDictionary::<CFString, CFBoolean>::from_slices(&[key], &[CFBoolean::new(true)]);
            // SAFETY: `options` is a valid CFDictionary<CFString, CFBoolean>, the documented
            // shape of the options argument; it outlives the call.
            unsafe { AXIsProcessTrustedWithOptions(Some(options.as_opaque())) }
        }
        PERM_SCREEN_RECORDING => CGRequestScreenCaptureAccess(),
        PERM_INPUT_MONITORING => CGRequestListenEventAccess(),
        _ => false,
    }
}
