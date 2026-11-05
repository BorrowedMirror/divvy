//! macos backend. Owned by its Phase 2 workstream (see CONTRACTS.md).
use crate::{Platform, PlatformError};

pub fn create() -> Result<Platform, PlatformError> {
    Err(PlatformError::Unsupported("macos backend not implemented yet"))
}
