package trade

// Header-driven rate limiter for GGG APIs.
//
// GGG returns per-policy headers:
// `X-Rate-Limit-Policy: trade-search-request-limit`,
// `X-Rate-Limit-Rules: Account,Ip`,
// `X-Rate-Limit-Ip: 8:10:60,15:60:120` (hits:period_s:penalty_s, comma-separated),
// `X-Rate-Limit-Ip-State: 1:10:0,1:60:0` (current_hits:period_s:active_penalty_s),
// likewise `-Account`/`-Account-State`; `Retry-After` (seconds) on 429.
//
// The limiter keeps, per policy key, the timestamps of the most recent hits for every rule
// and hands out start times so that no rule ever sees more than MaxHits requests inside any
// half-open window of length Period.

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Rule is one `hits:period:penalty` rule.
type Rule struct {
	MaxHits uint32
	Period  time.Duration
	Penalty time.Duration
}

// RuleState is one `current:period:restricted` state triple.
type RuleState struct {
	Hits       uint32
	Period     time.Duration
	Restricted time.Duration
}

func (r Rule) String() string {
	return fmt.Sprintf("%d:%d:%d", r.MaxHits, wholeSecs(r.Period), wholeSecs(r.Penalty))
}

func (s RuleState) String() string {
	return fmt.Sprintf("%d:%d:%d", s.Hits, wholeSecs(s.Period), wholeSecs(s.Restricted))
}

func wholeSecs(d time.Duration) int64 { return int64(d / time.Second) }

// maxSecs is the largest whole-second count a time.Duration holds.
const maxSecs = uint64(math.MaxInt64 / int64(time.Second))

// secs converts whole seconds, saturating at the largest Duration.
func secs(s uint64) time.Duration {
	if s > maxSecs {
		return time.Duration(math.MaxInt64)
	}
	return time.Duration(s) * time.Second
}

// parseU64 is Rust's `str::parse::<u64>` (optional leading `+`, ASCII digits only).
func parseU64(s string) (uint64, error) {
	digits := strings.TrimPrefix(s, "+")
	if s == "" {
		return 0, errors.New("cannot parse integer from empty string")
	}
	if digits == "" {
		return 0, errors.New("invalid digit found in string")
	}
	for i := 0; i < len(digits); i++ {
		if digits[i] < '0' || digits[i] > '9' {
			return 0, errors.New("invalid digit found in string")
		}
	}
	v, err := strconv.ParseUint(digits, 10, 64)
	if err != nil {
		return 0, errors.New("number too large to fit in target type")
	}
	return v, nil
}

type triple struct {
	a    uint32
	b, c time.Duration
}

func parseTriples(header string) ([]triple, error) {
	header = strings.TrimSpace(header)
	if header == "" {
		return nil, nil
	}
	var out []triple
	for part := range strings.SplitSeq(header, ",") {
		fields := strings.Split(strings.TrimSpace(part), ":")
		var nums [3]uint64
		for i, what := range []string{"hits", "period", "penalty"} {
			if i >= len(fields) {
				return nil, fmt.Errorf("missing %s in `%s`", what, part)
			}
			v, err := parseU64(strings.TrimSpace(fields[i]))
			if err != nil {
				return nil, fmt.Errorf("bad %s in `%s`: %w", what, part, err)
			}
			if i > 0 && v > maxSecs {
				// Rust's Duration holds any u64 seconds; time.Duration does not.
				return nil, fmt.Errorf("bad %s in `%s`: number too large to fit in target type", what, part)
			}
			nums[i] = v
		}
		if len(fields) > 3 {
			return nil, fmt.Errorf("too many fields in `%s`", part)
		}
		if nums[0] > math.MaxUint32 {
			return nil, fmt.Errorf("bad hits in `%s`: out of range integral type conversion attempted", part)
		}
		out = append(out, triple{uint32(nums[0]), secs(nums[1]), secs(nums[2])})
	}
	return out, nil
}

// ParseRules parses `8:10:60,15:60:120`.
func ParseRules(header string) ([]Rule, error) {
	ts, err := parseTriples(header)
	if err != nil {
		return nil, err
	}
	out := make([]Rule, 0, len(ts))
	for _, t := range ts {
		out = append(out, Rule{MaxHits: t.a, Period: t.b, Penalty: t.c})
	}
	return out, nil
}

