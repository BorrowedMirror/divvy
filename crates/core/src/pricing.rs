//! Price aggregation over listings.
use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize, specta::Type)]
pub struct Price {
    pub amount: f64,
    /// Trade currency id, e.g. `divine`, `exalted`, `chaos`.
    pub currency: String,
}

/// One listing from a price source.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize, specta::Type)]
pub struct Listing {
    pub id: String,
    pub price: Price,
    pub seller: Option<String>,
    pub online: bool,
    /// RFC 3339 listing timestamp.
    pub indexed: Option<String>,
    pub item_level: Option<u32>,
    pub stack_size: Option<u32>,
}

/// Where a price came from.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize, specta::Type)]
#[serde(rename_all = "snake_case")]
pub enum PriceSourceKind {
    Trade,
    PoeNinja,
    Poe2Scout,
}

/// Aggregated result displayed by the overlay.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize, specta::Type)]
pub struct PriceSummary {
    pub source: PriceSourceKind,
    /// Total matches reported by the source (may exceed `listings.len()`).
    pub total: u32,
    pub listings: Vec<Listing>,
    /// Min / median / max normalised to the most common currency among listings.
    pub min: Option<Price>,
    pub median: Option<Price>,
    pub max: Option<Price>,
    /// Link that opens the same search on pathofexile.com.
    pub trade_url: Option<String>,
}

/// Exchange rates to normalise prices into one currency; `rates[c]` = value of 1 `c` in `base`.
#[derive(Debug, Clone, PartialEq, Default, Serialize, Deserialize, specta::Type)]
pub struct ExchangeRates {
    pub base: String,
    pub rates: std::collections::BTreeMap<String, f64>,
}

impl ExchangeRates {
    /// Value of one `currency` in `base` (`base` itself is 1).
    pub fn rate(&self, currency: &str) -> Option<f64> {
        if currency == self.base {
            return Some(1.0);
        }
        self.rates.get(currency).copied().filter(|r| r.is_finite() && *r > 0.0)
    }

    /// Convert `amount` of `from` into `to`, when both rates are known.
    pub fn convert(&self, amount: f64, from: &str, to: &str) -> Option<f64> {
        if from == to {
            return Some(amount);
        }
        Some(amount * self.rate(from)? / self.rate(to)?)
    }
}

/// Minimum number of priced listings before outlier trimming kicks in.
pub const TRIM_MIN_LISTINGS: usize = 4;

/// Tukey fence multiplier for outlier trimming.
const IQR_FENCE: f64 = 1.5;

fn quantile(sorted: &[f64], q: f64) -> f64 {
    let pos = q * (sorted.len() - 1) as f64;
    let lo = pos.floor() as usize;
    let hi = pos.ceil() as usize;
    sorted[lo] + (sorted[hi] - sorted[lo]) * (pos - lo as f64)
}

/// Drop values outside the Tukey fences (`Q1 - 1.5 IQR`, `Q3 + 1.5 IQR`).
///
/// This removes both price-fixing bait at the low end and absurd asks at the high end
/// while keeping small samples intact.
pub fn trim_outliers(sorted: &[f64]) -> Vec<f64> {
    if sorted.len() < TRIM_MIN_LISTINGS {
        return sorted.to_vec();
    }
    let q1 = quantile(sorted, 0.25);
    let q3 = quantile(sorted, 0.75);
    let iqr = q3 - q1;
    let (lo, hi) = (q1 - IQR_FENCE * iqr, q3 + IQR_FENCE * iqr);
    sorted.iter().copied().filter(|v| (lo..=hi).contains(v)).collect()
}

fn round2(v: f64) -> f64 {
    (v * 100.0).round() / 100.0
}

/// Aggregate listings into a summary. Pure; outlier trimming lives here.
///
/// Prices are normalised to the most common currency among the listings (ties broken
/// alphabetically). With `rates`, other currencies are converted; without, listings in
/// other currencies are left out of min/median/max (but stay in `listings`).
/// Stack prices are per listing as given by the source.
pub fn summarise(
    source: PriceSourceKind,
    total: u32,
    listings: Vec<Listing>,
    rates: Option<&ExchangeRates>,
) -> PriceSummary {
    let mut counts: std::collections::BTreeMap<&str, usize> = std::collections::BTreeMap::new();
    for l in &listings {
        *counts.entry(l.price.currency.as_str()).or_default() += 1;
    }
    let target = counts.iter().max_by(|a, b| a.1.cmp(b.1).then(b.0.cmp(a.0))).map(|(c, _)| (*c).to_owned());

    let (min, median, max) = match &target {
        None => (None, None, None),
        Some(target) => {
            let mut values: Vec<f64> = listings
                .iter()
                .filter(|l| l.price.amount.is_finite() && l.price.amount > 0.0)
                .filter_map(|l| {
                    if &l.price.currency == target {
                        Some(l.price.amount)
                    } else {
                        rates.and_then(|r| r.convert(l.price.amount, &l.price.currency, target))
                    }
                })
                .collect();
            values.sort_by(f64::total_cmp);
            let kept = trim_outliers(&values);
            let price = |amount: f64| Price { amount: round2(amount), currency: target.clone() };
            if kept.is_empty() {
                (None, None, None)
            } else {
                let mid = kept.len() / 2;
                let median =
                    if kept.len().is_multiple_of(2) { f64::midpoint(kept[mid - 1], kept[mid]) } else { kept[mid] };
                (Some(price(kept[0])), Some(price(median)), Some(price(kept[kept.len() - 1])))
            }
        }
    };

    PriceSummary { source, total, listings, min, median, max, trade_url: None }
}

