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

/// Weapon class headers as printed by PoE2 (plural form).
pub const WEAPON_CLASSES: &[&str] = &[
    "Bows",
    "Crossbows",
    "Wands",
    "Staves",
    "Warstaves",
    "Quarterstaves",
    "One Hand Maces",
    "Two Hand Maces",
    "One Hand Swords",
    "Two Hand Swords",
    "One Hand Axes",
    "Two Hand Axes",
    "Sceptres",
    "Spears",
    "Flails",
    "Daggers",
    "Claws",
    "Traps",
    "Fishing Rods",
];

impl ItemClass {
    /// Map an `Item Class:` header value (e.g. "Stackable Currency") to an [`ItemClass`].
    ///
    /// Matching is case-insensitive and tolerant of singular/plural spellings.
    /// Unknown values are preserved in [`ItemClass::Other`]; weapon subclasses keep
    /// their raw header in [`ItemClass::Weapon`].
    pub fn from_header(value: &str) -> Self {
        let raw = value.trim();
        let key = raw.to_ascii_lowercase();
        match key.as_str() {
            "stackable currency" | "currency" => Self::StackableCurrency,
            "omen" | "omens" => Self::Omen,
            "essence" | "essences" => Self::Essence,
            "soul core" | "soul cores" => Self::SoulCore,
            "rune" | "runes" | "augment" | "augments" | "socketable" | "socketables" => Self::Rune,
            "talisman" | "talismans" => Self::Talisman,
            "distilled emotion" | "distilled emotions" => Self::DistilledEmotion,
            "splinter" | "splinters" | "breach splinter" | "breach splinters" => Self::Splinter,
            "catalyst" | "catalysts" | "breach catalyst" | "breach catalysts" => Self::Catalyst,
            "skill gem" | "skill gems" | "active skill gems" => Self::SkillGem,
            "support gem" | "support gems" | "meta gem" | "meta gems" => Self::SupportGem,
            "uncut skill gem" | "uncut skill gems" | "uncut support gem" | "uncut support gems"
            | "uncut spirit gem" | "uncut spirit gems" | "uncut gems" => Self::UncutGem,
            "waystone" | "waystones" | "map" | "maps" => Self::Waystone,
            "tablet" | "tablets" | "precursor tablet" | "precursor tablets" => Self::Tablet,
            "jewel" | "jewels" => Self::Jewel,
            "life flask" | "life flasks" => Self::LifeFlask,
            "mana flask" | "mana flasks" => Self::ManaFlask,
            "charm" | "charms" | "utility flask" | "utility flasks" => Self::Charm,
            "relic" | "relics" => Self::Relic,
            "amulet" | "amulets" => Self::Amulet,
            "ring" | "rings" => Self::Ring,
            "belt" | "belts" => Self::Belt,
            "body armour" | "body armours" => Self::BodyArmour,
            "helmet" | "helmets" => Self::Helmet,
            "glove" | "gloves" => Self::Gloves,
            "boot" | "boots" => Self::Boots,
            "shield" | "shields" => Self::Shield,
            "buckler" | "bucklers" => Self::Buckler,
            "focus" | "foci" => Self::Focus,
            "quiver" | "quivers" => Self::Quiver,
            _ if WEAPON_CLASSES.iter().any(|w| w.eq_ignore_ascii_case(raw)) || is_weapon_singular(&key) => {
                Self::Weapon(raw.to_owned())
            }
            _ => Self::Other(raw.to_owned()),
        }
    }

    /// Category used for routing.
    ///
    /// Currency-like and gem classes keep their category regardless of rarity; any other
    /// unique item routes as [`ItemCategory::Unique`].
    pub fn category(&self, rarity: Rarity) -> ItemCategory {
        let base = match self {
            Self::StackableCurrency
            | Self::Omen
            | Self::Essence
            | Self::SoulCore
            | Self::Rune
            | Self::Talisman
            | Self::DistilledEmotion
            | Self::Splinter
            | Self::Catalyst => return ItemCategory::Currency,
            Self::SkillGem | Self::SupportGem | Self::UncutGem => return ItemCategory::Gem,
            Self::Waystone => ItemCategory::Waystone,
            Self::Tablet => ItemCategory::Tablet,
            Self::Jewel => ItemCategory::Jewel,
            Self::LifeFlask | Self::ManaFlask => ItemCategory::Flask,
            Self::Charm => ItemCategory::Charm,
            Self::Relic => ItemCategory::Relic,
            Self::Amulet
            | Self::Ring
            | Self::Belt
            | Self::BodyArmour
            | Self::Helmet
            | Self::Gloves
            | Self::Boots
            | Self::Shield
            | Self::Buckler
            | Self::Focus
            | Self::Quiver
            | Self::Weapon(_) => ItemCategory::Equipment,
            Self::Other(_) => match rarity {
                Rarity::Currency => return ItemCategory::Currency,
                Rarity::Gem => return ItemCategory::Gem,
                _ => ItemCategory::Other,
            },
        };
        if rarity == Rarity::Unique { ItemCategory::Unique } else { base }
    }

