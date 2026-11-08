// Package ratelimit is a header-driven rate limiter for GGG APIs.
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
package ratelimit

import (
	"fmt"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
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

// parseUint parses a decimal number with an optional `+` sign.
func parseUint(s string) (uint64, error) {
	return strconv.ParseUint(strings.TrimPrefix(s, "+"), 10, 64)
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
			v, err := parseUint(strings.TrimSpace(fields[i]))
			if err != nil {
				return nil, fmt.Errorf("bad %s in `%s`: %w", what, part, err)
			}
			if i > 0 && v > maxSecs {
				return nil, fmt.Errorf("bad %s in `%s`: %d seconds is too long", what, part, v)
			}
			nums[i] = v
		}
		if len(fields) > 3 {
			return nil, fmt.Errorf("too many fields in `%s`", part)
		}
		if nums[0] > math.MaxUint32 {
			return nil, fmt.Errorf("bad hits in `%s`: %d is too many", part, nums[0])
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

// edgeMargin is the safety margin added to every window. GGG counts hits on arrival, so
// latency jitter can squeeze MaxHits+1 requests into a server-side window if we schedule
// exactly at the edge; the margin is `max(period / 20, 250ms)`.
func edgeMargin(period time.Duration) time.Duration {
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

// window is the rule's period plus [edgeMargin].
func (t *tracked) window() time.Duration { return satAdd(t.rule.Period, edgeMargin(t.rule.Period)) }

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
	name         string
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
		// The first old rule with the same period hands over its hits (once).
		for _, o := range old {
			if o.period == r.Period {
				hits, o.hits = o.hits, nil
				break
			}
		}
		p.rules = append(p.rules, &tracked{scope: scope, rule: r, hits: hits})
	}
}

// Limiter tracks policies by name and decides how long to wait before the next request.
// Safe for concurrent use.
//
// Time is measured with the monotonic clock from the limiter's creation (tests drive it with
// testing/synctest).
type Limiter struct {
	epoch    time.Time
	mu       sync.Mutex
	policies map[string]*policy
}

// defaultPenalty is the fallback restriction when a 429 arrives without Retry-After or
// penalty info.
const defaultPenalty = 60 * time.Second

// unbounded is a maxWait that never refuses.
const unbounded = time.Duration(math.MaxInt64)

// New creates an empty limiter.
func New() *Limiter {
	return &Limiter{epoch: time.Now(), policies: map[string]*policy{}}
}

// now is the time since the limiter was created.
func (r *Limiter) now() time.Duration { return time.Since(r.epoch) }

func (r *Limiter) entry(name string) *policy {
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
func (r *Limiter) Seed(policy string, rules []Rule) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entry(policy).setRules("seed", rules)
}

// Reserve records a hit under policy, returning a [Reservation] whose Wait is how long to
// wait before sending; it must be either committed (the request is being sent) or released
// (e.g. the caller was cancelled while waiting), so abandoned requests do not consume future
// slots. When the wait would exceed maxWait it records nothing: res is nil, ok false and
// wait the refused wait.
func (r *Limiter) Reserve(policy string, maxWait time.Duration) (res *Reservation, wait time.Duration, ok bool) {
	wait, start, ok := r.reserve(policy, maxWait)
	if !ok {
		return nil, wait, false
	}
	return &Reservation{limiter: r, policy: policy, start: start, wait: wait}, wait, true
}

// reserve returns (wait, start, true), or (wait, 0, false) when refused under maxWait.
func (r *Limiter) reserve(name string, maxWait time.Duration) (time.Duration, time.Duration, bool) {
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
	if wait > maxWait {
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
func (r *Limiter) release(name string, start time.Duration) {
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
// Unlike [Limiter.Reserve] this records nothing.
func (r *Limiter) BlockedFor(policy string) time.Duration {
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
func (r *Limiter) PolicyName(policy string) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.policies[policy]
	if !ok || p.name == "" {
		return "", false
	}
	return p.name, true
}

// Observe feeds response headers (and status) back in.
func (r *Limiter) Observe(policyHint string, status int, headers http.Header) {
	now := r.now()
	r.mu.Lock()
	defer r.mu.Unlock()
	p := r.entry(policyHint)
	if name := headers.Get("X-Rate-Limit-Policy"); name != "" {
		p.name = name
	}
	var restricted, maxPenalty time.Duration
	if scopesHdr := headers.Get("X-Rate-Limit-Rules"); scopesHdr != "" {
		var scopes []string
		for s := range strings.SplitSeq(scopesHdr, ",") {
			if s = strings.ToLower(strings.TrimSpace(s)); s != "" {
				scopes = append(scopes, s)
			}
		}
		if len(scopes) > 0 {
			// Real rules supersede seeded defaults.
			p.setRules("seed", nil)
		}
		for _, scope := range scopes {
			h := headers.Get("X-Rate-Limit-" + scope)
			if h == "" {
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
			if sh := headers.Get("X-Rate-Limit-" + scope + "-State"); sh != "" {
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
			wait = defaultPenalty
		}
		if v := headers.Get("Retry-After"); v != "" {
			if s, err := parseUint(strings.TrimSpace(v)); err == nil {
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
// [Reservation.Release] to hand the slot back; a typical caller defers Release, which is a
// no-op after Commit.
type Reservation struct {
	limiter *Limiter
	policy  string
	start   time.Duration
	wait    time.Duration
	done    atomic.Bool
}

// Wait is how long to wait before sending.
func (r *Reservation) Wait() time.Duration { return r.wait }

// Commit keeps the hit recorded: the request is being sent.
func (r *Reservation) Commit() { r.done.Store(true) }

// Release un-records the hit unless already committed or released.
func (r *Reservation) Release() {
	if r.done.CompareAndSwap(false, true) {
		r.limiter.release(r.policy, r.start)
	}
}
