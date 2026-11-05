//! wiremock contract tests for the trade2 client, driven by recorded fixtures
//! (see docs/research/trade2.md).
use std::time::{Duration, Instant};

use kalandra_core::{
    pricing::PriceSourceKind,
    query::{FilterSet, MiscFilters, OnlineStatus, QueryRoute, TradeQuery},
};
use kalandra_trade::{
    PriceRequest, PriceSource, TradeClient, TradeConfig, TradeError,
    client::{FETCH_CHUNK, exchange_body, exchange_listing, fetch_listing},
    error::TradeErrorInfo,
    ratelimit::SystemClock,
    types::{ExchangeResponse, FetchEntry},
};
use pretty_assertions::assert_eq;
use serde_json::json;
use url::Url;
use wiremock::{
    Mock, MockServer, ResponseTemplate,
    matchers::{body_json, header, method, path, query_param},
};

const UA: &str = "kalandra-test/0.0 (+https://example.invalid)";

fn fixture(name: &str) -> String {
    std::fs::read_to_string(format!("{}/tests/fixtures/{name}", env!("CARGO_MANIFEST_DIR"))).expect("fixture")
}

fn json_fixture(name: &str) -> serde_json::Value {
    serde_json::from_str(&fixture(name)).expect("fixture json")
}

/// Response carrying the recorded header set `headers/{name}.txt`.
fn with_headers(mut resp: ResponseTemplate, name: &str) -> ResponseTemplate {
    for line in fixture(&format!("headers/{name}.txt")).lines() {
        let (k, v) = line.split_once(':').expect("header line");
        resp = resp.insert_header(k.trim(), v.trim());
    }
    resp
}

fn config(server: &MockServer) -> TradeConfig {
    TradeConfig {
        base_url: Url::parse(&server.uri()).unwrap(),
        user_agent: UA.into(),
        poesessid: None,
        fetch_count: 20,
        cache_ttl: Duration::from_secs(60),
    }
}

fn client(server: &MockServer) -> TradeClient {
    TradeClient::new(config(server), Box::new(SystemClock)).unwrap()
}

fn query() -> TradeQuery {
    TradeQuery {
        query: json!({"status":{"option":"online"},"name":"Andvarius","type":"Gold Ring","stats":[{"type":"and","filters":[]}]}),
        sort: json!({"price":"asc"}),
    }
}

fn filters(route: QueryRoute, base: &str) -> FilterSet {
    FilterSet {
        league: "Standard".into(),
        status: OnlineStatus::Online,
        name: None,
        base_type: Some(base.into()),
        category: None,
        rarity: None,
        stats: Vec::new(),
        misc: MiscFilters::default(),
        widen_pct: 10.0,
        route,
    }
}

fn search_ok() -> ResponseTemplate {
    with_headers(ResponseTemplate::new(200).set_body_string(fixture("search.json")), "search.200")
}

fn fetch_ok() -> ResponseTemplate {
    with_headers(ResponseTemplate::new(200).set_body_string(fixture("fetch.json")), "fetch.200")
}

#[tokio::test]
async fn leagues_sends_user_agent_and_decodes() {
    let server = MockServer::start().await;
    Mock::given(method("GET"))
        .and(path("/api/trade2/data/leagues"))
        .and(header("user-agent", UA))
        .respond_with(ResponseTemplate::new(200).set_body_string(fixture("leagues.json")))
        .expect(1)
        .mount(&server)
        .await;
    let c = client(&server);
    let leagues = c.leagues().await.unwrap();
    assert_eq!(leagues.len(), 6);
    assert_eq!(leagues[4].id, "Standard");
    assert_eq!(leagues[4].realm.as_deref(), Some("poe2"));
    // second call is served from the cache (mock expects exactly one hit)
    assert_eq!(c.leagues().await.unwrap().len(), 6);
}

