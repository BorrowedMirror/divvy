use std::{sync::Arc, time::Duration};

use async_trait::async_trait;
use kalandra_core::{
    item::ParsedItem,
    pricing::{Listing, Price, PriceSourceKind, PriceSummary},
    query::{FilterSet, OnlineStatus, QueryRoute, TradeQuery},
};
use moka::future::Cache;
use serde::de::DeserializeOwned;
use url::Url;

use crate::{
    PriceRequest, PriceSource, TradeError,
    ratelimit::{RateLimiter, Rule},
    types::{ExchangeEntry, ExchangeResponse, FetchEntry, League, ResultList, SearchResponse},
};

#[derive(Debug, Clone)]
pub struct TradeConfig {
    /// `https://www.pathofexile.com` in production; wiremock URI in tests.
    pub base_url: Url,
    pub user_agent: String,
    /// Optional POESESSID cookie value.
    pub poesessid: Option<String>,
    /// Max listings to fetch per search (fetch takes <= 10 ids per call).
    pub fetch_count: usize,
    pub cache_ttl: std::time::Duration,
}

impl Default for TradeConfig {
    fn default() -> Self {
        Self {
            base_url: Url::parse("https://www.pathofexile.com").expect("static url"),
            user_agent: crate::default_user_agent(),
            poesessid: None,
            fetch_count: 20,
            cache_ttl: std::time::Duration::from_secs(300),
        }
    }
}

/// Rate-limiter keys. Each maps to one GGG policy.
pub mod policy {
    pub const SEARCH: &str = "search";
    pub const FETCH: &str = "fetch";
    pub const EXCHANGE: &str = "exchange";
    pub const DATA: &str = "data";
}

/// Max ids per `fetch` call (server limit).
pub const FETCH_CHUNK: usize = 10;

/// Longest a request is queued behind the limiter before giving up with
/// [`TradeError::RateLimited`] (an overlay waiting longer than this is useless).
pub const MAX_QUEUE_WAIT: Duration = Duration::from_secs(10);

/// Search/fetch results mapped to core listings, plus the link to the same search.
#[derive(Debug, Clone, PartialEq)]
pub struct ListingPage {
    pub search_id: String,
    pub total: u32,
    pub listings: Vec<Listing>,
    pub trade_url: Url,
}

/// Official trade2 API client.
#[derive(Debug)]
pub struct TradeClient {
    config: TradeConfig,
    http: reqwest::Client,
    limiter: RateLimiter,
    searches: Option<Cache<String, Arc<SearchResponse>>>,
    fetches: Option<Cache<String, Arc<Vec<FetchEntry>>>>,
    exchanges: Option<Cache<String, Arc<ExchangeResponse>>>,
    data: Option<Cache<String, Arc<serde_json::Value>>>,
}

fn cache<V: Clone + Send + Sync + 'static>(ttl: Duration) -> Option<Cache<String, V>> {
    (!ttl.is_zero()).then(|| Cache::builder().max_capacity(512).time_to_live(ttl).build())
}

fn rules(spec: &str) -> Vec<Rule> {
    crate::ratelimit::parse_rules(spec).expect("static rules")
}

impl TradeClient {
    pub fn new(config: TradeConfig, clock: Box<dyn crate::ratelimit::Clock>) -> Result<Self, TradeError> {
        if config.base_url.cannot_be_a_base() {
            return Err(TradeError::Config(format!("base_url `{}` cannot be a base", config.base_url)));
        }
        let mut headers = reqwest::header::HeaderMap::new();
        headers.insert(reqwest::header::ACCEPT, reqwest::header::HeaderValue::from_static("application/json"));
        if let Some(sess) = config.poesessid.as_deref().filter(|s| !s.is_empty()) {
            let v = reqwest::header::HeaderValue::from_str(&format!("POESESSID={sess}"))
                .map_err(|e| TradeError::Config(format!("invalid POESESSID: {e}")))?;
            headers.insert(reqwest::header::COOKIE, v);
        }
        let http = reqwest::Client::builder()
            .user_agent(config.user_agent.clone())
            .default_headers(headers)
            .timeout(Duration::from_secs(20))
            .build()
            .map_err(|e| TradeError::Config(format!("http client: {e}")))?;
        let limiter = RateLimiter::new(clock);
        // Anonymous limits observed on 2026-11-05 (docs/research/trade2.md); replaced by
        // live headers after the first response.
        limiter.seed(policy::SEARCH, &rules("5:10:60,15:60:300,30:300:1800"));
        limiter.seed(policy::FETCH, &rules("12:4:10,16:12:300,50:300:300"));
        limiter.seed(policy::EXCHANGE, &rules("5:15:60,10:90:300,30:300:1800"));
        // Data endpoints are CDN-cached and send no headers; stay conservative anyway.
        limiter.seed(policy::DATA, &rules("5:10:60"));
        let ttl = config.cache_ttl;
        Ok(Self {
            config,
            http,
            limiter,
            searches: cache(ttl),
            fetches: cache(ttl),
            exchanges: cache(ttl),
            data: cache(ttl),
        })
    }