// ParseStates parses `1:10:0,1:60:0`.
func ParseStates(header string) ([]RuleState, error) {
	ts, err := parseTriples(header)
	if err != nil {
		return nil, err
	}
	out := make([]RuleState, 0, len(ts))
	for _, t := range ts {
		out = append(out, RuleState{Hits: t.a, Period: t.b, Restricted: t.c})
	}
	return out, nil
}

// FormatRules renders rules back into header form (`8:10:60,15:60:120`).
func FormatRules(rules []Rule) string {
	parts := make([]string, len(rules))
	for i, r := range rules {
		parts[i] = r.String()
	}
	return strings.Join(parts, ",")
}

// FormatStates renders states back into header form (`1:10:0,1:60:0`).
func FormatStates(states []RuleState) string {
	parts := make([]string, len(states))
	for i, s := range states {
		parts[i] = s.String()
	}
	return strings.Join(parts, ",")
}

// EdgeMargin is the safety margin added to every window. GGG counts hits on arrival, so
// latency jitter can squeeze MaxHits+1 requests into a server-side window if we schedule
// exactly at the edge; the margin is `max(period / 20, 250ms)`.
func EdgeMargin(period time.Duration) time.Duration {
	return max(period/20, 250*time.Millisecond)
}

func satSub(a, b time.Duration) time.Duration {
	if a <= b {
		return 0
	}
	return a - b
}

func satAdd(a, b time.Duration) time.Duration {
	if a > math.MaxInt64-b {
		return time.Duration(math.MaxInt64)
	}
	return a + b
}

type tracked struct {
	// Rule-set name (`ip`, `account`, ...), lower-case.
	scope string
	rule  Rule
	// Sorted start times of hits still inside the (padded) window. Not truncated by count,
	// so releasing a cancelled reservation never forgets an older real hit.
	hits []time.Duration
}

// window is the rule's period plus [EdgeMargin].
func (t *tracked) window() time.Duration { return satAdd(t.rule.Period, EdgeMargin(t.rule.Period)) }

func (t *tracked) prune(now time.Duration) {
	w := t.window()
	i := 0
	for i < len(t.hits) && satAdd(t.hits[i], w) <= now {
		i++
	}
	t.hits = t.hits[i:]
}

// earliest is the earliest start time >= at that keeps this rule satisfied (hits sorted,
// all <= at).
func (t *tracked) earliest(at time.Duration) time.Duration {
	limit := int(t.rule.MaxHits)
	if limit == 0 || len(t.hits) < limit {
		return at
	}
	return max(at, satAdd(t.hits[len(t.hits)-limit], t.window()))
}

func (t *tracked) insert(at time.Duration) {
	pos, _ := slices.BinarySearchFunc(t.hits, at, func(h, target time.Duration) int {
		if h <= target {
			return -1
		}
		return 1
	})
	t.hits = slices.Insert(t.hits, pos, at)
}

// remove removes one hit recorded at exactly at, if still present.
func (t *tracked) remove(at time.Duration) {
	for i, v := range slices.Backward(t.hits) {
		if v == at {
			t.hits = slices.Delete(t.hits, i, i+1)
			return
		}
	}
}

type policy struct {
	// Server-side policy name (`trade-search-request-limit`), once seen.
	name         *string
	rules        []*tracked
	blockedUntil time.Duration
	lastStart    time.Duration
}

func (p *policy) setRules(scope string, rules []Rule) {
	type oldHits struct {
		period time.Duration
		hits   []time.Duration
	}
	var old []*oldHits
	kept := p.rules[:0:0]
	for _, t := range p.rules {
		if t.scope == scope {
			old = append(old, &oldHits{t.rule.Period, t.hits})
		} else {
			kept = append(kept, t)
		}
	}
	p.rules = kept
	for _, r := range rules {
		var hits []time.Duration
		// Like Rust: the first old rule with the same period hands over its hits (once).
		for _, o := range old {
			if o.period == r.Period {
				hits, o.hits = o.hits, nil
				break
			}
		}
		p.rules = append(p.rules, &tracked{scope: scope, rule: r, hits: hits})
	}
}