#[tokio::test]
async fn poesessid_is_sent_as_cookie() {
    let server = MockServer::start().await;
    Mock::given(method("GET"))
        .and(path("/api/trade2/data/stats"))
        .and(header("cookie", "POESESSID=abc123"))
        .respond_with(ResponseTemplate::new(200).set_body_string(fixture("stats.sample.json")))
        .expect(1)
        .mount(&server)
        .await;
    let cfg = TradeConfig { poesessid: Some("abc123".into()), ..config(&server) };
    let c = TradeClient::new(cfg, Box::new(SystemClock)).unwrap();
    let stats = c.data("stats").await.unwrap();
    assert_eq!(stats["result"][0]["id"], "pseudo");
}

#[tokio::test]
async fn empty_poesessid_sends_no_cookie() {
    let server = MockServer::start().await;
    Mock::given(path("/api/trade2/data/items"))
        .respond_with(ResponseTemplate::new(200).set_body_string(fixture("items.sample.json")))
        .mount(&server)
        .await;
    let cfg = TradeConfig { poesessid: Some(String::new()), ..config(&server) };
    let c = TradeClient::new(cfg, Box::new(SystemClock)).unwrap();
    c.data("items").await.unwrap();
    let reqs = server.received_requests().await.unwrap();
    assert!(reqs[0].headers.get("cookie").is_none());
}

#[test]
fn invalid_config_is_rejected() {
    let base = TradeConfig::default();
    assert_eq!(base.base_url.as_str(), "https://www.pathofexile.com/");
    assert!(base.user_agent.starts_with("kalandra/"));
    let bad_url = TradeConfig { base_url: Url::parse("mailto:a@b.c").unwrap(), ..base.clone() };
    assert!(matches!(TradeClient::new(bad_url, Box::new(SystemClock)), Err(TradeError::Config(_))));
    let bad_cookie = TradeConfig { poesessid: Some("a\nb".into()), ..base };
    let err = TradeClient::new(bad_cookie, Box::new(SystemClock)).unwrap_err();
    assert!(err.to_string().contains("POESESSID"), "{err}");
}

#[tokio::test]
async fn search_posts_body_to_realm_path_and_caches() {
    let server = MockServer::start().await;
    let q = query();
    Mock::given(method("POST"))
        .and(path("/api/trade2/search/poe2/Standard"))
        .and(header("user-agent", UA))
        .and(header("content-type", "application/json"))
        .and(body_json(&q))
        .respond_with(search_ok())
        .expect(1)
        .mount(&server)
        .await;
    let c = client(&server);
    let r = c.search("Standard", &q).await.unwrap();
    assert_eq!(r.total, 11);
    assert_eq!(r.result.len(), 11);
    assert_eq!(r.complexity, Some(6));
    let again = c.search("Standard", &q).await.unwrap();
    assert_eq!(again, r);
    assert_eq!(c.limiter().policy_name("search").as_deref(), Some("trade-search-request-limit"));
}

#[tokio::test]
async fn zero_ttl_disables_cache() {
    let server = MockServer::start().await;
    Mock::given(method("POST")).respond_with(search_ok()).expect(2).mount(&server).await;
    let cfg = TradeConfig { cache_ttl: Duration::ZERO, ..config(&server) };
    let c = TradeClient::new(cfg, Box::new(SystemClock)).unwrap();
    c.search("Standard", &query()).await.unwrap();
    c.search("Standard", &query()).await.unwrap();
}

#[tokio::test]
async fn league_with_spaces_is_percent_encoded() {
    let server = MockServer::start().await;
    Mock::given(method("POST"))
        .and(path("/api/trade2/search/poe2/Forbidden%20Rites"))
        .respond_with(search_ok())
        .expect(1)
        .mount(&server)
        .await;
    let c = client(&server);
    let r = c.search("Forbidden Rites", &query()).await.unwrap();
    assert_eq!(
        c.trade_url("Forbidden Rites", &r.id).as_str(),
        format!("{}/trade2/search/poe2/Forbidden%20Rites/{}", server.uri(), r.id)
    );
}