    pub fn config(&self) -> &TradeConfig {
        &self.config
    }

    /// The limiter, for diagnostics.
    pub fn limiter(&self) -> &RateLimiter {
        &self.limiter
    }

    fn url(&self, segments: &[&str]) -> Url {
        let mut url = self.config.base_url.clone();
        url.path_segments_mut().expect("checked in new").pop_if_empty().extend(segments);
        url
    }

    async fn send<T: DeserializeOwned>(&self, policy: &str, req: reqwest::RequestBuilder) -> Result<T, TradeError> {
        let blocked = self.limiter.blocked_for(policy);
        if !blocked.is_zero() {
            return Err(TradeError::RateLimited { retry_after: blocked });
        }
        // Refusals record nothing; if this future is dropped while sleeping, the
        // guard releases the slot so cancelled requests do not throttle later ones.
        let slot = self
            .limiter
            .reserve_guarded(policy, MAX_QUEUE_WAIT)
            .map_err(|retry_after| TradeError::RateLimited { retry_after })?;
        let wait = slot.wait();
        if !wait.is_zero() {
            tracing::debug!(policy, ?wait, "rate limiter delaying request");
            tokio::time::sleep(wait).await;
        }
        slot.commit();
        let resp = req.send().await?;
        let status = resp.status();
        self.limiter.observe(policy, status.as_u16(), resp.headers());
        if status.as_u16() == 429 {
            let retry_after = self.limiter.blocked_for(policy);
            return Err(TradeError::RateLimited { retry_after });
        }
        let body = resp.bytes().await?;
        if !status.is_success() {
            return Err(TradeError::Http { status: status.as_u16(), message: error_message(&body) });
        }
        serde_json::from_slice(&body).map_err(|e| TradeError::Decode(e.to_string()))
    }

    /// `GET /api/trade2/data/leagues`
    pub async fn leagues(&self) -> Result<Vec<League>, TradeError> {
        let v = self.data_cached("leagues").await?;
        let list: ResultList<League> =
            serde_json::from_value((*v).clone()).map_err(|e| TradeError::Decode(e.to_string()))?;
        Ok(list.result)
    }

    /// `GET /api/trade2/data/{stats|items|static|filters}` raw JSON (used by xtask data refresh).
    pub async fn data(&self, which: &str) -> Result<serde_json::Value, TradeError> {
        Ok((*self.data_cached(which).await?).clone())
    }

    async fn data_cached(&self, which: &str) -> Result<Arc<serde_json::Value>, TradeError> {
        if let Some(hit) = cached(self.data.as_ref(), which).await {
            return Ok(hit);
        }
        let url = self.url(&["api", "trade2", "data", which]);
        let v: Arc<serde_json::Value> = Arc::new(self.send(policy::DATA, self.http.get(url)).await?);
        if let Some(c) = &self.data {
            c.insert(which.to_owned(), v.clone()).await;
        }
        Ok(v)
    }

    /// `POST /api/trade2/search/poe2/{league}`
    pub async fn search(&self, league: &str, query: &TradeQuery) -> Result<SearchResponse, TradeError> {
        let body = serde_json::to_string(query).map_err(|e| TradeError::Decode(e.to_string()))?;
        let key = format!("{league}\n{body}");
        if let Some(hit) = cached(self.searches.as_ref(), &key).await {
            return Ok((*hit).clone());
        }
        let url = self.url(&["api", "trade2", "search", "poe2", league]);
        let req = self.http.post(url).header(reqwest::header::CONTENT_TYPE, "application/json").body(body);
        let resp: SearchResponse = self.send(policy::SEARCH, req).await?;
        if let Some(c) = &self.searches {
            c.insert(key, Arc::new(resp.clone())).await;
        }
        Ok(resp)
    }