// RateLimiter tracks policies by name and decides how long to wait before the next request.
// Safe for concurrent use.
//
// Time is measured with the monotonic clock from the limiter's creation, so tests drive it with
// testing/synctest instead of an injected clock (the Rust crate took a `Clock`).
type RateLimiter struct {
	epoch    time.Time
	mu       sync.Mutex
	policies map[string]*policy
}

// DefaultPenalty is the fallback restriction when a 429 arrives without Retry-After or
// penalty info.
const DefaultPenalty = 60 * time.Second

// NewRateLimiter creates an empty limiter.
func NewRateLimiter() *RateLimiter {
	return &RateLimiter{epoch: time.Now(), policies: map[string]*policy{}}
}

// now is the time since the limiter was created.
func (r *RateLimiter) now() time.Duration { return time.Since(r.epoch) }

func (r *RateLimiter) entry(name string) *policy {
	p, ok := r.policies[name]
	if !ok {
		p = &policy{}
		r.policies[name] = p
	}
	return p
}

// Seed pre-loads rules for policy before any response has been seen (e.g. the documented
// trade2 defaults), so the very first burst is already throttled. Rules learnt later from
// headers replace these.
func (r *RateLimiter) Seed(policy string, rules []Rule) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entry(policy).setRules("seed", rules)
}

// Reserve says how long the caller must wait before issuing a request under policy (0 = go
// now). Records the hit optimistically.
func (r *RateLimiter) Reserve(policy string) time.Duration {
	wait, _, _ := r.reserve(policy, nil)
	return wait
}

// TryReserve is like [RateLimiter.Reserve], but refuses (recording nothing) when the wait
// would exceed maxWait; ok is false then and wait is the wait that was refused.
func (r *RateLimiter) TryReserve(policy string, maxWait time.Duration) (wait time.Duration, ok bool) {
	wait, _, ok = r.reserve(policy, &maxWait)
	return wait, ok
}

// ReserveGuarded is like [RateLimiter.TryReserve], returning a [Reservation] that must be
// either committed (the request is being sent) or released (e.g. the caller was cancelled
// while waiting), so abandoned requests do not consume future slots. When refused, res is
// nil, ok false and wait the refused wait.
func (r *RateLimiter) ReserveGuarded(policy string, maxWait time.Duration) (res *Reservation, wait time.Duration, ok bool) {
	wait, start, ok := r.reserve(policy, &maxWait)
	if !ok {
		return nil, wait, false
	}
	return &Reservation{limiter: r, policy: policy, start: start, wait: wait}, wait, true
}

// reserve returns (wait, start, true), or (wait, 0, false) when refused under maxWait.
func (r *RateLimiter) reserve(name string, maxWait *time.Duration) (time.Duration, time.Duration, bool) {
	now := r.now()
	r.mu.Lock()
	defer r.mu.Unlock()
	p := r.entry(name)
	for _, t := range p.rules {
		t.prune(now)
	}
	start := max(now, p.blockedUntil, p.lastStart)
	for _, t := range p.rules {
		start = t.earliest(start)
	}
	wait := satSub(start, now)
	if maxWait != nil && wait > *maxWait {
		return wait, 0, false
	}
	for _, t := range p.rules {
		if t.rule.MaxHits > 0 {
			t.insert(start)
		}
	}
	p.lastStart = start
	return wait, start, true
}

// release un-records a hit previously reserved at start under policy.
func (r *RateLimiter) release(name string, start time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.policies[name]
	if !ok {
		return
	}
	for _, t := range p.rules {
		t.remove(start)
	}
	if p.lastStart == start {
		var last time.Duration
		for _, t := range p.rules {
			if n := len(t.hits); n > 0 {
				last = max(last, t.hits[n-1])
			}
		}
		p.lastStart = last
	}
}

// BlockedFor is the remaining hard restriction (from a 429 or an active penalty) for policy.
// Unlike [RateLimiter.Reserve] this records nothing.
func (r *RateLimiter) BlockedFor(policy string) time.Duration {
	now := r.now()
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.policies[policy]
	if !ok {
		return 0
	}
	return satSub(p.blockedUntil, now)
}

