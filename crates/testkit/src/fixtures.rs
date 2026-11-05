//! Wiremock fixture helpers.
//!
//! Recorded trade2 JSON lives in `crates/trade/tests/fixtures/` (owned by the trade crate).
//! [`fixture`] reads from there first and falls back to this crate's own minimal samples in
//! `crates/testkit/fixtures/`, so testkit (and anything built on it) works even before the
//! recorded set exists.
//!
//! - [`mount_trade_fixtures`] mounts static `leagues` / `search` / `fetch` responses.
//! - [`TradeMock`] is a programmable trade2 API: chosen prices, listing counts and 429 injection.
use std::fmt;
use std::path::{Path, PathBuf};
use std::sync::Arc;

use serde_json::{Value, json};
use wiremock::matchers::{method, path, path_regex};
use wiremock::{Mock, MockServer, Request, Respond, ResponseTemplate};

/// API path prefix of the trade2 API (relative to `TradeConfig::base_url`).
pub const LEAGUES_PATH: &str = "/api/trade2/data/leagues";
pub const SEARCH_PREFIX: &str = "/api/trade2/search/poe2/";
pub const FETCH_PREFIX: &str = "/api/trade2/fetch/";

/// Recorded fixtures of the trade crate.
pub fn trade_fixtures_dir() -> PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR")).join("../trade/tests/fixtures")
}

/// Testkit's own minimal sample fixtures.
pub fn testkit_fixtures_dir() -> PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR")).join("fixtures")
}

#[derive(Debug)]
pub enum FixtureError {
    NotFound { name: String, searched: Vec<PathBuf> },
    Io { path: PathBuf, error: std::io::Error },
    Json { path: PathBuf, error: serde_json::Error },
}

impl fmt::Display for FixtureError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::NotFound { name, searched } => {
                write!(f, "fixture `{name}` not found; looked in:")?;
                for p in searched {
                    write!(f, " {}", p.display())?;
                }
                Ok(())
            }
            Self::Io { path, error } => write!(f, "reading fixture {}: {error}", path.display()),
            Self::Json { path, error } => write!(f, "fixture {} is not valid JSON: {error}", path.display()),
        }
    }
}

impl std::error::Error for FixtureError {}

fn read_json(path: &Path) -> Result<Value, FixtureError> {
    let text = std::fs::read_to_string(path).map_err(|error| FixtureError::Io { path: path.to_owned(), error })?;
    serde_json::from_str(&text).map_err(|error| FixtureError::Json { path: path.to_owned(), error })
}

/// Load a fixture by name (with or without `.json`) from `dirs` in order.
pub fn try_fixture_in(dirs: &[PathBuf], name: &str) -> Result<Value, FixtureError> {
    let file =
        if Path::new(name).extension().is_some_and(|e| e == "json") { name.to_owned() } else { format!("{name}.json") };
    let mut searched = Vec::new();
    for dir in dirs {
        let p = dir.join(&file);
        if p.is_file() {
            return read_json(&p);
        }
        searched.push(p);
    }
    Err(FixtureError::NotFound { name: name.to_owned(), searched })
}

/// Load a fixture: `crates/trade/tests/fixtures/<name>.json`, else `crates/testkit/fixtures/<name>.json`.
pub fn try_fixture(name: &str) -> Result<Value, FixtureError> {
    try_fixture_in(&[trade_fixtures_dir(), testkit_fixtures_dir()], name)
}

/// Like [`try_fixture`], panicking with a readable message (tests only).
pub fn fixture(name: &str) -> Value {
    try_fixture(name).unwrap_or_else(|e| panic!("{e}"))
}

/// Load one of testkit's own samples (never the recorded set). Stable for assertions.
pub fn testkit_fixture(name: &str) -> Value {
    try_fixture_in(&[testkit_fixtures_dir()], name).unwrap_or_else(|e| panic!("{e}"))
}

fn rate_limit_headers(t: ResponseTemplate, penalised: bool) -> ResponseTemplate {
    t.insert_header("X-Rate-Limit-Policy", "trade-search-request-limit")
        .insert_header("X-Rate-Limit-Rules", "Ip")
        .insert_header("X-Rate-Limit-Ip", "5:10:60,15:60:300")
        .insert_header("X-Rate-Limit-Ip-State", if penalised { "6:10:60,6:60:0" } else { "1:10:0,1:60:0" })
}

fn ok_json(body: Value) -> ResponseTemplate {
    rate_limit_headers(ResponseTemplate::new(200).set_body_json(body), false)
}

