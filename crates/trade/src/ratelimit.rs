//! Header-driven rate limiter for GGG APIs.
//!
//! GGG returns per-policy headers:
//! `X-Rate-Limit-Policy: trade-search-request-limit`,
//! `X-Rate-Limit-Rules: Account,Ip`,
//! `X-Rate-Limit-Ip: 8:10:60,15:60:120` (hits:period_s:penalty_s, comma-separated),
//! `X-Rate-Limit-Ip-State: 1:10:0,1:60:0` (current_hits:period_s:active_penalty_s),
//! likewise `-Account`/`-Account-State`; `Retry-After` (seconds) on 429.
//!
//! The limiter keeps, per policy key, the timestamps of the most recent hits for
//! every rule and hands out start times so that no rule ever sees more than
//! `max_hits` requests inside any half-open window of length `period`.
use std::{
    collections::{HashMap, VecDeque},
    fmt,
    sync::Mutex,
    time::Duration,
};

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

impl fmt::Display for Rule {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "{}:{}:{}", self.max_hits, self.period.as_secs(), self.penalty.as_secs())
    }
}

impl fmt::Display for RuleState {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "{}:{}:{}", self.hits, self.period.as_secs(), self.restricted.as_secs())
    }
}

fn parse_triples(header: &str) -> Result<Vec<(u32, u64, u64)>, String> {
    let header = header.trim();
    if header.is_empty() {
        return Ok(Vec::new());
    }
    header
        .split(',')
        .map(|part| {
            let mut it = part.trim().split(':');
            let mut next = |what: &str| -> Result<u64, String> {
                it.next()
                    .ok_or_else(|| format!("missing {what} in `{part}`"))?
                    .trim()
                    .parse::<u64>()
                    .map_err(|e| format!("bad {what} in `{part}`: {e}"))
            };
            let a = next("hits")?;
            let b = next("period")?;
            let c = next("penalty")?;
            if it.next().is_some() {
                return Err(format!("too many fields in `{part}`"));
            }
            let a = u32::try_from(a).map_err(|e| format!("bad hits in `{part}`: {e}"))?;
            Ok((a, b, c))
        })
        .collect()
}

/// Parse `8:10:60,15:60:120`.
pub fn parse_rules(header: &str) -> Result<Vec<Rule>, String> {
    Ok(parse_triples(header)?
        .into_iter()
        .map(|(h, p, x)| Rule { max_hits: h, period: Duration::from_secs(p), penalty: Duration::from_secs(x) })
        .collect())
}

/// Parse `1:10:0,1:60:0`.
pub fn parse_states(header: &str) -> Result<Vec<RuleState>, String> {
    Ok(parse_triples(header)?
        .into_iter()
        .map(|(h, p, x)| RuleState { hits: h, period: Duration::from_secs(p), restricted: Duration::from_secs(x) })
        .collect())
}

/// Render rules back into header form (`8:10:60,15:60:120`).
pub fn format_rules(rules: &[Rule]) -> String {
    rules.iter().map(ToString::to_string).collect::<Vec<_>>().join(",")
}

/// Render states back into header form (`1:10:0,1:60:0`).
pub fn format_states(states: &[RuleState]) -> String {
    states.iter().map(ToString::to_string).collect::<Vec<_>>().join(",")
}

#[derive(Debug)]
struct Tracked {
    /// Rule-set name (`ip`, `account`, ...), lower-case.
    scope: String,
    rule: Rule,
    /// Sorted start times of recent hits; at most `max_hits` retained.
    hits: VecDeque<Duration>,
}

impl Tracked {
    fn prune(&mut self, now: Duration) {
        while self.hits.front().is_some_and(|h| *h + self.rule.period <= now) {
            self.hits.pop_front();
        }
        while self.hits.len() > self.rule.max_hits as usize {
            self.hits.pop_front();
        }
    }

    /// Earliest start time `>= t` that keeps this rule satisfied (hits sorted, all `<= t`).
    fn earliest(&self, t: Duration) -> Duration {
        let max = self.rule.max_hits as usize;
        if max == 0 || self.hits.len() < max {
            return t;
        }
        t.max(self.hits[self.hits.len() - max] + self.rule.period)
    }