// PolicyName is the server-side policy name last reported for policy.
func (r *RateLimiter) PolicyName(policy string) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.policies[policy]
	if !ok || p.name == nil {
		return "", false
	}
	return *p.name, true
}

// headerStr is `HeaderMap::get(name).and_then(|v| v.to_str().ok())`: the first value, only
// when it is visible ASCII (or tab).
func headerStr(h http.Header, name string) (string, bool) {
	vs := h.Values(name)
	if len(vs) == 0 {
		return "", false
	}
	v := vs[0]
	for i := 0; i < len(v); i++ {
		if c := v[i]; (c < 0x20 && c != '\t') || c >= 0x7f {
			return "", false
		}
	}
	return v, true
}

func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}

// Observe feeds response headers (and status) back in.
func (r *RateLimiter) Observe(policyHint string, status int, headers http.Header) {
	now := r.now()
	r.mu.Lock()
	defer r.mu.Unlock()
	p := r.entry(policyHint)
	if name, ok := headerStr(headers, "x-rate-limit-policy"); ok {
		p.name = &name
	}
	var restricted, maxPenalty time.Duration
	if scopesHdr, ok := headerStr(headers, "x-rate-limit-rules"); ok {
		var scopes []string
		for s := range strings.SplitSeq(scopesHdr, ",") {
			if s = asciiLower(strings.TrimSpace(s)); s != "" {
				scopes = append(scopes, s)
			}
		}
		if len(scopes) > 0 {
			// Real rules supersede seeded defaults.
			p.setRules("seed", nil)
		}
		for _, scope := range scopes {
			h, ok := headerStr(headers, "x-rate-limit-"+scope)
			if !ok {
				continue
			}
			rules, err := ParseRules(h)
			if err != nil {
				continue
			}
			p.setRules(scope, rules)
			for _, rl := range rules {
				maxPenalty = max(maxPenalty, rl.Penalty)
			}
			var states []RuleState
			if sh, ok := headerStr(headers, "x-rate-limit-"+scope+"-state"); ok {
				states, _ = ParseStates(sh)
			}
			for _, st := range states {
				restricted = max(restricted, st.Restricted)
				for _, t := range p.rules {
					if t.scope != scope || t.rule.Period != st.Period {
						continue
					}
					t.prune(now)
					ours := 0
					for _, h := range t.hits {
						if satAdd(h, t.rule.Period) > now && h <= now {
							ours++
						}
					}
					missing := min(max(int(st.Hits)-ours, 0), int(t.rule.MaxHits))
					for range missing {
						t.insert(now)
					}
					t.prune(now)
					break
				}
			}
		}
	}
	if status == http.StatusTooManyRequests {
		var wait time.Duration
		switch {
		case restricted != 0:
			wait = restricted
		case maxPenalty != 0:
			wait = maxPenalty
		default:
			wait = DefaultPenalty
		}
		if v, ok := headerStr(headers, "retry-after"); ok {
			if s, err := parseU64(strings.TrimSpace(v)); err == nil {
				wait = secs(s)
			}
		}
		restricted = max(restricted, wait)
	}
	if restricted != 0 {
		p.blockedUntil = max(p.blockedUntil, satAdd(now, restricted))
	}
}

// Reservation is a recorded hit. Call [Reservation.Commit] when the request is sent, or
// [Reservation.Release] to hand the slot back (the Rust guard released on drop; Go callers
// release explicitly, typically with `defer res.Release()` after which Commit makes the
// release a no-op).
type Reservation struct {
	limiter *RateLimiter
	policy  string
	start   time.Duration
	wait    time.Duration
	mu      sync.Mutex
	done    bool
}

// Wait is how long to wait before sending.
func (s *Reservation) Wait() time.Duration { return s.wait }

// Commit keeps the hit recorded: the request is being sent.
func (s *Reservation) Commit() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.done = true
}

// Release un-records the hit unless already committed or released.
func (s *Reservation) Release() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done {
		return
	}
	s.done = true
	s.limiter.release(s.policy, s.start)
}