    /// Trade2 `type_filters.category` option for this class, when one exists.
    ///
    /// Ids follow `/api/trade2/data/filters` (`weapon.bow`, `armour.helmet`, ...).
    pub fn trade_category(&self) -> Option<&'static str> {
        Some(match self {
            Self::StackableCurrency | Self::Catalyst => "currency",
            Self::Omen => "currency.omen",
            Self::Essence => "currency.essence",
            Self::SoulCore => "currency.soulcore",
            Self::Rune => "currency.rune",
            Self::Talisman => "currency.talisman",
            Self::DistilledEmotion => "currency.distilledemotion",
            Self::Splinter => "currency.splinter",
            Self::SkillGem => "gem.activegem",
            Self::SupportGem => "gem.supportgem",
            Self::UncutGem => "gem",
            Self::Waystone => "map.waystone",
            Self::Tablet => "map.tablet",
            Self::Jewel => "jewel",
            Self::LifeFlask => "flask.life",
            Self::ManaFlask => "flask.mana",
            Self::Charm => "azmeri.charm",
            Self::Relic => "sanctum.relic",
            Self::Amulet => "accessory.amulet",
            Self::Ring => "accessory.ring",
            Self::Belt => "accessory.belt",
            Self::BodyArmour => "armour.chest",
            Self::Helmet => "armour.helmet",
            Self::Gloves => "armour.gloves",
            Self::Boots => "armour.boots",
            Self::Shield => "armour.shield",
            Self::Buckler => "armour.buckler",
            Self::Focus => "armour.focus",
            Self::Quiver => "armour.quiver",
            Self::Weapon(raw) => return weapon_trade_category(raw),
            Self::Other(_) => return None,
        })
    }

    /// Whether this class is a weapon.
    pub fn is_weapon(&self) -> bool {
        matches!(self, Self::Weapon(_))
    }
}

fn weapon_trade_category(raw: &str) -> Option<&'static str> {
    let key = raw.to_ascii_lowercase();
    let key = key.trim_end_matches('s');
    Some(match key {
        "bow" => "weapon.bow",
        "crossbow" => "weapon.crossbow",
        "wand" => "weapon.wand",
        "stave" | "staff" => "weapon.staff",
        "quarterstave" | "quarterstaff" | "warstave" | "warstaff" => "weapon.warstaff",
        "one hand mace" => "weapon.onemace",
        "two hand mace" => "weapon.twomace",
        "one hand sword" => "weapon.onesword",
        "two hand sword" => "weapon.twosword",
        "one hand axe" => "weapon.oneaxe",
        "two hand axe" => "weapon.twoaxe",
        "sceptre" => "weapon.sceptre",
        "spear" => "weapon.spear",
        "flail" => "weapon.flail",
        "dagger" => "weapon.dagger",
        "claw" => "weapon.claw",
        "trap" => "weapon.trap",
        "fishing rod" => "weapon.rod",
        _ => return None,
    })
}

fn is_weapon_singular(key: &str) -> bool {
    WEAPON_CLASSES.iter().any(|w| {
        let w = w.to_ascii_lowercase();
        let singular = w
            .strip_suffix("ves")
            .map(|s| format!("{s}ff"))
            .or_else(|| w.strip_suffix('s').map(str::to_owned))
            .unwrap_or_default();
        singular == key
    })
}