#[test]
fn trade_urls_match_site_format() {
    let c = TradeClient::new(TradeConfig::default(), Box::new(SystemClock)).unwrap();
    assert_eq!(c.trade_url("Standard", "abc").as_str(), "https://www.pathofexile.com/trade2/search/poe2/Standard/abc");
    assert_eq!(
        c.exchange_url("HC Runes of Aldur", "xyz").as_str(),
        "https://www.pathofexile.com/trade2/exchange/poe2/HC%20Runes%20of%20Aldur/xyz"
    );
}

#[tokio::test]
async fn fetch_chunks_ids_by_ten_and_skips_nulls() {
    let server = MockServer::start().await;
    let ids: Vec<String> = (0..12).map(|i| format!("id{i}")).collect();
    let first = ids[..FETCH_CHUNK].join(",");
    let second = ids[FETCH_CHUNK..].join(",");
    Mock::given(method("GET"))
        .and(path(format!("/api/trade2/fetch/{first}")))
        .and(query_param("query", "Q1"))
        .and(query_param("realm", "poe2"))
        .respond_with(fetch_ok())
        .expect(1)
        .mount(&server)
        .await;
    let mut tail = json_fixture("fetch.json");
    let one = tail["result"][0].clone();
    tail["result"] = json!([one, null]);
    Mock::given(method("GET"))
        .and(path(format!("/api/trade2/fetch/{second}")))
        .respond_with(ResponseTemplate::new(200).set_body_json(tail))
        .expect(1)
        .mount(&server)
        .await;
    let c = client(&server);
    let entries = c.fetch(&ids, "Q1").await.unwrap();
    assert_eq!(entries.len(), 7);
    // cached per chunk
    assert_eq!(c.fetch(&ids, "Q1").await.unwrap().len(), 7);
}

#[tokio::test]
async fn search_listings_maps_fetch_entries() {
    let server = MockServer::start().await;
    Mock::given(method("POST")).respond_with(search_ok()).mount(&server).await;
    Mock::given(method("GET")).respond_with(fetch_ok()).expect(1).mount(&server).await;
    let cfg = TradeConfig { fetch_count: 6, ..config(&server) };
    let c = TradeClient::new(cfg, Box::new(SystemClock)).unwrap();
    let page = c.search_listings("Standard", &query()).await.unwrap();
    assert_eq!(page.total, 11);
    assert_eq!(page.listings.len(), 6);
    let l = &page.listings[0];
    assert_eq!(l.seller.as_deref(), Some("Seller1#0001"));
    assert!((l.price.amount - 1.0).abs() < f64::EPSILON);
    assert_eq!(l.price.currency, "divine");
    assert!(l.online);
    assert_eq!(l.item_level, Some(40));
    assert_eq!(l.indexed.as_deref(), Some("2025-01-13T21:21:29Z"));
    assert!(page.trade_url.as_str().ends_with(&format!("/trade2/search/poe2/Standard/{}", page.search_id)));
    insta::assert_json_snapshot!("search_listings", page.listings);
}

#[tokio::test]
async fn empty_search_skips_fetch() {
    let server = MockServer::start().await;
    Mock::given(method("POST"))
        .respond_with(ResponseTemplate::new(200).set_body_json(json!({"id":"E","result":[],"total":0})))
        .mount(&server)
        .await;
    Mock::given(method("GET")).respond_with(fetch_ok()).expect(0).mount(&server).await;
    let page = client(&server).search_listings("Standard", &query()).await.unwrap();
    assert_eq!(page.total, 0);
    assert!(page.listings.is_empty());
}

#[test]
fn listing_mapping_edge_cases() {
    let mut e: FetchEntry = serde_json::from_value(json_fixture("fetch.json")["result"][0].clone()).unwrap();
    e.listing.account.as_mut().unwrap().online = None;
    e.item = json!({"stackSize": 7});
    let l = fetch_listing(&e).unwrap();
    assert!(!l.online);
    assert_eq!(l.stack_size, Some(7));
    assert_eq!(l.item_level, None);
    e.listing.price = None;
    assert!(fetch_listing(&e).is_none());

    let x: ExchangeResponse = serde_json::from_str(&fixture("exchange.json")).unwrap();
    let mut entry = x.result[0].clone();
    entry.listing.offers[0].item.amount = 0.0;
    assert!(exchange_listing(&entry).is_none());
    entry.listing.offers.clear();
    assert!(exchange_listing(&entry).is_none());
}