/// Mount static `leagues`, `search` and `fetch` fixtures (see [`fixture`] for lookup order).
pub async fn mount_trade_fixtures(server: &MockServer) {
    Mock::given(method("GET")).and(path(LEAGUES_PATH)).respond_with(ok_json(fixture("leagues"))).mount(server).await;
    Mock::given(method("POST"))
        .and(path_regex(format!("^{SEARCH_PREFIX}[^/]+$")))
        .respond_with(ok_json(fixture("search")))
        .mount(server)
        .await;
    Mock::given(method("GET"))
        .and(path_regex(format!("^{FETCH_PREFIX}[^/]+$")))
        .respond_with(ok_json(fixture("fetch")))
        .mount(server)
        .await;
}

/// One listing served by [`TradeMock`].
#[derive(Debug, Clone, PartialEq)]
pub struct MockListing {
    pub amount: f64,
    pub currency: String,
    pub account: String,
    pub online: bool,
}

/// Which endpoint a 429 is injected on.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Endpoint {
    Leagues,
    Search,
    Fetch,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
struct Injection {
    endpoint: Endpoint,
    times: u64,
    retry_after_secs: u64,
}

/// Programmable trade2 API for desktop / e2e tests.
///
/// ```no_run
/// # async fn demo() {
/// use kalandra_testkit::fixtures::{Endpoint, TradeMock};
/// let server = wiremock::MockServer::start().await;
/// TradeMock::new().price(2.0, "divine").price(3.0, "divine").rate_limited(Endpoint::Search, 1, 2).mount(&server).await;
/// // point TradeConfig::base_url (or KALANDRA_TRADE_BASE_URL) at server.uri()
/// # }
/// ```
#[derive(Debug, Clone)]
pub struct TradeMock {
    leagues: Vec<String>,
    listings: Vec<MockListing>,
    total: Option<u64>,
    search_id: String,
    injections: Vec<Injection>,
}

impl Default for TradeMock {
    fn default() -> Self {
        Self::new()
    }
}

impl TradeMock {
    /// Leagues `Standard` + `Hardcore`, no listings.
    pub fn new() -> Self {
        Self {
            leagues: vec!["Standard".into(), "Hardcore".into()],
            listings: Vec::new(),
            total: None,
            search_id: "kalandraMock".into(),
            injections: Vec::new(),
        }
    }

    /// Replace the league list.
    #[must_use]
    pub fn leagues(mut self, leagues: &[&str]) -> Self {
        self.leagues = leagues.iter().map(|s| (*s).to_owned()).collect();
        self
    }

    /// Add an online listing priced `amount currency`.
    #[must_use]
    pub fn price(mut self, amount: f64, currency: &str) -> Self {
        let n = self.listings.len() + 1;
        self.listings.push(MockListing {
            amount,
            currency: currency.to_owned(),
            account: format!("seller#{n:04}"),
            online: true,
        });
        self
    }

    /// Add several listings.
    #[must_use]
    pub fn prices(self, prices: &[(f64, &str)]) -> Self {
        prices.iter().fold(self, |m, (a, c)| m.price(*a, c))
    }

    /// Add a fully specified listing.
    #[must_use]
    pub fn listing(mut self, listing: MockListing) -> Self {
        self.listings.push(listing);
        self
    }

    /// Override the `total` reported by search (defaults to the listing count).
    #[must_use]
    pub fn total(mut self, total: u64) -> Self {
        self.total = Some(total);
        self
    }

    /// Answer the first `times` calls to `endpoint` with `429` + `Retry-After`.
    #[must_use]
    pub fn rate_limited(mut self, endpoint: Endpoint, times: u64, retry_after_secs: u64) -> Self {
        self.injections.push(Injection { endpoint, times, retry_after_secs });
        self
    }

    /// Listing id for the `i`-th listing (64 hex chars like the real API).
    pub fn listing_id(i: usize) -> String {
        format!("{:064x}", i + 1)
    }

    pub fn leagues_json(&self) -> Value {
        json!({
            "result": self.leagues.iter().map(|l| json!({ "id": l, "realm": "poe2", "text": l })).collect::<Vec<_>>()
        })
    }

    pub fn search_json(&self) -> Value {
        let ids: Vec<String> = (0..self.listings.len()).map(Self::listing_id).collect();
        json!({
            "id": self.search_id,
            "complexity": 4,
            "result": ids,
            "total": self.total.unwrap_or(self.listings.len() as u64),
        })
    }

