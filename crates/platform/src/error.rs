#[derive(Debug, thiserror::Error)]
pub enum PlatformError {
    #[error("capability `{0}` is not supported on this backend")]
    Unsupported(&'static str),
    #[error("permission `{0}` has not been granted")]
    PermissionDenied(&'static str),
    #[error("invalid key chord `{0}`")]
    InvalidChord(String),
    #[error("hotkey already registered by another application: {0}")]
    HotkeyConflict(String),
    #[error("already started")]
    AlreadyStarted,
    #[error("timed out after {0:?}")]
    Timeout(std::time::Duration),
    #[error("clipboard did not contain item text")]
    NoItemText,
    #[error("backend error: {0}")]
    Backend(String),
}
