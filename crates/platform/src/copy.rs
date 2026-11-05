//! The user-triggered copy flow: send Ctrl+C (or Ctrl+Alt+C), poll the clipboard, restore.
use std::time::Duration;

use crate::{Clipboard, InputSimulator, PlatformError};

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct CopyOptions {
    pub advanced: bool,
    pub timeout: Duration,
    pub poll_interval: Duration,
    pub restore_clipboard: bool,
}

impl Default for CopyOptions {
    fn default() -> Self {
        Self {
            advanced: true,
            timeout: Duration::from_millis(500),
            poll_interval: Duration::from_millis(15),
            restore_clipboard: true,
        }
    }
}

/// Send the copy chord and return the item text. Clears the clipboard first so a stale
/// item is never returned; restores the prior clipboard text afterwards when asked.
/// `sleep` is injected so tests run instantly.
pub fn copy_item_text(
    _sim: &dyn InputSimulator,
    _clip: &dyn Clipboard,
    _opts: CopyOptions,
    _sleep: &dyn Fn(Duration),
) -> Result<String, PlatformError> {
    todo!("workstream F (platform common)")
}
