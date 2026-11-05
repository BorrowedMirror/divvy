//! Runs the Windows backend's pure helpers on every OS (the backend module itself only compiles on
//! Windows). The helper files name crate-root items (`crate::Rect`, ...), so they are re-exported
//! here at this test crate's root.
pub use kalandra_platform::{
    Capabilities, Chord, GAME_WINDOW_TITLE, KeyModifier, PlatformError, Rect, WindowEvent, WindowInfo,
};

#[allow(dead_code)]
#[path = "../src/backend/windows/logic.rs"]
mod logic;

// Needs `global-hotkey`, which is only a dependency with the `native` feature.
#[cfg(feature = "native")]
#[allow(dead_code)]
#[path = "../src/backend/windows/ghk_map.rs"]
mod ghk_map;