    /// Fetch response for the comma-separated `ids` (unknown ids yield `null`, like the API).
    pub fn fetch_json(&self, ids: &str) -> Value {
        let result: Vec<Value> = ids
            .split(',')
            .filter(|s| !s.is_empty())
            .map(|id| {
                let idx = (0..self.listings.len()).find(|i| Self::listing_id(*i) == id);
                idx.map_or(Value::Null, |i| {
                    let l = &self.listings[i];
                    json!({
                        "id": id,
                        "listing": {
                            "method": "psapi",
                            "indexed": "2026-11-01T10:00:00Z",
                            "account": {
                                "name": l.account,
                                "online": if l.online { json!({ "league": self.leagues.first() }) } else { Value::Null },
                                "lastCharacterName": "MockCharacter",
                            },
                            "price": { "type": "~price", "amount": l.amount, "currency": l.currency },
                        },
                        "item": { "name": "", "typeLine": "Mock Item", "baseType": "Mock Item", "frameType": 0 },
                    })
                })
            })
            .collect();
        json!({ "result": result })
    }

    /// Mount all routes on `server`.
    pub async fn mount(self, server: &MockServer) {
        for inj in &self.injections {
            let mock = match inj.endpoint {
                Endpoint::Leagues => Mock::given(method("GET")).and(path(LEAGUES_PATH)),
                Endpoint::Search => Mock::given(method("POST")).and(path_regex(format!("^{SEARCH_PREFIX}[^/]+$"))),
                Endpoint::Fetch => Mock::given(method("GET")).and(path_regex(format!("^{FETCH_PREFIX}[^/]+$"))),
            };
            let resp = rate_limit_headers(ResponseTemplate::new(429), true)
                .insert_header("Retry-After", inj.retry_after_secs.to_string().as_str())
                .set_body_json(json!({ "error": { "code": 3, "message": "Rate limit exceeded" } }));
            mock.respond_with(resp).up_to_n_times(inj.times).with_priority(1).mount(server).await;
        }
        let me = Arc::new(self);
        Mock::given(method("GET")).and(path(LEAGUES_PATH)).respond_with(ok_json(me.leagues_json())).mount(server).await;
        Mock::given(method("POST"))
            .and(path_regex(format!("^{SEARCH_PREFIX}[^/]+$")))
            .respond_with(ok_json(me.search_json()))
            .mount(server)
            .await;
        Mock::given(method("GET"))
            .and(path_regex(format!("^{FETCH_PREFIX}[^/]+$")))
            .respond_with(FetchResponder(me))
            .mount(server)
            .await;
    }

    /// Start a server on `addr` (e.g. `127.0.0.1:0` or a fixed port for an external app) and mount.
    pub async fn serve(self, addr: &str) -> std::io::Result<MockServer> {
        let listener = std::net::TcpListener::bind(addr)?;
        let server = MockServer::builder().listener(listener).start().await;
        self.mount(&server).await;
        Ok(server)
    }
}

struct FetchResponder(Arc<TradeMock>);