    fn insert(&mut self, at: Duration) {
        let pos = self.hits.partition_point(|h| *h <= at);
        self.hits.insert(pos, at);
    }
}

#[derive(Debug, Default)]
struct Policy {
    /// Server-side policy name (`trade-search-request-limit`), once seen.
    name: Option<String>,
    rules: Vec<Tracked>,
    blocked_until: Duration,
    last_start: Duration,
}

impl Policy {
    fn set_rules(&mut self, scope: &str, rules: &[Rule]) {
        let mut old: Vec<Tracked> = Vec::new();
        self.rules.retain_mut(|t| {
            if t.scope == scope {
                old.push(Tracked { scope: String::new(), rule: t.rule, hits: std::mem::take(&mut t.hits) });
                false
            } else {
                true
            }
        });
        for rule in rules {
            let hits = old
                .iter_mut()
                .find(|o| o.rule.period == rule.period)
                .map(|o| std::mem::take(&mut o.hits))
                .unwrap_or_default();
            let mut t = Tracked { scope: scope.to_owned(), rule: *rule, hits };
            while t.hits.len() > rule.max_hits as usize {
                t.hits.pop_front();
            }
            self.rules.push(t);
        }
    }
}

/// Tracks policies by name and decides how long to wait before the next request.
#[derive(Debug)]
pub struct RateLimiter {
    clock: Box<dyn Clock>,
    inner: Mutex<HashMap<String, Policy>>,
}

/// Fallback restriction when a 429 arrives without `Retry-After` or penalty info.
const DEFAULT_PENALTY: Duration = Duration::from_secs(60);

impl RateLimiter {
    pub fn new(clock: Box<dyn Clock>) -> Self {
        Self { clock, inner: Mutex::new(HashMap::new()) }
    }

    pub fn clock(&self) -> &dyn Clock {
        self.clock.as_ref()
    }

    fn lock(&self) -> std::sync::MutexGuard<'_, HashMap<String, Policy>> {
        self.inner.lock().unwrap_or_else(std::sync::PoisonError::into_inner)
    }

    /// Pre-load rules for `policy` before any response has been seen (e.g. the
    /// documented trade2 defaults), so the very first burst is already throttled.
    /// Rules learnt later from headers replace these.
    pub fn seed(&self, policy: &str, rules: &[Rule]) {
        let mut map = self.lock();
        let p = map.entry(policy.to_owned()).or_default();
        p.set_rules("seed", rules);
    }

    /// How long the caller must wait before issuing a request under `policy`
    /// (`Duration::ZERO` = go now). Records the hit optimistically.
    pub fn reserve(&self, policy: &str) -> Duration {
        let now = self.clock.now();
        let mut map = self.lock();
        let p = map.entry(policy.to_owned()).or_default();
        for t in &mut p.rules {
            t.prune(now);
        }
        let mut start = now.max(p.blocked_until).max(p.last_start);
        for t in &p.rules {
            start = t.earliest(start);
        }
        for t in &mut p.rules {
            if t.rule.max_hits > 0 {
                t.insert(start);
            }
        }
        p.last_start = start;
        start.saturating_sub(now)
    }

    /// Remaining hard restriction (from a 429 or an active penalty) for `policy`.
    /// Unlike [`Self::reserve`] this records nothing.
    pub fn blocked_for(&self, policy: &str) -> Duration {
        let now = self.clock.now();
        self.lock().get(policy).map_or(Duration::ZERO, |p| p.blocked_until.saturating_sub(now))
    }

    /// Server-side policy name last reported for `policy`.
    pub fn policy_name(&self, policy: &str) -> Option<String> {
        self.lock().get(policy).and_then(|p| p.name.clone())
    }

    /// Feed response headers (and status) back in.
    pub fn observe(&self, policy_hint: &str, status: u16, headers: &http::HeaderMap) {
        let now = self.clock.now();
        let header = |name: &str| headers.get(name).and_then(|v| v.to_str().ok());
        let mut map = self.lock();
        let p = map.entry(policy_hint.to_owned()).or_default();
        if let Some(name) = header("x-rate-limit-policy") {
            p.name = Some(name.to_owned());
        }
        let mut restricted = Duration::ZERO;
        let mut max_penalty = Duration::ZERO;
        if let Some(scopes) = header("x-rate-limit-rules") {
            let scopes: Vec<String> =
                scopes.split(',').map(|s| s.trim().to_ascii_lowercase()).filter(|s| !s.is_empty()).collect();
            if !scopes.is_empty() {
                // Real rules supersede seeded defaults.
                p.set_rules("seed", &[]);
            }
            for scope in scopes {
                let Some(rules) = header(&format!("x-rate-limit-{scope}")).and_then(|h| parse_rules(h).ok()) else {
                    continue;
                };
                p.set_rules(&scope, &rules);
                max_penalty = rules.iter().map(|r| r.penalty).fold(max_penalty, Duration::max);
                let states = header(&format!("x-rate-limit-{scope}-state"))
                    .and_then(|h| parse_states(h).ok())
                    .unwrap_or_default();
                for st in states {
                    restricted = restricted.max(st.restricted);
                    if let Some(t) = p.rules.iter_mut().find(|t| t.scope == scope && t.rule.period == st.period) {
                        t.prune(now);
                        let ours = t.hits.iter().filter(|h| **h + t.rule.period > now && **h <= now).count();
                        let missing = (st.hits as usize).saturating_sub(ours).min(t.rule.max_hits as usize);
                        for _ in 0..missing {
                            t.insert(now);
                        }
                        t.prune(now);
                    }
                }
            }
        }
        if status == 429 {
            let retry = header("retry-after").and_then(|v| v.trim().parse::<u64>().ok()).map(Duration::from_secs);
            let wait = retry.unwrap_or(if restricted.is_zero() {
                if max_penalty.is_zero() { DEFAULT_PENALTY } else { max_penalty }
            } else {
                restricted
            });
            restricted = restricted.max(wait);
        }
        if !restricted.is_zero() {
            p.blocked_until = p.blocked_until.max(now + restricted);
        }
    }
}

