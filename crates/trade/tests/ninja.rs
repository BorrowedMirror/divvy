//! Contract tests for `PoeNinjaSource` against recorded poe.ninja fixtures.
use std::time::Duration;

use rkl_core::{
    item::{ItemCategory, ItemClass, ItemStats, ParsedItem, Rarity, Requirements},
    pricing::{Price, PriceSourceKind},
    query::{FilterSet, MiscFilters, OnlineStatus, QueryRoute},
};
use rkl_trade::{PriceRequest, PriceSource, TradeError, ninja::PoeNinjaSource};
use url::Url;
use wiremock::{
    Mock, MockServer, ResponseTemplate,
    matchers::{header, method, path, query_param},
};

const UA: &str = "rkl-test/1.0 (+ninja)";
const LEAGUE: &str = "Forbidden Rites";
const EXCHANGE: &str = "/poe2/api/economy/exchange/current/overview";
const STASH: &str = "/poe2/api/economy/stash/current/item/overview";

fn fixture(name: &str) -> String {
    std::fs::read_to_string(format!("{}/tests/fixtures/ninja/{name}", env!("CARGO_MANIFEST_DIR"))).unwrap()
}

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

fn request(item: ParsedItem) -> PriceRequest {
    PriceRequest {
        filters: FilterSet {
            league: LEAGUE.into(),
            status: OnlineStatus::Online,
            name: item.name.clone(),
            base_type: Some(item.base_type.clone()),
            category: None,
            rarity: None,
            stats: Vec::new(),
            misc: MiscFilters::default(),
            widen_pct: 10.0,
            route: QueryRoute::Exchange,
        },
        item,
    }
}

fn currency(base: &str) -> PriceRequest {
    request(item(ItemClass::StackableCurrency, ItemCategory::Currency, None, base))
}

fn unique(name: &str, base: &str) -> PriceRequest {
    request(item(ItemClass::Belt, ItemCategory::Unique, Some(name), base))
}

fn source(server: &MockServer) -> PoeNinjaSource {
    PoeNinjaSource::new(Url::parse(&server.uri()).unwrap(), UA.into())
}

async fn mount_currency(server: &MockServer, expect: u64) {
    Mock::given(method("GET"))
        .and(path(EXCHANGE))
        .and(query_param("league", LEAGUE))
        .and(query_param("type", "Currency"))
        .and(header("user-agent", UA))
        .respond_with(ResponseTemplate::new(200).set_body_string(fixture("exchange-currency.json")))
        .expect(expect)
        .mount(server)
        .await;
}

async fn mount_uniques(server: &MockServer, expect: u64) {
    Mock::given(method("GET"))
        .and(path(STASH))
        .and(query_param("league", LEAGUE))
        .and(query_param("type", "UniqueAccessories"))
        .and(header("user-agent", UA))
        .respond_with(ResponseTemplate::new(200).set_body_string(fixture("stash-unique-accessories.json")))
        .expect(expect)
        .mount(server)
        .await;
}

fn approx(p: &Price, amount: f64, currency: &str) {
    assert_eq!(p.currency, currency, "{p:?}");
    assert!((p.amount - amount).abs() < 1e-3, "{p:?} != {amount} {currency}");
}

#[tokio::test]
async fn prices_currency_with_normalisation() {
    let server = MockServer::start().await;
    mount_currency(&server, 1).await;
    let src = source(&server);
    assert_eq!(src.kind(), PriceSourceKind::PoeNinja);

    let divine = src.price(&currency("Divine Orb")).await.unwrap();
    assert_eq!(divine.source, PriceSourceKind::PoeNinja);
    approx(divine.median.as_ref().unwrap(), 1.0, "divine");
    assert_eq!(divine.min, divine.max);
    assert!(divine.listings.is_empty() && divine.trade_url.is_none());

    // 0.001_380 divine * 724.8 exalted/divine ~= 1 exalted
    let exalted = src.price(&currency("Exalted Orb")).await.unwrap();
    approx(exalted.median.as_ref().unwrap(), 0.001_380 * 724.8, "exalted");

    let alch = src.price(&currency("Orb of Alchemy")).await.unwrap();
    approx(alch.median.as_ref().unwrap(), 0.005_949 * 724.8, "exalted");
}

#[tokio::test]
async fn prices_uniques_in_divine_or_exalted() {
    let server = MockServer::start().await;
    mount_uniques(&server, 1).await;
    let src = source(&server);

    let mb = src.price(&unique("Mageblood", "Utility Belt")).await.unwrap();
    approx(mb.median.as_ref().unwrap(), 130.0, "divine");
    assert_eq!(mb.total, 7570);

    let andv = PriceRequest {
        item: item(ItemClass::Ring, ItemCategory::Unique, Some("Andvarius"), "Gold Ring"),
        ..unique("Andvarius", "Gold Ring")
    };
    let p = src.price(&andv).await.unwrap();
    // 0.07735 divine at 704.8 exalted/divine (rates in this fixture's core).
    approx(p.median.as_ref().unwrap(), 0.077_35 * 704.8, "exalted");
}