#[cfg(test)]
mod tests {
    use pretty_assertions::assert_eq;
    use proptest::prelude::*;

    use super::*;

    fn listing(amount: f64, currency: &str) -> Listing {
        Listing {
            id: format!("{amount}{currency}"),
            price: Price { amount, currency: currency.into() },
            seller: None,
            online: true,
            indexed: None,
            item_level: None,
            stack_size: None,
        }
    }

    #[allow(clippy::unnecessary_wraps)] // compared against Option fields
    fn p(amount: f64, currency: &str) -> Option<Price> {
        Some(Price { amount, currency: currency.into() })
    }

    #[test]
    fn empty() {
        let s = summarise(PriceSourceKind::Trade, 0, vec![], None);
        assert_eq!((s.min, s.median, s.max), (None, None, None));
        assert_eq!(s.total, 0);
    }

    #[test]
    fn single_currency_with_outliers() {
        let ls = [1.0, 10.0, 11.0, 12.0, 12.0, 13.0, 500.0].iter().map(|a| listing(*a, "exalted")).collect();
        let s = summarise(PriceSourceKind::Trade, 120, ls, None);
        assert_eq!(s.min, p(10.0, "exalted"));
        assert_eq!(s.median, p(12.0, "exalted"));
        assert_eq!(s.max, p(13.0, "exalted"));
        assert_eq!(s.listings.len(), 7);
        assert_eq!(s.total, 120);
        assert_eq!(s.source, PriceSourceKind::Trade);
    }

    #[test]
    fn even_median_and_small_sample() {
        let ls = vec![listing(1.0, "divine"), listing(2.0, "divine"), listing(100.0, "divine")];
        let s = summarise(PriceSourceKind::PoeNinja, 3, ls, None);
        assert_eq!(s.max, p(100.0, "divine"), "no trimming below threshold");
        let ls = vec![listing(1.0, "divine"), listing(2.0, "divine")];
        assert_eq!(summarise(PriceSourceKind::Poe2Scout, 2, ls, None).median, p(1.5, "divine"));
    }

    #[test]
    fn converts_with_rates() {
        let rates = ExchangeRates {
            base: "exalted".into(),
            rates: [("divine".to_owned(), 200.0), ("chaos".to_owned(), 10.0), ("bad".to_owned(), 0.0)]
                .into_iter()
                .collect(),
        };
        assert_eq!(rates.rate("exalted"), Some(1.0));
        assert_eq!(rates.rate("bad"), None);
        assert_eq!(rates.convert(1.0, "divine", "chaos"), Some(20.0));
        assert_eq!(rates.convert(1.0, "bad", "chaos"), None);
        let ls = vec![
            listing(1.0, "divine"),
            listing(2.0, "divine"),
            listing(100.0, "exalted"),
            listing(5.0, "mirror"),
            listing(-1.0, "divine"),
        ];
        let s = summarise(PriceSourceKind::Trade, 5, ls.clone(), Some(&rates));
        assert_eq!(s.min, p(0.5, "divine"));
        assert_eq!(s.max, p(2.0, "divine"));
        // Without rates, foreign currencies are ignored.
        let s = summarise(PriceSourceKind::Trade, 5, ls, None);
        assert_eq!(s.min, p(1.0, "divine"));
        // Nothing priceable.
        let s = summarise(PriceSourceKind::Trade, 1, vec![listing(0.0, "divine")], None);
        assert_eq!(s.min, None);
    }

    #[test]
    fn tie_breaks_alphabetically() {
        let ls = vec![listing(3.0, "exalted"), listing(1.0, "divine")];
        assert_eq!(summarise(PriceSourceKind::Trade, 2, ls, None).min, p(1.0, "divine"));
    }

    proptest! {
        #[test]
        fn summary_is_ordered(amounts in proptest::collection::vec(0.01f64..10_000.0, 1..40)) {
            let ls = amounts.iter().map(|a| listing(*a, "exalted")).collect();
            let s = summarise(PriceSourceKind::Trade, 0, ls, None);
            let (min, med, max) = (s.min.unwrap().amount, s.median.unwrap().amount, s.max.unwrap().amount);
            prop_assert!(min <= med && med <= max);
        }
    }
}