impl Rarity {
    /// Parse the value of a `Rarity:` line.
    pub fn from_header(value: &str) -> Option<Self> {
        Some(match value.trim().to_ascii_lowercase().as_str() {
            "normal" => Self::Normal,
            "magic" => Self::Magic,
            "rare" => Self::Rare,
            "unique" => Self::Unique,
            "currency" => Self::Currency,
            "gem" => Self::Gem,
            "quest" | "divination card" | "other" => Self::Other,
            _ => return None,
        })
    }

    /// Lower-case trade2 rarity option (`normal`, `magic`, `rare`, `unique`), if any.
    pub fn trade_option(self) -> Option<&'static str> {
        match self {
            Self::Normal => Some("normal"),
            Self::Magic => Some("magic"),
            Self::Rare => Some("rare"),
            Self::Unique => Some("unique"),
            _ => None,
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn maps_every_known_header() {
        let cases: &[(&str, ItemClass)] = &[
            ("Stackable Currency", ItemClass::StackableCurrency),
            ("Omen", ItemClass::Omen),
            ("Essence", ItemClass::Essence),
            ("Essences", ItemClass::Essence),
            ("Soul Cores", ItemClass::SoulCore),
            ("Runes", ItemClass::Rune),
            ("Socketable", ItemClass::Rune),
            ("Talismans", ItemClass::Talisman),
            ("Distilled Emotions", ItemClass::DistilledEmotion),
            ("Splinter", ItemClass::Splinter),
            ("Catalysts", ItemClass::Catalyst),
            ("Skill Gems", ItemClass::SkillGem),
            ("Support Gems", ItemClass::SupportGem),
            ("Uncut Skill Gems", ItemClass::UncutGem),
            ("Uncut Support Gems", ItemClass::UncutGem),
            ("Uncut Spirit Gems", ItemClass::UncutGem),
            ("Waystones", ItemClass::Waystone),
            ("Tablet", ItemClass::Tablet),
            ("Precursor Tablets", ItemClass::Tablet),
            ("Jewels", ItemClass::Jewel),
            ("Life Flasks", ItemClass::LifeFlask),
            ("Mana Flasks", ItemClass::ManaFlask),
            ("Charms", ItemClass::Charm),
            ("Relics", ItemClass::Relic),
            ("Amulets", ItemClass::Amulet),
            ("Rings", ItemClass::Ring),
            ("Belts", ItemClass::Belt),
            ("Body Armours", ItemClass::BodyArmour),
            ("Helmets", ItemClass::Helmet),
            ("Gloves", ItemClass::Gloves),
            ("Boots", ItemClass::Boots),
            ("Shields", ItemClass::Shield),
            ("Bucklers", ItemClass::Buckler),
            ("Foci", ItemClass::Focus),
            ("Quivers", ItemClass::Quiver),
            ("  helmets \r", ItemClass::Helmet),
        ];
        for (header, want) in cases {
            assert_eq!(&ItemClass::from_header(header), want, "{header}");
        }
    }

    #[test]
    fn weapons_preserve_raw_header() {
        for w in WEAPON_CLASSES {
            assert_eq!(ItemClass::from_header(w), ItemClass::Weapon((*w).to_owned()));
            assert!(ItemClass::from_header(w).is_weapon());
        }
        assert_eq!(ItemClass::from_header("Bow"), ItemClass::Weapon("Bow".into()));
        assert_eq!(ItemClass::from_header("Staff"), ItemClass::Weapon("Staff".into()));
        assert_eq!(ItemClass::from_header("Quarterstaff"), ItemClass::Weapon("Quarterstaff".into()));
    }

    #[test]
    fn unknown_is_other() {
        assert_eq!(ItemClass::from_header("Pinnacle Keys"), ItemClass::Other("Pinnacle Keys".into()));
        assert!(!ItemClass::from_header("Rings").is_weapon());
    }

    #[test]
    fn weapon_trade_categories() {
        for w in WEAPON_CLASSES {
            assert!(ItemClass::from_header(w).trade_category().is_some(), "{w} has no trade category");
        }
        assert_eq!(ItemClass::Weapon("Lances".into()).trade_category(), None);
        assert_eq!(ItemClass::Other("x".into()).trade_category(), None);
        assert_eq!(ItemClass::Helmet.trade_category(), Some("armour.helmet"));
    }

    #[test]
    fn every_class_has_category() {
        let all = [
            ItemClass::StackableCurrency,
            ItemClass::Omen,
            ItemClass::Essence,
            ItemClass::SoulCore,
            ItemClass::Rune,
            ItemClass::Talisman,
            ItemClass::DistilledEmotion,
            ItemClass::Splinter,
            ItemClass::Catalyst,
            ItemClass::SkillGem,
            ItemClass::SupportGem,
            ItemClass::UncutGem,
            ItemClass::Waystone,
            ItemClass::Tablet,
            ItemClass::Jewel,
            ItemClass::LifeFlask,
            ItemClass::ManaFlask,
            ItemClass::Charm,
            ItemClass::Relic,
            ItemClass::Amulet,
            ItemClass::Ring,
            ItemClass::Belt,
            ItemClass::BodyArmour,
            ItemClass::Helmet,
            ItemClass::Gloves,
            ItemClass::Boots,
            ItemClass::Shield,
            ItemClass::Buckler,
            ItemClass::Focus,
            ItemClass::Quiver,
        ];
        for c in &all {
            assert!(c.trade_category().is_some(), "{c:?}");
            let _ = c.category(Rarity::Rare);
        }
    }

    #[test]
    fn categories() {
        assert_eq!(ItemClass::StackableCurrency.category(Rarity::Currency), ItemCategory::Currency);
        assert_eq!(ItemClass::Omen.category(Rarity::Unique), ItemCategory::Currency);
        assert_eq!(ItemClass::SkillGem.category(Rarity::Gem), ItemCategory::Gem);
        assert_eq!(ItemClass::UncutGem.category(Rarity::Currency), ItemCategory::Gem);
        assert_eq!(ItemClass::Helmet.category(Rarity::Rare), ItemCategory::Equipment);
        assert_eq!(ItemClass::Helmet.category(Rarity::Unique), ItemCategory::Unique);
        assert_eq!(ItemClass::Jewel.category(Rarity::Unique), ItemCategory::Unique);
        assert_eq!(ItemClass::Jewel.category(Rarity::Magic), ItemCategory::Jewel);
        assert_eq!(ItemClass::Waystone.category(Rarity::Rare), ItemCategory::Waystone);
        assert_eq!(ItemClass::Tablet.category(Rarity::Magic), ItemCategory::Tablet);
        assert_eq!(ItemClass::LifeFlask.category(Rarity::Normal), ItemCategory::Flask);
        assert_eq!(ItemClass::Charm.category(Rarity::Normal), ItemCategory::Charm);
        assert_eq!(ItemClass::Relic.category(Rarity::Magic), ItemCategory::Relic);
        assert_eq!(ItemClass::Relic.category(Rarity::Unique), ItemCategory::Unique);
        assert_eq!(ItemClass::Weapon("Bows".into()).category(Rarity::Normal), ItemCategory::Equipment);
        assert_eq!(ItemClass::Other("x".into()).category(Rarity::Currency), ItemCategory::Currency);
        assert_eq!(ItemClass::Other("x".into()).category(Rarity::Gem), ItemCategory::Gem);
        assert_eq!(ItemClass::Other("x".into()).category(Rarity::Normal), ItemCategory::Other);
        assert_eq!(ItemClass::Other("x".into()).category(Rarity::Unique), ItemCategory::Unique);
    }

    #[test]
    fn rarity_header() {
        assert_eq!(Rarity::from_header("Rare"), Some(Rarity::Rare));
        assert_eq!(Rarity::from_header("Normal"), Some(Rarity::Normal));
        assert_eq!(Rarity::from_header("Magic"), Some(Rarity::Magic));
        assert_eq!(Rarity::from_header("Unique"), Some(Rarity::Unique));
        assert_eq!(Rarity::from_header("Currency"), Some(Rarity::Currency));
        assert_eq!(Rarity::from_header("Gem"), Some(Rarity::Gem));
        assert_eq!(Rarity::from_header("Quest"), Some(Rarity::Other));
        assert_eq!(Rarity::from_header("Legendary"), None);
        assert_eq!(Rarity::Rare.trade_option(), Some("rare"));
        assert_eq!(Rarity::Normal.trade_option(), Some("normal"));
        assert_eq!(Rarity::Magic.trade_option(), Some("magic"));
        assert_eq!(Rarity::Unique.trade_option(), Some("unique"));
        assert_eq!(Rarity::Gem.trade_option(), None);
    }
}
