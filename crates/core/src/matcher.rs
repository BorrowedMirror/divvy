//! Maps parsed modifiers to trade stat ids.
use crate::{catalog::Catalog, item::ParsedItem};

/// Replace every number in `line` with `#` (keeping `+`/`-` signs as text), matching trade templates.
pub fn templatize(_line: &str) -> String {
    todo!("workstream A")
}

/// Outcome per modifier.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum MatchOutcome {
    Matched { stat_id: String, negated: bool },
    Ambiguous(Vec<String>),
    Unmatched,
}

#[derive(Debug)]
pub struct StatMatcher<'c> {
    catalog: &'c dyn Catalog,
}

impl<'c> StatMatcher<'c> {
    pub fn new(catalog: &'c dyn Catalog) -> Self {
        Self { catalog }
    }

    pub fn catalog(&self) -> &'c dyn Catalog {
        self.catalog
    }

    /// Fill `stat_id` on every modifier that can be matched; returns per-modifier outcomes.
    pub fn match_item(&self, _item: &mut ParsedItem) -> Vec<MatchOutcome> {
        todo!("workstream A")
    }
}

impl std::fmt::Debug for dyn Catalog + '_ {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str("dyn Catalog")
    }
}