#[cfg(test)]
mod tests {
    use std::sync::{
        Arc,
        atomic::{AtomicU64, Ordering},
    };

    use http::{HeaderMap, HeaderValue};
    use proptest::prelude::*;

    use super::*;

    #[derive(Debug, Clone, Default)]
    struct FakeClock(Arc<AtomicU64>);
    impl FakeClock {
        fn advance(&self, d: Duration) {
            self.0.fetch_add(d.as_millis() as u64, Ordering::SeqCst);
        }
    }
    impl Clock for FakeClock {
        fn now(&self) -> Duration {
            Duration::from_millis(self.0.load(Ordering::SeqCst))
        }
    }

    fn headers(pairs: &[(&'static str, &str)]) -> HeaderMap {
        let mut h = HeaderMap::new();
        for (k, v) in pairs {
            h.insert(*k, HeaderValue::from_str(v).unwrap());
        }
        h
    }

    fn rule(h: u32, p: u64, x: u64) -> Rule {
        Rule { max_hits: h, period: Duration::from_secs(p), penalty: Duration::from_secs(x) }
    }

    #[test]
    fn parses_rules_and_states() {
        assert_eq!(parse_rules("5:10:60,15:60:300").unwrap(), vec![rule(5, 10, 60), rule(15, 60, 300)]);
        assert_eq!(
            parse_states(" 1:10:0 , 6:60:120").unwrap(),
            vec![
                RuleState { hits: 1, period: Duration::from_secs(10), restricted: Duration::ZERO },
                RuleState { hits: 6, period: Duration::from_secs(60), restricted: Duration::from_secs(120) },
            ]
        );
        assert_eq!(parse_rules("").unwrap(), vec![]);
        assert!(parse_rules("5:10").unwrap_err().contains("missing penalty"));
        assert!(parse_rules("5:10:1:2").unwrap_err().contains("too many"));
        assert!(parse_rules("x:10:1").unwrap_err().contains("bad hits"));
        assert!(parse_rules("99999999999:10:1").unwrap_err().contains("bad hits"));
        assert!(parse_states("1:y:0").unwrap_err().contains("bad period"));
    }

    #[test]
    fn system_clock_is_monotonic() {
        let c = SystemClock;
        let a = c.now();
        assert!(c.now() >= a);
    }

    #[test]
    fn reserve_without_rules_is_free() {
        let rl = RateLimiter::new(Box::new(FakeClock::default()));
        for _ in 0..100 {
            assert_eq!(rl.reserve("search"), Duration::ZERO);
        }
        assert_eq!(rl.clock().now(), Duration::ZERO);
    }

    #[test]
    fn learns_rules_from_headers_and_throttles() {
        let clock = FakeClock::default();
        let rl = RateLimiter::new(Box::new(clock.clone()));
        assert_eq!(rl.reserve("search"), Duration::ZERO);
        rl.observe(
            "search",
            200,
            &headers(&[
                ("x-rate-limit-policy", "trade-search-request-limit"),
                ("x-rate-limit-rules", "Ip"),
                ("x-rate-limit-ip", "2:10:60"),
                ("x-rate-limit-ip-state", "1:10:0"),
            ]),
        );
        assert_eq!(rl.policy_name("search").as_deref(), Some("trade-search-request-limit"));
        assert_eq!(rl.reserve("search"), Duration::ZERO);
        // Third request inside 10s must wait for the first hit to leave the window.
        assert_eq!(rl.reserve("search"), Duration::from_secs(10));
        clock.advance(Duration::from_secs(10));
        // Window [10, 20) holds the reservation made for t=10 plus this one...
        assert_eq!(rl.reserve("search"), Duration::ZERO);
        // ...so a third one has to wait until t=20.
        assert_eq!(rl.reserve("search"), Duration::from_secs(10));
    }

    #[test]
    fn server_state_adds_unseen_hits() {
        let clock = FakeClock::default();
        let rl = RateLimiter::new(Box::new(clock));
        rl.observe(
            "fetch",
            200,
            &headers(&[
                ("x-rate-limit-rules", "Account,Ip"),
                ("x-rate-limit-account", "3:5:60"),
                ("x-rate-limit-account-state", "3:5:0"),
                ("x-rate-limit-ip", "10:10:60"),
                ("x-rate-limit-ip-state", "1:10:0"),
            ]),
        );
        // Account window is already full according to the server.
        assert_eq!(rl.reserve("fetch"), Duration::from_secs(5));
    }

    #[test]
    fn too_many_requests_honours_retry_after() {
        let clock = FakeClock::default();
        let rl = RateLimiter::new(Box::new(clock.clone()));
        rl.observe("search", 429, &headers(&[("retry-after", "30")]));
        assert_eq!(rl.blocked_for("search"), Duration::from_secs(30));
        assert_eq!(rl.blocked_for("other"), Duration::ZERO);
        assert_eq!(rl.reserve("search"), Duration::from_secs(30));
        clock.advance(Duration::from_secs(31));
        assert_eq!(rl.blocked_for("search"), Duration::ZERO);
    }

    #[test]
    fn too_many_requests_without_retry_after_uses_state_then_penalty_then_default() {
        let rl = RateLimiter::new(Box::new(FakeClock::default()));
        rl.observe(
            "a",
            429,
            &headers(&[
                ("x-rate-limit-rules", "ip"),
                ("x-rate-limit-ip", "5:10:60"),
                ("x-rate-limit-ip-state", "6:10:45"),
            ]),
        );
        assert_eq!(rl.blocked_for("a"), Duration::from_secs(45));
        rl.observe("b", 429, &headers(&[("x-rate-limit-rules", "ip"), ("x-rate-limit-ip", "5:10:90")]));
        assert_eq!(rl.blocked_for("b"), Duration::from_secs(90));
        rl.observe("c", 429, &HeaderMap::new());
        assert_eq!(rl.blocked_for("c"), DEFAULT_PENALTY);
    }

    #[test]
    fn active_restriction_state_blocks_even_on_success() {
        let rl = RateLimiter::new(Box::new(FakeClock::default()));
        rl.observe(
            "x",
            200,
            &headers(&[
                ("x-rate-limit-rules", "Ip"),
                ("x-rate-limit-ip", "5:10:60"),
                ("x-rate-limit-ip-state", "1:10:20"),
            ]),
        );
        assert_eq!(rl.reserve("x"), Duration::from_secs(20));
    }

    #[test]
    fn seeded_rules_apply_until_replaced() {
        let clock = FakeClock::default();
        let rl = RateLimiter::new(Box::new(clock));
        rl.seed("s", &[rule(1, 4, 0)]);
        assert_eq!(rl.reserve("s"), Duration::ZERO);
        assert_eq!(rl.reserve("s"), Duration::from_secs(4));
        rl.observe("s", 200, &headers(&[("x-rate-limit-rules", "Ip"), ("x-rate-limit-ip", "100:4:0")]));
        // seed rule gone; real rule is generous but monotonic ordering is preserved.
        assert_eq!(rl.reserve("s"), Duration::from_secs(4));
    }

    #[test]
    fn malformed_headers_are_ignored() {
        let rl = RateLimiter::new(Box::new(FakeClock::default()));
        rl.observe(
            "m",
            200,
            &headers(&[
                ("x-rate-limit-rules", "Ip, ,Account"),
                ("x-rate-limit-ip", "garbage"),
                ("x-rate-limit-ip-state", "x"),
            ]),
        );
        assert_eq!(rl.reserve("m"), Duration::ZERO);
    }

    #[test]
    fn rule_change_keeps_hits_for_same_period() {
        let rl = RateLimiter::new(Box::new(FakeClock::default()));
        let h = |r: &str| headers(&[("x-rate-limit-rules", "Ip"), ("x-rate-limit-ip", r)]);
        rl.observe("p", 200, &h("3:10:0"));
        rl.reserve("p");
        rl.reserve("p");
        rl.reserve("p");
        rl.observe("p", 200, &h("2:10:0,50:60:0"));
        assert_eq!(rl.reserve("p"), Duration::from_secs(10));
    }

    #[test]
    fn zero_hit_rules_do_not_block() {
        let rl = RateLimiter::new(Box::new(FakeClock::default()));
        rl.seed("z", &[rule(0, 10, 0)]);
        assert_eq!(rl.reserve("z"), Duration::ZERO);
    }

    fn arb_rules() -> impl Strategy<Value = Vec<Rule>> {
        prop::collection::vec((1u32..6, 1u64..30, 0u64..100), 1..4)
            .prop_map(|v| v.into_iter().map(|(h, p, x)| rule(h, p, x)).collect())
    }

    proptest! {
        #[test]
        fn rules_round_trip(rules in prop::collection::vec((0u32..10_000, 0u64..100_000, 0u64..100_000), 0..6)) {
            let rules: Vec<Rule> = rules.into_iter().map(|(h, p, x)| rule(h, p, x)).collect();
            prop_assert_eq!(parse_rules(&format_rules(&rules)).unwrap(), rules);
        }

        #[test]
        fn states_round_trip(states in prop::collection::vec((0u32..10_000, 0u64..100_000, 0u64..100_000), 0..6)) {
            let states: Vec<RuleState> = states
                .into_iter()
                .map(|(h, p, x)| RuleState { hits: h, period: Duration::from_secs(p), restricted: Duration::from_secs(x) })
                .collect();
            prop_assert_eq!(parse_states(&format_states(&states)).unwrap(), states);
        }

        #[test]
        fn never_exceeds_max_hits_in_any_window(
            rules in arb_rules(),
            gaps in prop::collection::vec(0u64..5_000, 1..60),
        ) {
            let clock = FakeClock::default();
            let rl = RateLimiter::new(Box::new(clock.clone()));
            rl.seed("p", &rules);
            let mut starts = Vec::new();
            for gap in gaps {
                clock.advance(Duration::from_millis(gap));
                let wait = rl.reserve("p");
                starts.push(clock.now() + wait);
            }
            let mut sorted = starts.clone();
            sorted.sort();
            prop_assert_eq!(&sorted, &starts, "start times must be monotonic");
            for r in &rules {
                for (i, s) in sorted.iter().enumerate() {
                    let in_window = sorted[i..].iter().take_while(|t| **t < *s + r.period).count();
                    prop_assert!(in_window <= r.max_hits as usize, "rule {} saw {} hits from {:?}", r, in_window, s);
                }
            }
        }
    }
}
