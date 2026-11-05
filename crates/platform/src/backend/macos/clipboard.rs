//! Text clipboard via arboard (NSPasteboard). No permission required.
use crate::{Clipboard, PlatformError};

#[derive(Debug)]
pub(super) struct MacClipboard;

fn open() -> Result<arboard::Clipboard, PlatformError> {
    arboard::Clipboard::new().map_err(|e| PlatformError::Backend(format!("clipboard: {e}")))
}

impl Clipboard for MacClipboard {
    fn get_text(&self) -> Result<Option<String>, PlatformError> {
        match open()?.get_text() {
            Ok(text) => Ok(Some(text)),
            Err(arboard::Error::ContentNotAvailable) => Ok(None),
            Err(e) => Err(PlatformError::Backend(format!("clipboard: {e}"))),
        }
    }

    fn set_text(&self, text: &str) -> Result<(), PlatformError> {
        open()?.set_text(text).map_err(|e| PlatformError::Backend(format!("clipboard: {e}")))
    }

    fn clear(&self) -> Result<(), PlatformError> {
        open()?.clear().map_err(|e| PlatformError::Backend(format!("clipboard: {e}")))
    }
}