#[tokio::test]
async fn too_many_requests_maps_retry_after_and_blocks_further_calls() {
    let server = MockServer::start().await;
    Mock::given(method("POST"))
        .respond_with(with_headers(ResponseTemplate::new(429).set_body_string("{}"), "search.429"))
        .expect(1)
        .mount(&server)
        .await;
    let c = client(&server);
    let err = c.search("Standard", &query()).await.unwrap_err();
    let TradeError::RateLimited { retry_after } = err else { panic!("expected rate limit, got {err:?}") };
    assert!(retry_after > Duration::from_secs(59) && retry_after <= Duration::from_secs(60), "{retry_after:?}");
    let info = TradeErrorInfo::from(&err);
    assert_eq!(info.kind, "rate_limited");
    assert!(info.retry_after_ms.unwrap() > 59_000);
    // the limiter now refuses locally without touching the server (expect(1) above)
    let again = c.search("Standard", &query()).await.unwrap_err();
    assert!(matches!(again, TradeError::RateLimited { .. }));
}

#[tokio::test]
async fn rate_limit_headers_throttle_subsequent_calls() {
    let server = MockServer::start().await;
    Mock::given(method("POST"))
        .respond_with(
            search_ok().insert_header("x-rate-limit-ip", "1:60:60").insert_header("x-rate-limit-ip-state", "1:60:0"),
        )
        .expect(1)
        .mount(&server)
        .await;
    let c = client(&server);
    c.search("Standard", &query()).await.unwrap();
    // A different query is not cached; the 1-per-60s rule makes it wait ~60s, which is
    // beyond MAX_QUEUE_WAIT, so it fails fast without a request.
    let other = TradeQuery { sort: json!({"price":"desc"}), ..query() };
    let err = c.search("Standard", &other).await.unwrap_err();
    let TradeError::RateLimited { retry_after } = err else { panic!("{err:?}") };
    assert!(retry_after > Duration::from_secs(50));
    // Refusals record nothing: retrying does not push the next free slot further out.
    for _ in 0..3 {
        let TradeError::RateLimited { retry_after: again } = c.search("Standard", &other).await.unwrap_err() else {
            panic!("expected rate limit")
        };
        assert!(again <= retry_after, "{again:?} > {retry_after:?}");
    }
}

#[tokio::test]
async fn cancelled_waits_release_their_slot() {
    let server = MockServer::start().await;
    Mock::given(method("POST"))
        .respond_with(
            search_ok().insert_header("x-rate-limit-ip", "1:2:0").insert_header("x-rate-limit-ip-state", "1:2:0"),
        )
        .expect(1)
        .mount(&server)
        .await;
    let c = client(&server);
    c.search("Standard", &query()).await.unwrap();
    let other = TradeQuery { sort: json!({"price":"desc"}), ..query() };
    // The caller gives up while the request is queued behind the limiter.
    let abandoned = tokio::time::timeout(Duration::from_millis(50), c.search("Standard", &other)).await;
    assert!(abandoned.is_err(), "request should still have been waiting");
    // Its slot was handed back: the next request waits for one padded window (2.25s),
    // not two (~4.5s).
    let next = c.limiter().try_reserve("search", Duration::ZERO).unwrap_err();
    assert!(next <= Duration::from_millis(2_250), "{next:?}");
}

#[tokio::test]
async fn data_endpoints_are_throttled_before_any_headers() {
    let server = MockServer::start().await;
    Mock::given(path("/api/trade2/data/stats"))
        .respond_with(ResponseTemplate::new(200).set_body_string(fixture("stats.sample.json")))
        .mount(&server)
        .await;
    let c =
        TradeClient::new(TradeConfig { cache_ttl: Duration::ZERO, ..config(&server) }, Box::new(SystemClock)).unwrap();
    for _ in 0..5 {
        c.data("stats").await.unwrap();
    }
    // Seeded 5:10 rule: the sixth call inside 10s would queue ~10.5s > MAX_QUEUE_WAIT.
    let err = c.data("stats").await.unwrap_err();
    assert!(matches!(err, TradeError::RateLimited { .. }), "{err:?}");
}

