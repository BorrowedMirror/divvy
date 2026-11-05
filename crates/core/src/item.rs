//! Domain model for a parsed PoE2 item.
use serde::{Deserialize, Serialize};

/// Rarity line (`Rarity: ...`) of an item.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize, specta::Type)]
#[serde(rename_all = "snake_case")]
pub enum Rarity {
    Normal,
    Magic,
    Rare,
    Unique,
    Currency,
    Gem,
    /// Quest items, divination-like or other.
    Other,
}

/// Coarse category used for routing (which price source, which filters).
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize, specta::Type)]
#[serde(rename_all = "snake_case")]
pub enum ItemCategory {
    Currency,
    Gem,
    Unique,
    Equipment,
    Waystone,
    Tablet,
    Jewel,
    Flask,
    Charm,
    Relic,
    Other,
}

/// Exact `Item Class:` header value, normalised.
#[derive(Debug, Clone, PartialEq, Eq, Hash, Serialize, Deserialize, specta::Type)]
#[serde(rename_all = "snake_case", tag = "type", content = "value")]
pub enum ItemClass {
    StackableCurrency,
    Omen,
    Essence,
    SoulCore,
    Rune,
    Talisman,
    DistilledEmotion,
    Splinter,
    Catalyst,
    SkillGem,
    SupportGem,
    UncutGem,
    Waystone,
    Tablet,
    Jewel,
    LifeFlask,
    ManaFlask,
    Charm,
    Relic,
    Amulet,
    Ring,
    Belt,
    BodyArmour,
    Helmet,
    Gloves,
    Boots,
    Shield,
    Buckler,
    Focus,
    Quiver,
    /// Weapon subclasses ("Bows", "Two Hand Maces", ...), raw header value preserved.
    Weapon(String),
    /// Unmapped header value preserved verbatim.
    Other(String),
}

/// Which block of the item the modifier came from.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize, specta::Type)]
#[serde(rename_all = "snake_case")]
pub enum ModKind {
    Implicit,
    Explicit,
    Enchant,
    Rune,
    Crafted,
    Fractured,
    Desecrated,
    Sanctified,
    Skill,
}

/// Prefix / suffix information only available in advanced (Ctrl+Alt+C) copies.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize, specta::Type)]
pub struct AdvancedModInfo {
    pub affix: Option<Affix>,
    pub name: Option<String>,
    pub tier: Option<u8>,
    /// Value ranges as printed, e.g. `(10-20)` -> `[10.0, 20.0]`, per value.
    pub ranges: Vec<ValueRange>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize, specta::Type)]
#[serde(rename_all = "snake_case")]
pub enum Affix {
    Prefix,
    Suffix,
    Implicit,
}

#[derive(Debug, Clone, Copy, PartialEq, Serialize, Deserialize, specta::Type)]
pub struct ValueRange {
    pub min: f64,
    pub max: f64,
}

impl Eq for ValueRange {}

/// One modifier line.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize, specta::Type)]
pub struct Modifier {
    /// Line text exactly as copied (minus `(implicit)`-style suffix markers).
    pub text: String,
    /// Text with numbers replaced by `#`, used to look up stat ids.
    pub template: String,
    pub kind: ModKind,
    /// Numeric values in order of appearance.
    pub values: Vec<f64>,
    /// Trade stat id once matched (e.g. `explicit.stat_3299347043`).
    pub stat_id: Option<String>,
    pub advanced: Option<AdvancedModInfo>,
}

/// Generic `Name: value` property in the properties block.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize, specta::Type)]
pub struct Property {
    pub name: String,
    pub value: String,
    /// `(augmented)` marker present.
    pub augmented: bool,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Default, Serialize, Deserialize, specta::Type)]
pub struct Requirements {
    pub level: Option<u32>,
    pub str: Option<u32>,
    pub dex: Option<u32>,
    pub int: Option<u32>,
}

/// Defence / offence numbers extracted from properties.
#[derive(Debug, Clone, Copy, PartialEq, Default, Serialize, Deserialize, specta::Type)]
pub struct ItemStats {
    pub armour: Option<u32>,
    pub evasion: Option<u32>,
    pub energy_shield: Option<u32>,
    pub spirit: Option<u32>,
    pub block_chance: Option<u32>,
    pub physical_dps: Option<f64>,
    pub elemental_dps: Option<f64>,
    pub total_dps: Option<f64>,
    pub attacks_per_second: Option<f64>,
    pub critical_hit_chance: Option<f64>,
}

/// Fully parsed item. Every field is optional/empty unless present in the text.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize, specta::Type)]
pub struct ParsedItem {
    pub class: ItemClass,
    pub category: ItemCategory,
    pub rarity: Rarity,
    /// Unique / rare name line, if any (rares and uniques have two header lines).
    pub name: Option<String>,
    /// Base type line (e.g. `Expert Hunter Hood`, `Divine Orb`).
    pub base_type: String,
    pub item_level: Option<u32>,
    pub quality: Option<u32>,
    pub stack_size: Option<StackSize>,
    pub gem_level: Option<u32>,
    pub waystone_tier: Option<u32>,
    pub area_level: Option<u32>,
    /// Number of rune/soul-core sockets (`Sockets: S S`).
    pub sockets: u8,
    pub requirements: Requirements,
    pub stats: ItemStats,
    pub properties: Vec<Property>,
    pub modifiers: Vec<Modifier>,
    pub corrupted: bool,
    pub unidentified: bool,
    pub mirrored: bool,
    pub sanctified: bool,
    /// `Note: ~price 1 divine` line if present.
    pub note: Option<String>,
    /// True if parsed from an advanced (Ctrl+Alt+C) copy.
    pub advanced: bool,
    /// Original text, untouched.
    pub raw: String,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize, specta::Type)]
pub struct StackSize {
    pub current: u32,
    pub max: u32,
}

impl ItemClass {
    /// Map an `Item Class:` header value (e.g. "Stackable Currency") to an [`ItemClass`].
    pub fn from_header(_value: &str) -> Self {
        todo!("workstream A: map every PoE2 item class header")
    }

    /// Category used for routing.
    pub fn category(&self, _rarity: Rarity) -> ItemCategory {
        todo!("workstream A")
    }
}
