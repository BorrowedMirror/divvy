//! Game-data model consumed by the matcher. Implemented by `kalandra-data`; faked in tests.
use serde::{Deserialize, Serialize};

use crate::item::ModKind;

/// One trade stat as exposed by `/api/trade2/data/stats`.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize, specta::Type)]
pub struct StatDef {
    /// e.g. `explicit.stat_3299347043`
    pub id: String,
    /// Template text with `#` placeholders, e.g. `+# to maximum Life`.
    pub text: String,
    /// `explicit`, `implicit`, `rune`, `enchant`, `pseudo`, ...
    pub kind: String,
    /// Alternative templates that map to the same stat (e.g. "reduced" vs "increased").
    #[serde(default)]
    pub aliases: Vec<StatAlias>,
}

/// Alternative wording; `negate` means the values must be multiplied by -1.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize, specta::Type)]
pub struct StatAlias {
    pub text: String,
    pub negate: bool,
}

/// Item base as exposed by `/api/trade2/data/items` plus export data.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize, specta::Type)]
pub struct BaseDef {
    pub name: String,
    /// Trade category, e.g. `armour.helmet`.
    pub category: String,
    pub item_class: String,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize, specta::Type)]
pub struct UniqueDef {
    pub name: String,
    pub base_type: String,
}

/// Static currency / exchange-able item (`/api/trade2/data/static`).
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize, specta::Type)]
pub struct StaticDef {
    /// Exchange id, e.g. `divine`.
    pub id: String,
    pub text: String,
    pub group: String,
    pub image: Option<String>,
}

/// Read-only view over bundled game data.
pub trait Catalog: Send + Sync {
    /// Stats whose template (or alias) equals `template` for the given modifier kind.
    fn stats_by_template(&self, template: &str, kind: ModKind) -> Vec<(&StatDef, bool)>;
    fn base(&self, name: &str) -> Option<&BaseDef>;
    /// Find the base contained in a magic item's full name (`Prefix Base of Suffix`).
    fn base_in_magic_name(&self, full_name: &str) -> Option<&BaseDef>;
    fn unique(&self, name: &str) -> Option<&UniqueDef>;
    fn static_item(&self, text: &str) -> Option<&StaticDef>;
}
