//! Runs the macOS backend's pure helpers (window-list conversion, permission -> capability
//! mapping, key mapping) on every OS. The library only compiles them on macOS with `native`,
//! so they are included here by path; `crate::types` is shimmed to the public types.

mod types {
    pub use rkl_platform::types::*;
}

// Some helpers are only used by the native macOS code, not by these tests.
#[allow(dead_code)]
#[path = "../src/backend/macos/model.rs"]
mod model;

#[allow(dead_code)]
#[path = "../src/backend/macos/keymap.rs"]
mod keymap;
