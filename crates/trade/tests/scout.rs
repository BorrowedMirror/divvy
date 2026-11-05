//! poe2scout contract tests against recorded fixtures.
use std::time::Duration;

use rkl_core::{
    item::{ItemCategory, ItemClass, ItemStats, ParsedItem, Rarity, Requirements},
    pricing::{Price, PriceSourceKind},
    query::{FilterSet, MiscFilters, OnlineStatus, QueryRoute},
};
use rkl_trade::{PriceRequest, PriceSource, TradeError, scout::Poe2ScoutSource};
use url::Url;
use wiremock::{
    Mock, MockServer, ResponseTemplate,
    matchers::{header, method, path},
};

const UA: &str = "rkl-test/0 (+scout)";
const LEAGUES: &str = include_str!("fixtures/scout/leagues.json");
const ITEMS: &str = include_str!("fixtures/scout/items.json");
const LEAGUE: &str = "Forbidden Rites";
const ITEMS_PATH: &str = "/poe2/Leagues/Forbidden%20Rites/Items";

fn item(class: ItemClass, category: ItemCategory, name: Option<&str>, base: &str) -> ParsedItem {
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

fn currency(base: &str) -> ParsedItem {
    item(ItemClass::StackableCurrency, ItemCategory::Currency, None, base)
}

fn unique(name: &str, base: &str) -> ParsedItem {
    item(ItemClass::Belt, ItemCategory::Unique, Some(name), base)
}

fn request(item: ParsedItem, league: &str) -> PriceRequest {
    PriceRequest {
        item,
        filters: FilterSet {
            league: league.into(),
            status: OnlineStatus::Online,
            name: None,
            base_type: None,
            category: None,
            rarity: None,
            stats: Vec::new(),
            misc: MiscFilters::default(),
            widen_pct: 10.0,
            route: QueryRoute::Exchange,
        },
    }
}

async fn server() -> MockServer {
    let s = MockServer::start().await;
    Mock::given(method("GET"))
        .and(path("/poe2/Leagues"))
        .and(header("user-agent", UA))
        .respond_with(ResponseTemplate::new(200).set_body_raw(LEAGUES, "application/json"))
        .mount(&s)
        .await;
    Mock::given(method("GET"))
        .and(path(ITEMS_PATH))
        .and(header("user-agent", UA))
        .respond_with(ResponseTemplate::new(200).set_body_raw(ITEMS, "application/json"))
        .mount(&s)
        .await;
    s
}

fn source(s: &MockServer) -> Poe2ScoutSource {
    Poe2ScoutSource::new(Url::parse(&s.uri()).unwrap(), UA.into())
}

async fn price_of(src: &Poe2ScoutSource, it: ParsedItem) -> Price {
    let summary = src.price(&request(it, LEAGUE)).await.expect("priced");
    assert_eq!(summary.source, PriceSourceKind::Poe2Scout);
    assert_eq!(summary.min, summary.max);
    summary.median.expect("median")
}

#[tokio::test]
async fn prices_currency_in_divine_and_exalted() {
    let s = server().await;
    let src = source(&s);
    assert_eq!(src.kind(), PriceSourceKind::Poe2Scout);
    let div = price_of(&src, currency("Divine Orb")).await;
    assert_eq!(div.currency, "divine");
    assert!((div.amount - 1.0).abs() < 1e-9);
    let alch = price_of(&src, currency("Orb of Alchemy")).await;
    assert_eq!(alch.currency, "exalted");
    assert!((alch.amount - 3.9217).abs() < 1e-3);
}

#[tokio::test]
async fn prices_uniques() {
    let s = server().await;
    let src = source(&s);
    let mb = price_of(&src, unique("Mageblood", "Utility Belt")).await;
    assert_eq!(mb.currency, "divine");
    assert!((mb.amount - 70000.0 / 966.563_873_384_240_5).abs() < 1e-6);
    let andv = price_of(&src, item(ItemClass::Ring, ItemCategory::Unique, Some("Andvarius"), "Gold Ring")).await;
    assert_eq!(andv, Price { amount: 15.0, currency: "exalted".into() });
    // Base type mismatch falls back to the name alone.
    let hh = price_of(&src, unique("Headhunter", "Some Other Belt")).await;
    assert_eq!(hh.currency, "divine");
}

#[tokio::test]
async fn prices_uncut_gem_by_level() {
    let s = server().await;
    let src = source(&s);
    let mut gem = item(ItemClass::UncutGem, ItemCategory::Gem, None, "Uncut Support Gem");
    gem.gem_level = Some(2);
    assert_eq!(price_of(&src, gem).await, Price { amount: 20.0, currency: "exalted".into() });
}

#[tokio::test]
async fn unknown_item_and_league_are_unsupported() {
    let s = server().await;
    let src = source(&s);
    let err = src.price(&request(currency("Mirror of Kalandra"), LEAGUE)).await.unwrap_err();
    assert!(matches!(err, TradeError::Unsupported("poe2scout")));
    let err = src.price(&request(currency("Divine Orb"), "Nope")).await.unwrap_err();
    assert!(matches!(err, TradeError::Unsupported("poe2scout")));
    let rare = item(ItemClass::Helmet, ItemCategory::Equipment, None, "Hood");
    let err = src.price(&request(rare, LEAGUE)).await.unwrap_err();
    assert!(matches!(err, TradeError::Unsupported("poe2scout")));
}

#[tokio::test]
async fn null_price_is_unsupported() {
    let s = MockServer::start().await;
    Mock::given(path("/poe2/Leagues"))
        .respond_with(ResponseTemplate::new(200).set_body_raw(LEAGUES, "application/json"))
        .mount(&s)
        .await;
    Mock::given(path(ITEMS_PATH))
        .respond_with(ResponseTemplate::new(200).set_body_raw(
            r#"[{"ItemId":1,"CategoryApiId":"currency","Text":"Divine Orb","CurrentPrice":null}]"#,
            "application/json",
        ))
        .mount(&s)
        .await;
    let err = source(&s).price(&request(currency("Divine Orb"), LEAGUE)).await.unwrap_err();
    assert!(matches!(err, TradeError::Unsupported(_)));
}

#[test]
fn supports_matrix() {
    let src = Poe2ScoutSource::new(Url::parse("http://localhost").unwrap(), UA.into());
    assert!(src.supports(&currency("Divine Orb")));
    assert!(src.supports(&item(ItemClass::UncutGem, ItemCategory::Gem, None, "Uncut Skill Gem")));
    assert!(src.supports(&unique("Mageblood", "Utility Belt")));
    assert!(!src.supports(&item(ItemClass::Belt, ItemCategory::Unique, None, "Utility Belt")));
    assert!(!src.supports(&item(ItemClass::Helmet, ItemCategory::Equipment, None, "Hood")));
    assert!(!src.supports(&item(ItemClass::Waystone, ItemCategory::Waystone, None, "Waystone (Tier 15)")));
}

#[tokio::test]
async fn server_error_maps_to_http() {
    let s = MockServer::start().await;
    Mock::given(path("/poe2/Leagues")).respond_with(ResponseTemplate::new(503).set_body_string("down")).mount(&s).await;
    match source(&s).rates(LEAGUE).await.unwrap_err() {
        TradeError::Http { status, message } => {
            assert_eq!(status, 503);
            assert_eq!(message, "down");
        }
        e => panic!("unexpected {e:?}"),
    }
}

#[tokio::test]
async fn bad_json_maps_to_decode() {
    let s = MockServer::start().await;
    Mock::given(path("/poe2/Leagues"))
        .respond_with(ResponseTemplate::new(200).set_body_raw(LEAGUES, "application/json"))
        .mount(&s)
        .await;
    Mock::given(path(ITEMS_PATH))
        .respond_with(ResponseTemplate::new(200).set_body_raw("{not json", "application/json"))
        .mount(&s)
        .await;
    let err = source(&s).price(&request(currency("Divine Orb"), LEAGUE)).await.unwrap_err();
    assert!(matches!(err, TradeError::Decode(ref m) if m.starts_with("poe2scout")), "{err:?}");
}

#[tokio::test]
async fn caches_leagues_and_items() {
    let s = MockServer::start().await;
    Mock::given(method("GET"))
        .and(path("/poe2/Leagues"))
        .respond_with(ResponseTemplate::new(200).set_body_raw(LEAGUES, "application/json"))
        .expect(1)
        .mount(&s)
        .await;
    Mock::given(method("GET"))
        .and(path(ITEMS_PATH))
        .respond_with(ResponseTemplate::new(200).set_body_raw(ITEMS, "application/json"))
        .expect(1)
        .mount(&s)
        .await;
    let src = Poe2ScoutSource::with_cache_ttl(Url::parse(&s.uri()).unwrap(), UA.into(), Duration::from_secs(60));
    price_of(&src, currency("Divine Orb")).await;
    price_of(&src, currency("Chaos Orb")).await;
    price_of(&src, unique("Mageblood", "Utility Belt")).await;
    s.verify().await;
}

#[tokio::test]
async fn rates_from_league_entry() {
    let s = server().await;
    let src = source(&s);
    let r = src.rates("Standard").await.unwrap();
    assert_eq!(r.base, "exalted");
    assert!((r.rates["divine"] - 195.887_294_117_647_03).abs() < 1e-9);
    assert!((r.rates["chaos"] - 195.887_294_117_647_03 / 1.843_645_121_107_266_2).abs() < 1e-9);
}

#[tokio::test]
async fn rates_tolerate_missing_fields() {
    let s = MockServer::start().await;
    Mock::given(path("/poe2/Leagues"))
        .respond_with(ResponseTemplate::new(200).set_body_raw(
            r#"[{"Value":"X","DivinePrice":0},{"Value":"Y","DivinePrice":100,"ChaosDivinePrice":0}]"#,
            "application/json",
        ))
        .mount(&s)
        .await;
    let src = source(&s);
    let x = src.rates("X").await.unwrap();
    assert_eq!(x.base, "exalted");
    assert!(x.rates.is_empty());
    let y = src.rates("Y").await.unwrap();
    assert_eq!(y.rates.len(), 1);
}

#[tokio::test]
async fn base_url_with_path_prefix() {
    let s = server().await;
    // Trailing slash on the base URL must not produce `//poe2`.
    let src = Poe2ScoutSource::new(Url::parse(&format!("{}/", s.uri())).unwrap(), UA.into());
    assert_eq!(src.base_url.path(), "/");
    assert_eq!(src.user_agent, UA);
    price_of(&src, currency("Exalted Orb")).await;
}