impl Respond for FetchResponder {
    fn respond(&self, request: &Request) -> ResponseTemplate {
        let ids = request.url.path().strip_prefix(FETCH_PREFIX).unwrap_or_default();
        ok_json(self.0.fetch_json(ids))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    async fn get(url: &str) -> (u16, Value, reqwest::header::HeaderMap) {
        let r = reqwest::get(url).await.unwrap();
        let s = r.status().as_u16();
        let h = r.headers().clone();
        (s, r.json().await.unwrap_or(Value::Null), h)
    }

    async fn post(url: &str) -> (u16, Value, reqwest::header::HeaderMap) {
        let r = reqwest::Client::new().post(url).json(&json!({"query": {}})).send().await.unwrap();
        let s = r.status().as_u16();
        let h = r.headers().clone();
        (s, r.json().await.unwrap_or(Value::Null), h)
    }

    #[test]
    fn testkit_samples_load() {
        assert_eq!(testkit_fixture("leagues")["result"].as_array().unwrap().len(), 4);
        assert_eq!(testkit_fixture("search.json")["total"], 3);
        assert_eq!(testkit_fixture("fetch")["result"][1]["listing"]["price"]["amount"], 2);
        // falls back to testkit samples (or the recorded set) by name
        assert!(fixture("leagues")["result"].is_array());
    }

    #[test]
    fn missing_fixture_reports_paths() {
        let err = try_fixture("definitely-not-a-fixture").unwrap_err();
        let msg = err.to_string();
        assert!(msg.contains("definitely-not-a-fixture"), "{msg}");
        assert!(msg.contains("trade/tests/fixtures"), "{msg}");
        assert!(msg.contains("testkit/fixtures"), "{msg}");
    }

    #[test]
    #[should_panic(expected = "not found")]
    fn fixture_panics_readably() {
        let _ = fixture("nope");
    }

    #[test]
    fn invalid_json_and_io_errors() {
        let dir = std::env::temp_dir().join(format!("kalandra-testkit-{}", std::process::id()));
        std::fs::create_dir_all(dir.join("dir.json")).unwrap();
        std::fs::write(dir.join("bad.json"), "{nope").unwrap();
        let e = try_fixture_in(std::slice::from_ref(&dir), "bad").unwrap_err();
        assert!(matches!(e, FixtureError::Json { .. }));
        assert!(e.to_string().contains("not valid JSON"));
        let e = read_json(&dir.join("dir.json")).unwrap_err();
        assert!(matches!(e, FixtureError::Io { .. }));
        assert!(e.to_string().contains("reading fixture"));
        std::fs::remove_dir_all(dir).unwrap();
    }

    #[tokio::test]
    async fn mounts_static_fixtures() {
        let server = MockServer::start().await;
        mount_trade_fixtures(&server).await;
        let (s, v, h) = get(&format!("{}{LEAGUES_PATH}", server.uri())).await;
        assert_eq!(s, 200);
        assert!(v["result"].is_array());
        assert_eq!(h["x-rate-limit-rules"], "Ip");
        let (s, v, _) = post(&format!("{}{SEARCH_PREFIX}Standard", server.uri())).await;
        assert_eq!(s, 200);
        assert!(v["result"].is_array());
        let (s, v, _) = get(&format!("{}{FETCH_PREFIX}abc,def?query=q", server.uri())).await;
        assert_eq!(s, 200);
        assert!(v["result"].is_array());
    }

    #[tokio::test]
    async fn trade_mock_serves_prices_by_id() {
        let server = MockServer::start().await;
        TradeMock::new()
            .leagues(&["Standard"])
            .prices(&[(1.0, "divine"), (2.5, "exalted")])
            .listing(MockListing { amount: 9.0, currency: "chaos".into(), account: "x".into(), online: false })
            .total(120)
            .mount(&server)
            .await;
        let (_, v, _) = get(&format!("{}{LEAGUES_PATH}", server.uri())).await;
        assert_eq!(v["result"][0]["id"], "Standard");
        let (_, search, _) = post(&format!("{}{SEARCH_PREFIX}Standard", server.uri())).await;
        assert_eq!(search["total"], 120);
        let ids: Vec<&str> = search["result"].as_array().unwrap().iter().map(|v| v.as_str().unwrap()).collect();
        assert_eq!(ids.len(), 3);
        assert_eq!(ids[0].len(), 64);
        let url = format!("{}{FETCH_PREFIX}{},{},unknown?query={}", server.uri(), ids[1], ids[2], search["id"]);
        let (s, f, _) = get(&url).await;
        assert_eq!(s, 200);
        assert_eq!(f["result"][0]["listing"]["price"]["amount"], 2.5);
        assert_eq!(f["result"][0]["listing"]["price"]["currency"], "exalted");
        assert_eq!(f["result"][1]["listing"]["account"]["online"], Value::Null);
        assert_eq!(f["result"][2], Value::Null);
    }

    #[tokio::test]
    async fn trade_mock_injects_429_then_recovers() {
        let server = MockServer::start().await;
        TradeMock::default()
            .price(1.0, "divine")
            .rate_limited(Endpoint::Search, 2, 7)
            .rate_limited(Endpoint::Leagues, 1, 1)
            .rate_limited(Endpoint::Fetch, 1, 3)
            .mount(&server)
            .await;
        let search = format!("{}{SEARCH_PREFIX}Standard", server.uri());
        for _ in 0..2 {
            let (s, v, h) = post(&search).await;
            assert_eq!(s, 429);
            assert_eq!(h["retry-after"], "7");
            assert_eq!(v["error"]["code"], 3);
        }
        assert_eq!(post(&search).await.0, 200);
        let leagues = format!("{}{LEAGUES_PATH}", server.uri());
        assert_eq!(get(&leagues).await.0, 429);
        assert_eq!(get(&leagues).await.0, 200);
        let fetch = format!("{}{FETCH_PREFIX}{}", server.uri(), TradeMock::listing_id(0));
        assert_eq!(get(&fetch).await.0, 429);
        let (s, v, _) = get(&fetch).await;
        assert_eq!(s, 200);
        assert_eq!(v["result"][0]["listing"]["price"]["amount"], 1.0);
    }

    #[tokio::test]
    async fn serve_binds_address() {
        let server = TradeMock::new().serve("127.0.0.1:0").await.unwrap();
        let (s, v, _) = get(&format!("{}{LEAGUES_PATH}", server.uri())).await;
        assert_eq!(s, 200);
        assert_eq!(v["result"].as_array().unwrap().len(), 2);
        assert!(TradeMock::new().serve("256.0.0.1:1").await.is_err());
    }
}