#[tokio::test]
async fn unknown_item_is_unsupported() {
    let server = MockServer::start().await;
    mount_uniques(&server, 1).await;
    let err = source(&server).price(&unique("Not A Real Belt", "Utility Belt")).await.unwrap_err();
    assert!(matches!(err, TradeError::Unsupported("poe.ninja")), "{err:?}");
}

#[tokio::test]
async fn currency_falls_through_types_until_found() {
    let server = MockServer::start().await;
    // Currency overview lacks the item, Fragments has it.
    mount_currency(&server, 1).await;
    let fragments = r#"{"core":{"primary":"divine","rates":{"exalted":700.0}},
        "lines":[{"id":"breach-splinter","primaryValue":2.5}],
        "items":[{"id":"breach-splinter","name":"Simulacrum"}]}"#;
    Mock::given(method("GET"))
        .and(path(EXCHANGE))
        .and(query_param("type", "Fragments"))
        .respond_with(ResponseTemplate::new(200).set_body_string(fragments))
        .expect(1)
        .mount(&server)
        .await;
    let p = source(&server).price(&currency("Simulacrum")).await.unwrap();
    approx(p.median.as_ref().unwrap(), 2.5, "divine");
}

#[tokio::test]
async fn supports_matrix() {
    let src = PoeNinjaSource::new(Url::parse("https://poe.ninja").unwrap(), UA.into());
    assert!(src.supports(&item(ItemClass::StackableCurrency, ItemCategory::Currency, None, "Divine Orb")));
    assert!(src.supports(&item(ItemClass::Ring, ItemCategory::Unique, Some("Andvarius"), "Gold Ring")));
    assert!(!src.supports(&item(ItemClass::Ring, ItemCategory::Unique, None, "Gold Ring")));
    assert!(src.supports(&item(ItemClass::UncutGem, ItemCategory::Gem, None, "Uncut Skill Gem")));
    assert!(src.supports(&item(ItemClass::SupportGem, ItemCategory::Gem, None, "Some Lineage Support")));
    assert!(!src.supports(&item(ItemClass::SkillGem, ItemCategory::Gem, None, "Fireball")));
    assert!(!src.supports(&item(ItemClass::Helmet, ItemCategory::Equipment, None, "Expert Hunter Hood")));

    let rare = request(item(ItemClass::Helmet, ItemCategory::Equipment, None, "Expert Hunter Hood"));
    assert!(matches!(src.price(&rare).await, Err(TradeError::Unsupported(_))));
}

#[tokio::test]
async fn server_error_maps_to_http() {
    let server = MockServer::start().await;
    Mock::given(method("GET"))
        .and(path(EXCHANGE))
        .respond_with(ResponseTemplate::new(503).set_body_string("upstream down"))
        .mount(&server)
        .await;
    match source(&server).price(&currency("Divine Orb")).await {
        Err(TradeError::Http { status, message }) => {
            assert_eq!(status, 503);
            assert_eq!(message, "upstream down");
        }
        other => panic!("expected http error, got {other:?}"),
    }
}

#[tokio::test]
async fn bad_json_maps_to_decode() {
    let server = MockServer::start().await;
    Mock::given(method("GET"))
        .and(path(STASH))
        .respond_with(ResponseTemplate::new(200).set_body_string("{\"lines\":"))
        .mount(&server)
        .await;
    let err = source(&server).price(&unique("Mageblood", "Utility Belt")).await.unwrap_err();
    assert!(matches!(err, TradeError::Decode(ref m) if m.contains("UniqueAccessories")), "{err:?}");
}

#[tokio::test]
async fn caches_overviews_per_league_and_type() {
    let server = MockServer::start().await;
    mount_currency(&server, 1).await;
    let src = source(&server).with_cache_ttl(Duration::from_secs(60));
    src.price(&currency("Divine Orb")).await.unwrap();
    src.price(&currency("Exalted Orb")).await.unwrap();
    let rates = src.rates(LEAGUE).await.unwrap();
    assert_eq!(rates.base, "divine");
    assert!((rates.rates["exalted"] - 1.0 / 724.8).abs() < 1e-12);
    assert!((rates.rates["chaos"] - 1.0 / 9.36).abs() < 1e-12);
    // `.expect(1)` on the mock is verified when the server drops.
}

#[tokio::test]
async fn transport_error_surfaces() {
    // Nothing listens on this port.
    let src = PoeNinjaSource::new(Url::parse("http://127.0.0.1:9").unwrap(), UA.into());
    assert!(matches!(src.rates(LEAGUE).await, Err(TradeError::Transport(_))));
}