    /// `GET /api/trade2/fetch/{id,id,...}?query={search_id}`, in chunks of [`FETCH_CHUNK`].
    /// Listings that vanished between search and fetch (`null`) are skipped.
    ///
    /// Chunks are requested concurrently (each still takes its own rate-limiter slot), so
    /// the default 20 listings cost one round trip instead of two; results keep id order.
    pub async fn fetch(&self, ids: &[String], search_id: &str) -> Result<Vec<FetchEntry>, TradeError> {
        let chunks =
            futures::future::try_join_all(ids.chunks(FETCH_CHUNK).map(|c| self.fetch_chunk(c, search_id))).await?;
        let mut out = Vec::with_capacity(ids.len());
        for chunk in chunks {
            out.extend(chunk.iter().cloned());
        }
        Ok(out)
    }

    async fn fetch_chunk(&self, chunk: &[String], search_id: &str) -> Result<Arc<Vec<FetchEntry>>, TradeError> {
        let joined = chunk.join(",");
        let key = format!("{search_id}\n{joined}");
        if let Some(hit) = cached(self.fetches.as_ref(), &key).await {
            return Ok(hit);
        }
        let mut url = self.url(&["api", "trade2", "fetch", &joined]);
        url.query_pairs_mut().append_pair("query", search_id).append_pair("realm", "poe2");
        let list: ResultList<Option<FetchEntry>> = self.send(policy::FETCH, self.http.get(url)).await?;
        let entries = Arc::new(list.result.into_iter().flatten().collect::<Vec<_>>());
        if let Some(c) = &self.fetches {
            c.insert(key, entries.clone()).await;
        }
        Ok(entries)
    }

    /// `POST /api/trade2/exchange/poe2/{league}` with a raw bulk-exchange body.
    pub async fn exchange(&self, league: &str, body: &serde_json::Value) -> Result<ExchangeResponse, TradeError> {
        let body = body.to_string();
        let key = format!("{league}\n{body}");
        if let Some(hit) = cached(self.exchanges.as_ref(), &key).await {
            return Ok((*hit).clone());
        }
        let url = self.url(&["api", "trade2", "exchange", "poe2", league]);
        let req = self.http.post(url).header(reqwest::header::CONTENT_TYPE, "application/json").body(body);
        let resp: ExchangeResponse = self.send(policy::EXCHANGE, req).await?;
        if let Some(c) = &self.exchanges {
            c.insert(key, Arc::new(resp.clone())).await;
        }
        Ok(resp)
    }

    /// `https://www.pathofexile.com/trade2/search/poe2/{league}/{search_id}`
    pub fn trade_url(&self, league: &str, search_id: &str) -> Url {
        self.url(&["trade2", "search", "poe2", league, search_id])
    }

    /// `https://www.pathofexile.com/trade2/exchange/poe2/{league}/{search_id}`
    pub fn exchange_url(&self, league: &str, search_id: &str) -> Url {
        self.url(&["trade2", "exchange", "poe2", league, search_id])
    }

    /// Search, then fetch the first `fetch_count` results and map them to listings.
    pub async fn search_listings(&self, league: &str, query: &TradeQuery) -> Result<ListingPage, TradeError> {
        let search = self.search(league, query).await?;
        let ids: Vec<String> = search.result.iter().take(self.config.fetch_count).cloned().collect();
        let entries = if ids.is_empty() { Vec::new() } else { self.fetch(&ids, &search.id).await? };
        Ok(ListingPage {
            trade_url: self.trade_url(league, &search.id),
            search_id: search.id,
            total: search.total,
            listings: entries.iter().filter_map(fetch_listing).collect(),
        })
    }

    /// Exchange tag (`divine`, `exalted`, ...) for an item name, from `data/static`.
    pub async fn static_id(&self, text: &str) -> Result<Option<String>, TradeError> {
        let v = self.data_cached("static").await?;
        Ok(v["result"]
            .as_array()
            .into_iter()
            .flatten()
            .flat_map(|g| g["entries"].as_array().into_iter().flatten())
            .find(|e| e["text"].as_str() == Some(text))
            .and_then(|e| e["id"].as_str().map(str::to_owned)))
    }

