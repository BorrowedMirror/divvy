//! Header-driven rate limiter for GGG APIs.
//!
//! GGG returns per-policy headers:
//! `X-Rate-Limit-Policy: trade-search-request-limit`,
//! `X-Rate-Limit-Rules: Account,Ip`,
//! `X-Rate-Limit-Ip: 8:10:60,15:60:120` (hits:period_s:penalty_s, comma-separated),
//! `X-Rate-Limit-Ip-State: 1:10:0,1:60:0` (current_hits:period_s:active_penalty_s),
//! likewise `-Account`/`-Account-State`; `Retry-After` (seconds) on 429.
use std::time::Duration;

/// Monotonic time source, injectable for tests.
pub trait Clock: Send + Sync + std::fmt::Debug {
    /// Time since an arbitrary fixed epoch.
    fn now(&self) -> Duration;
}

#[derive(Debug, Default, Clone, Copy)]
pub struct SystemClock;

impl Clock for SystemClock {
    fn now(&self) -> Duration {
        use std::sync::OnceLock;
        static START: OnceLock<std::time::Instant> = OnceLock::new();
        START.get_or_init(std::time::Instant::now).elapsed()
    }
}

/// One `hits:period:penalty` rule.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct Rule {
    pub max_hits: u32,
    pub period: Duration,
    pub penalty: Duration,
}

/// One `current:period:restricted` state triple.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct RuleState {
    pub hits: u32,
    pub period: Duration,
    pub restricted: Duration,
}

/// Parse `8:10:60,15:60:120`.
pub fn parse_rules(_header: &str) -> Result<Vec<Rule>, String> {
    todo!("workstream B")
}

/// Parse `1:10:0,1:60:0`.
pub fn parse_states(_header: &str) -> Result<Vec<RuleState>, String> {
    todo!("workstream B")
}

/// Tracks policies by name and decides how long to wait before the next request.
#[derive(Debug)]
pub struct RateLimiter {
    clock: Box<dyn Clock>,
}

impl RateLimiter {
    pub fn new(clock: Box<dyn Clock>) -> Self {
        Self { clock }
    }

    pub fn clock(&self) -> &dyn Clock {
        self.clock.as_ref()
    }

    /// How long the caller must wait before issuing a request under `policy`
    /// (`Duration::ZERO` = go now). Records the hit optimistically.
    pub fn reserve(&self, _policy: &str) -> Duration {
        todo!("workstream B")
    }

    /// Feed response headers (and status) back in.
    pub fn observe(&self, _policy_hint: &str, _status: u16, _headers: &http::HeaderMap) {
        todo!("workstream B")
    }
}