#[tokio::test]
async fn short_waits_are_slept_through() {
    let server = MockServer::start().await;
    Mock::given(method("POST"))
        .respond_with(
            search_ok().insert_header("x-rate-limit-ip", "1:1:0").insert_header("x-rate-limit-ip-state", "1:1:0"),
        )
        .expect(2)
        .mount(&server)
        .await;
    let c = client(&server);
    c.search("Standard", &query()).await.unwrap();
    let start = Instant::now();
    let other = TradeQuery { sort: json!({"price":"desc"}), ..query() };
    c.search("Standard", &other).await.unwrap();
    assert!(start.elapsed() >= Duration::from_millis(900), "{:?}", start.elapsed());
}

#[tokio::test]
async fn server_errors_map_to_http() {
    let server = MockServer::start().await;
    Mock::given(path("/api/trade2/data/leagues"))
        .respond_with(ResponseTemplate::new(503).set_body_json(json!({"error":{"code":0,"message":"Maintenance"}})))
        .mount(&server)
        .await;
    Mock::given(path("/api/trade2/data/stats"))
        .respond_with(ResponseTemplate::new(500).set_body_string("upstream exploded"))
        .mount(&server)
        .await;
    let c = client(&server);
    let err = c.leagues().await.unwrap_err();
    assert!(matches!(&err, TradeError::Http { status: 503, message } if message == "Maintenance"), "{err:?}");
    assert_eq!(TradeErrorInfo::from(&err).kind, "http");
    let err = c.data("stats").await.unwrap_err();
    assert!(matches!(&err, TradeError::Http { status: 500, message } if message == "upstream exploded"), "{err:?}");
}

#[tokio::test]
async fn invalid_json_maps_to_decode() {
    let server = MockServer::start().await;
    Mock::given(method("POST")).respond_with(ResponseTemplate::new(200).set_body_string("<html>")).mount(&server).await;
    Mock::given(path("/api/trade2/data/leagues"))
        .respond_with(ResponseTemplate::new(200).set_body_json(json!({"result": 5})))
        .mount(&server)
        .await;
    let c = client(&server);
    let err = c.search("Standard", &query()).await.unwrap_err();
    assert!(matches!(err, TradeError::Decode(_)), "{err:?}");
    assert_eq!(TradeErrorInfo::from(&err).kind, "decode");
    assert!(matches!(c.leagues().await.unwrap_err(), TradeError::Decode(_)));
}

#[tokio::test]
async fn transport_errors_are_reported() {
    // A server that hangs up on every connection. (Binding then releasing a port is
    // racy: a concurrently started wiremock server can grab it.)
    let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
    let port = listener.local_addr().unwrap().port();
    tokio::spawn(async move {
        while let Ok((sock, _)) = listener.accept().await {
            drop(sock);
        }
    });
    let cfg =
        TradeConfig { base_url: Url::parse(&format!("http://127.0.0.1:{port}")).unwrap(), ..TradeConfig::default() };
    let c = TradeClient::new(cfg, Box::new(SystemClock)).unwrap();
    let err = c.leagues().await.unwrap_err();
    assert!(matches!(err, TradeError::Transport(_)), "{err:?}");
    assert_eq!(TradeErrorInfo::from(&err).kind, "transport");
}

#[test]
fn exchange_body_shape() {
    assert_eq!(
        exchange_body("divine", OnlineStatus::Securable),
        json!({"query":{"status":{"option":"online"},"have":["exalted"],"want":["divine"]},"sort":{"have":"asc"},"engine":"new"})
    );
    assert_eq!(exchange_body("chaos", OnlineStatus::Any)["query"]["have"], json!(["exalted", "divine"]));
    assert_eq!(exchange_body("chaos", OnlineStatus::Any)["query"]["status"]["option"], "any");
    assert_eq!(exchange_body("exalted", OnlineStatus::Online)["query"]["have"], json!(["divine"]));
}