    /// Bulk-exchange lookup for an item sold by exchange tag `want`.
    pub async fn exchange_listings(
        &self,
        league: &str,
        want: &str,
        status: OnlineStatus,
    ) -> Result<ListingPage, TradeError> {
        let resp = self.exchange(league, &exchange_body(want, status)).await?;
        Ok(ListingPage {
            trade_url: self.exchange_url(league, &resp.id),
            search_id: resp.id,
            total: resp.total,
            listings: resp.result.iter().filter_map(exchange_listing).take(self.config.fetch_count).collect(),
        })
    }

    /// Run the filters against the right route and return raw listings.
    pub async fn listings_for(&self, filters: &FilterSet) -> Result<ListingPage, TradeError> {
        match filters.route {
            QueryRoute::Search => self.search_listings(&filters.league, &filters.to_trade_query()).await,
            QueryRoute::Exchange => {
                let name = filters.base_type.as_deref().or(filters.name.as_deref()).unwrap_or_default();
                let want = self.static_id(name).await?.ok_or(TradeError::Unsupported("trade exchange"))?;
                self.exchange_listings(&filters.league, &want, filters.status).await
            }
        }
    }
}

/// Bulk-exchange body (EE2 `createTradeRequest` shape). Prices are asked in exalted and
/// divine, except for the currency being priced itself.
pub fn exchange_body(want: &str, status: OnlineStatus) -> serde_json::Value {
    let have: Vec<&str> = ["exalted", "divine"].into_iter().filter(|c| *c != want).collect();
    let status = match status {
        OnlineStatus::Any => "any",
        // The exchange has no "securable" option.
        OnlineStatus::Online | OnlineStatus::Securable => "online",
    };
    serde_json::json!({
        "query": { "status": { "option": status }, "have": have, "want": [want] },
        "sort": { "have": "asc" },
        "engine": "new",
    })
}

fn error_message(body: &[u8]) -> String {
    serde_json::from_slice::<serde_json::Value>(body)
        .ok()
        .and_then(|v| v["error"]["message"].as_str().map(str::to_owned))
        .unwrap_or_else(|| String::from_utf8_lossy(&body[..body.len().min(200)]).into_owned())
}

/// Map a fetch entry to a core listing; entries without a price are dropped.
pub fn fetch_listing(e: &FetchEntry) -> Option<Listing> {
    let price = e.listing.price.as_ref()?;
    let account = e.listing.account.as_ref();
    let as_u32 = |k: &str| e.item.get(k).and_then(serde_json::Value::as_u64).map(|v| v as u32);
    Some(Listing {
        id: e.id.clone(),
        price: Price { amount: price.amount, currency: price.currency.clone() },
        seller: account.map(|a| a.name.clone()),
        online: account.and_then(|a| a.online.as_ref()).is_some_and(|o| !o.is_null()),
        indexed: e.listing.indexed.clone(),
        item_level: as_u32("ilvl"),
        stack_size: as_u32("stackSize"),
    })
}

/// Map an exchange entry to a per-unit listing; `stack_size` is the seller's stock.
pub fn exchange_listing(e: &ExchangeEntry) -> Option<Listing> {
    let offer = e.listing.offers.first()?;
    if offer.item.amount <= 0.0 {
        return None;
    }
    let account = e.listing.account.as_ref();
    Some(Listing {
        id: e.id.clone(),
        price: Price { amount: offer.exchange.amount / offer.item.amount, currency: offer.exchange.currency.clone() },
        seller: account.map(|a| a.name.clone()),
        online: account.and_then(|a| a.online.as_ref()).is_some_and(|o| !o.is_null()),
        indexed: e.listing.indexed.clone(),
        item_level: None,
        stack_size: offer.item.stock,
    })
}

async fn cached<V: Clone + Send + Sync + 'static>(cache: Option<&Cache<String, V>>, key: &str) -> Option<V> {
    match cache {
        Some(c) => c.get(key).await,
        None => None,
    }
}

#[async_trait]
impl PriceSource for TradeClient {
    fn kind(&self) -> PriceSourceKind {
        PriceSourceKind::Trade
    }
    fn supports(&self, _item: &ParsedItem) -> bool {
        true
    }
    async fn price(&self, req: &PriceRequest) -> Result<PriceSummary, TradeError> {
        let page = self.listings_for(&req.filters).await?;
        let mut summary = kalandra_core::pricing::summarise(PriceSourceKind::Trade, page.total, page.listings, None);
        summary.trade_url = Some(page.trade_url.to_string());
        Ok(summary)
    }
}
