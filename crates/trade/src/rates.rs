//! Exchange-rate helpers shared by the aggregated price sources.
//!
//! [`ExchangeRates`] semantics follow core: `rates[c]` = value of one `c` in `base`.
use rkl_core::{
    item::{ItemCategory, ParsedItem},
    pricing::{ExchangeRates, Price, PriceSourceKind, PriceSummary},
};

/// Value of one `currency` in `rates.base`, if known.
pub fn value_in_base(rates: &ExchangeRates, currency: &str) -> Option<f64> {
    if currency == rates.base {
        return Some(1.0);
    }
    rates.rates.get(currency).copied().filter(|v| v.is_finite() && *v > 0.0)
}

/// Pick a readable currency for `amount` (expressed in `rates.base`): divine when it is
/// worth at least one divine, otherwise exalted; falls back to the base currency.
pub fn display_price(amount: f64, rates: &ExchangeRates) -> Price {
    if let Some(div) = value_in_base(rates, "divine") {
        let in_div = amount / div;
        if in_div >= 1.0 {
            return Price { amount: in_div, currency: "divine".into() };
        }
    }
    if let Some(ex) = value_in_base(rates, "exalted") {
        return Price { amount: amount / ex, currency: "exalted".into() };
    }
    Price { amount, currency: rates.base.clone() }
}

/// Single-value summary for an aggregated source (no individual listings).
pub fn aggregated_summary(source: PriceSourceKind, price: Price, total: u32) -> PriceSummary {
    PriceSummary {
        source,
        total,
        listings: Vec::new(),
        min: Some(price.clone()),
        median: Some(price.clone()),
        max: Some(price),
        trade_url: None,
    }
}

/// Name an aggregated source lists this item under: uncut gems carry their level
/// (`Uncut Support Gem (Level 2)`), everything else uses the base type.
pub fn exchange_name(item: &ParsedItem) -> String {
    match (item.category, item.gem_level) {
        (ItemCategory::Gem, Some(level)) if item.base_type.starts_with("Uncut ") => {
            format!("{} (Level {level})", item.base_type)
        }
        _ => item.base_type.clone(),
    }
}

#[cfg(test)]
pub(crate) mod test_support {
    use rkl_core::item::{ItemCategory, ItemClass, ItemStats, ParsedItem, Rarity, Requirements};

    /// Minimal parsed item for source tests.
    pub fn item(class: ItemClass, category: ItemCategory, name: Option<&str>, base: &str) -> ParsedItem {
        ParsedItem {
            class,
            category,
            rarity: if category == ItemCategory::Unique { Rarity::Unique } else { Rarity::Currency },
            name: name.map(Into::into),
            base_type: base.into(),
            item_level: None,
            quality: None,
            stack_size: None,
            gem_level: None,
            waystone_tier: None,
            area_level: None,
            sockets: 0,
            requirements: Requirements::default(),
            stats: ItemStats::default(),
            properties: Vec::new(),
            modifiers: Vec::new(),
            corrupted: false,
            unidentified: false,
            mirrored: false,
            sanctified: false,
            note: None,
            advanced: false,
            raw: String::new(),
        }
    }
}

#[cfg(test)]
mod tests {
    use rkl_core::item::ItemClass;

    use super::*;

    fn ninja_rates() -> ExchangeRates {
        ExchangeRates {
            base: "divine".into(),
            rates: [("exalted".to_owned(), 1.0 / 724.8), ("chaos".to_owned(), 1.0 / 9.36)].into(),
        }
    }

    #[test]
    fn displays_in_divine_or_exalted() {
        let r = ninja_rates();
        assert_eq!(display_price(130.0, &r), Price { amount: 130.0, currency: "divine".into() });
        let p = display_price(0.5, &r);
        assert_eq!(p.currency, "exalted");
        assert!((p.amount - 362.4).abs() < 1e-6);
        let scout = ExchangeRates { base: "exalted".into(), rates: [("divine".to_owned(), 966.0)].into() };
        assert_eq!(display_price(15.0, &scout), Price { amount: 15.0, currency: "exalted".into() });
        assert_eq!(display_price(1932.0, &scout), Price { amount: 2.0, currency: "divine".into() });
        let bare = ExchangeRates { base: "chaos".into(), rates: [("divine".to_owned(), f64::NAN)].into() };
        assert_eq!(display_price(3.0, &bare), Price { amount: 3.0, currency: "chaos".into() });
    }

    #[test]
    fn summary_has_single_value() {
        let s = aggregated_summary(PriceSourceKind::PoeNinja, Price { amount: 1.0, currency: "divine".into() }, 7);
        assert_eq!(s.total, 7);
        assert_eq!(s.min, s.max);
        assert!(s.listings.is_empty() && s.trade_url.is_none());
    }

    #[test]
    fn exchange_name_for_uncut_gems() {
        let mut gem = test_support::item(ItemClass::UncutGem, ItemCategory::Gem, None, "Uncut Support Gem");
        assert_eq!(exchange_name(&gem), "Uncut Support Gem");
        gem.gem_level = Some(2);
        assert_eq!(exchange_name(&gem), "Uncut Support Gem (Level 2)");
        let orb = test_support::item(ItemClass::StackableCurrency, ItemCategory::Currency, None, "Divine Orb");
        assert_eq!(exchange_name(&orb), "Divine Orb");
    }
}