#[tokio::test]
async fn exchange_route_resolves_static_id_and_maps_offers() {
    let server = MockServer::start().await;
    Mock::given(path("/api/trade2/data/static"))
        .respond_with(ResponseTemplate::new(200).set_body_string(fixture("static.sample.json")))
        .expect(1)
        .mount(&server)
        .await;
    Mock::given(method("POST"))
        .and(path("/api/trade2/exchange/poe2/Standard"))
        .and(body_json(exchange_body("transmute", OnlineStatus::Online)))
        .respond_with(with_headers(
            ResponseTemplate::new(200).set_body_string(fixture("exchange.json")),
            "exchange.200",
        ))
        .expect(1)
        .mount(&server)
        .await;
    let c = client(&server);
    let f = filters(QueryRoute::Exchange, "Orb of Transmutation");
    let page = c.listings_for(&f).await.unwrap();
    assert_eq!(page.total, 3);
    assert_eq!(page.listings.len(), 3);
    assert_eq!(page.listings[0].price.currency, "exalted");
    assert_eq!(page.listings[0].stack_size, Some(2));
    assert!(!page.listings[0].online);
    assert!(page.listings[1].online);
    assert!(page.trade_url.as_str().contains("/trade2/exchange/poe2/Standard/"));
    // cached: second lookup hits neither endpoint again
    assert_eq!(c.listings_for(&f).await.unwrap(), page);
    assert_eq!(c.limiter().policy_name("exchange").as_deref(), Some("trade-exchange-request-limit"));
}

#[tokio::test]
async fn exchange_route_handles_empty_and_unknown() {
    let server = MockServer::start().await;
    Mock::given(path("/api/trade2/data/static"))
        .respond_with(ResponseTemplate::new(200).set_body_string(fixture("static.sample.json")))
        .mount(&server)
        .await;
    Mock::given(method("POST"))
        .respond_with(ResponseTemplate::new(200).set_body_string(fixture("exchange.empty.json")))
        .mount(&server)
        .await;
    let c = client(&server);
    let page = c.listings_for(&filters(QueryRoute::Exchange, "Scroll of Wisdom")).await.unwrap();
    assert_eq!(page.total, 0);
    assert!(page.listings.is_empty());
    let err = c.listings_for(&filters(QueryRoute::Exchange, "Mirror of Nothing")).await.unwrap_err();
    assert!(matches!(err, TradeError::Unsupported(_)));
    assert_eq!(c.static_id("Orb of Transmutation").await.unwrap().as_deref(), Some("transmute"));
}

#[test]
fn trade_client_is_a_price_source_for_everything() {
    let c = TradeClient::new(TradeConfig::default(), Box::new(SystemClock)).unwrap();
    assert_eq!(c.kind(), PriceSourceKind::Trade);
    assert_eq!(c.config().fetch_count, 20);
}

// Full price() path: FilterSet::to_trade_query + search + fetch + core::pricing::summarise.
// test after merging core.
#[tokio::test]
async fn price_full_search_path() {
    let server = MockServer::start().await;
    Mock::given(method("POST")).respond_with(search_ok()).mount(&server).await;
    Mock::given(method("GET")).respond_with(fetch_ok()).mount(&server).await;
    let c = client(&server);
    let mut f = filters(QueryRoute::Search, "Gold Ring");
    f.name = Some("Andvarius".into());
    let item = kalandra_core::parser::parse_item(
        "Item Class: Rings\nRarity: Unique\nAndvarius\nGold Ring\n--------\nItem Level: 40\n",
    )
    .unwrap();
    let summary = c.price(&PriceRequest { item, filters: f }).await.unwrap();
    assert_eq!(summary.source, PriceSourceKind::Trade);
    assert_eq!(summary.total, 11);
    assert!(summary.min.is_some());
    assert!(summary.trade_url.unwrap().contains("/trade2/search/poe2/Standard/"));
}
