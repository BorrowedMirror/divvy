//! x11 backend. Owned by its Phase 2 workstream (see CONTRACTS.md).
use crate::{Platform, PlatformError};

pub fn create() -> Result<Platform, PlatformError> {
    Err(PlatformError::Unsupported("x11 backend not implemented yet"))
}
