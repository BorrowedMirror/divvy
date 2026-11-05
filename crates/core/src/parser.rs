//! Parser for PoE2 clipboard item text (Ctrl+C and Ctrl+Alt+C formats).
//!
//! Sections are separated by lines of `--------`. The first section is the header
//! (`Item Class:`, `Rarity:`, name lines).
use crate::{error::ParseError, item::ParsedItem};

/// Section separator used in clipboard text.
pub const SEPARATOR: &str = "--------";

/// Parse clipboard text into a [`ParsedItem`]. Accepts `\r\n` or `\n`.
pub fn parse_item(_text: &str) -> Result<ParsedItem, ParseError> {
    todo!("workstream A: parser for every item class")
}

/// Cheap check used by the clipboard poller to decide whether text looks like an item.
pub fn looks_like_item(text: &str) -> bool {
    text.trim_start().starts_with("Item Class:")
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn smoke_looks_like_item() {
        assert!(looks_like_item("Item Class: Stackable Currency\nRarity: Currency\nDivine Orb"));
        assert!(!looks_like_item("hello"));
    }
}
