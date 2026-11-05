//! System clipboard via `arboard`. A fresh handle per call: the Win32 clipboard is opened and
//! closed around each operation anyway, and this keeps the type trivially `Send + Sync`.
use crate::{Clipboard, PlatformError};

#[derive(Debug)]
pub(super) struct ArboardClipboard;

fn backend(e: arboard::Error) -> PlatformError {
    PlatformError::Backend(format!("clipboard: {e}"))
}

fn open() -> Result<arboard::Clipboard, PlatformError> {
    arboard::Clipboard::new().map_err(backend)
}

impl Clipboard for ArboardClipboard {
    fn get_text(&self) -> Result<Option<String>, PlatformError> {
        match open()?.get_text() {
            Ok(text) => Ok(Some(text)),
            Err(arboard::Error::ContentNotAvailable) => Ok(None),
            Err(e) => Err(backend(e)),
        }
    }

    fn set_text(&self, text: &str) -> Result<(), PlatformError> {
        open()?.set_text(text).map_err(backend)
    }

    fn clear(&self) -> Result<(), PlatformError> {
        open()?.clear().map_err(backend)
    }

    fn has_non_text(&self) -> bool {
        open().is_ok_and(|mut c| c.get_image().is_ok())
    }
}
