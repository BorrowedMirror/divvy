package trade

import (
	"fmt"
	"math"
	"math/rand/v2"
	"net/http"
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The Rust tests drove the limiter with an injected FakeClock; here each test runs in a
// synctest bubble, where time.Sleep advances the fake clock instantly (clock.advance(d) in
// Rust is time.Sleep(d) here) and time never moves otherwise.

const durationMax = time.Duration(math.MaxInt64)

func hdrs(pairs ...string) http.Header {
	h := http.Header{}
	for i := 0; i < len(pairs); i += 2 {
		h.Set(pairs[i], pairs[i+1])
	}
	return h
}

func rule(h uint32, p, x uint64) Rule {
	return Rule{MaxHits: h, Period: time.Duration(p) * time.Second, Penalty: time.Duration(x) * time.Second}
}

func TestParsesRulesAndStates(t *testing.T) {
	r, err := ParseRules("5:10:60,15:60:300")
	require.NoError(t, err)
	assert.Equal(t, []Rule{rule(5, 10, 60), rule(15, 60, 300)}, r)
	s, err := ParseStates(" 1:10:0 , 6:60:120")
	require.NoError(t, err)
	assert.Equal(t, []RuleState{
		{Hits: 1, Period: 10 * time.Second, Restricted: 0},
		{Hits: 6, Period: 60 * time.Second, Restricted: 120 * time.Second},
	}, s)
	r, err = ParseRules("")
	require.NoError(t, err)
	assert.Empty(t, r)
	errContains := func(err error, want string) {
		t.Helper()
		require.Error(t, err)
		assert.Contains(t, err.Error(), want)
	}
	_, err = ParseRules("5:10")
	errContains(err, "missing penalty")
	_, err = ParseRules("5:10:1:2")
	errContains(err, "too many")
	_, err = ParseRules("x:10:1")
	errContains(err, "bad hits")
	_, err = ParseRules("99999999999:10:1")
	errContains(err, "bad hits")
	_, err = ParseStates("1:y:0")
	errContains(err, "bad period")
}

func TestSystemClockIsMonotonic(t *testing.T) {
	rl := NewRateLimiter()
	a := rl.now()
	assert.GreaterOrEqual(t, rl.now(), a)
}

func TestReserveWithoutRulesIsFree(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rl := NewRateLimiter()
		for range 100 {
			assert.Equal(t, time.Duration(0), rl.Reserve("search"))
		}
		assert.Equal(t, time.Duration(0), rl.now())
	})
}

func TestLearnsRulesFromHeadersAndThrottles(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rl := NewRateLimiter()
		assert.Equal(t, time.Duration(0), rl.Reserve("search"))
		rl.Observe("search", 200, hdrs(
			"x-rate-limit-policy", "trade-search-request-limit",
			"x-rate-limit-rules", "Ip",
			"x-rate-limit-ip", "2:10:60",
			"x-rate-limit-ip-state", "1:10:0",
		))
		name, ok := rl.PolicyName("search")
		assert.True(t, ok)
		assert.Equal(t, "trade-search-request-limit", name)
		assert.Equal(t, time.Duration(0), rl.Reserve("search"))
		// Third request inside 10s must wait for the first hit to leave the window (plus
		// the 500ms edge margin for a 10s period).
		assert.Equal(t, 10_500*time.Millisecond, rl.Reserve("search"))
		time.Sleep(10_500 * time.Millisecond)
		// Window from 10.5s holds the reservation made for t=10.5 plus this one...
		assert.Equal(t, time.Duration(0), rl.Reserve("search"))
		// ...so a third one has to wait until t=21.
		assert.Equal(t, 10_500*time.Millisecond, rl.Reserve("search"))
	})
}

func TestServerStateAddsUnseenHits(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rl := NewRateLimiter()
		rl.Observe("fetch", 200, hdrs(
			"x-rate-limit-rules", "Account,Ip",
			"x-rate-limit-account", "3:5:60",
			"x-rate-limit-account-state", "3:5:0",
			"x-rate-limit-ip", "10:10:60",
			"x-rate-limit-ip-state", "1:10:0",
		))
		// Account window is already full according to the server.
		assert.Equal(t, 5_250*time.Millisecond, rl.Reserve("fetch"))
	})
}

func TestTooManyRequestsHonoursRetryAfter(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rl := NewRateLimiter()
		rl.Observe("search", 429, hdrs("retry-after", "30"))
		assert.Equal(t, 30*time.Second, rl.BlockedFor("search"))
		assert.Equal(t, time.Duration(0), rl.BlockedFor("other"))
		assert.Equal(t, 30*time.Second, rl.Reserve("search"))
		time.Sleep(31 * time.Second)
		assert.Equal(t, time.Duration(0), rl.BlockedFor("search"))
	})
}

func TestTooManyRequestsWithoutRetryAfterUsesStateThenPenaltyThenDefault(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rl := NewRateLimiter()
		rl.Observe("a", 429, hdrs(
			"x-rate-limit-rules", "ip",
			"x-rate-limit-ip", "5:10:60",
			"x-rate-limit-ip-state", "6:10:45",
		))
		assert.Equal(t, 45*time.Second, rl.BlockedFor("a"))
		rl.Observe("b", 429, hdrs("x-rate-limit-rules", "ip", "x-rate-limit-ip", "5:10:90"))
		assert.Equal(t, 90*time.Second, rl.BlockedFor("b"))
		rl.Observe("c", 429, http.Header{})
		assert.Equal(t, DefaultPenalty, rl.BlockedFor("c"))
	})
}

func TestActiveRestrictionStateBlocksEvenOnSuccess(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rl := NewRateLimiter()
		rl.Observe("x", 200, hdrs(
			"x-rate-limit-rules", "Ip",
			"x-rate-limit-ip", "5:10:60",
			"x-rate-limit-ip-state", "1:10:20",
		))
		assert.Equal(t, 20*time.Second, rl.Reserve("x"))
	})
}

func TestSeededRulesApplyUntilReplaced(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rl := NewRateLimiter()
		rl.Seed("s", []Rule{rule(1, 4, 0)})
		assert.Equal(t, time.Duration(0), rl.Reserve("s"))
		assert.Equal(t, 4_250*time.Millisecond, rl.Reserve("s"))
		rl.Observe("s", 200, hdrs("x-rate-limit-rules", "Ip", "x-rate-limit-ip", "100:4:0"))
		// seed rule gone; real rule is generous but monotonic ordering is preserved.
		assert.Equal(t, 4_250*time.Millisecond, rl.Reserve("s"))
	})
}

func TestMalformedHeadersAreIgnored(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rl := NewRateLimiter()
		rl.Observe("m", 200, hdrs(
			"x-rate-limit-rules", "Ip, ,Account",
			"x-rate-limit-ip", "garbage",
			"x-rate-limit-ip-state", "x",
		))
		assert.Equal(t, time.Duration(0), rl.Reserve("m"))
	})
}

func TestRuleChangeKeepsHitsForSamePeriod(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rl := NewRateLimiter()
		h := func(r string) http.Header { return hdrs("x-rate-limit-rules", "Ip", "x-rate-limit-ip", r) }
		rl.Observe("p", 200, h("3:10:0"))
		rl.Reserve("p")
		rl.Reserve("p")
		rl.Reserve("p")
		rl.Observe("p", 200, h("2:10:0,50:60:0"))
		assert.Equal(t, 10_500*time.Millisecond, rl.Reserve("p"))
	})
}

func TestZeroHitRulesDoNotBlock(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rl := NewRateLimiter()
		rl.Seed("z", []Rule{rule(0, 10, 0)})
		assert.Equal(t, time.Duration(0), rl.Reserve("z"))
	})
}

// Property tests (proptest in Rust): 256 deterministic pseudo-random cases each.

const propCases = 256

func newRand(seed uint64) *rand.Rand { return rand.New(rand.NewPCG(seed, 0x5eed)) }

func arbRules(r *rand.Rand) []Rule {
	n := 1 + r.IntN(3)
	out := make([]Rule, n)
	for i := range out {
		out[i] = rule(uint32(1+r.IntN(5)), uint64(1+r.IntN(29)), uint64(r.IntN(100)))
	}
	return out
}

func TestRulesRoundTrip(t *testing.T) {
	r := newRand(1)
	for range propCases {
		rules := make([]Rule, r.IntN(6))
		for i := range rules {
			rules[i] = rule(uint32(r.IntN(10_000)), uint64(r.IntN(100_000)), uint64(r.IntN(100_000)))
		}
		got, err := ParseRules(FormatRules(rules))
		require.NoError(t, err)
		if len(rules) == 0 {
			assert.Empty(t, got)
			continue
		}
		require.Equal(t, rules, got)
	}
}

func TestStatesRoundTrip(t *testing.T) {
	r := newRand(2)
	for range propCases {
		states := make([]RuleState, r.IntN(6))
		for i := range states {
			states[i] = RuleState{
				Hits:       uint32(r.IntN(10_000)),
				Period:     time.Duration(r.IntN(100_000)) * time.Second,
				Restricted: time.Duration(r.IntN(100_000)) * time.Second,
			}
		}
		got, err := ParseStates(FormatStates(states))
		require.NoError(t, err)
		if len(states) == 0 {
			assert.Empty(t, got)
			continue
		}
		require.Equal(t, states, got)
	}
}

// checkWindows: every window of period + EdgeMargin holds at most MaxHits starts.
func checkWindows(t *testing.T, rules []Rule, sorted []time.Duration) {
	t.Helper()
	for _, r := range rules {
		window := r.Period + EdgeMargin(r.Period)
		for i, s := range sorted {
			inWindow := 0
			for _, x := range sorted[i:] {
				if x >= s+window {
					break
				}
				inWindow++
			}
			require.LessOrEqual(t, inWindow, int(r.MaxHits), "rule %s saw %d hits from %v", r, inWindow, s)
		}
	}
}

func TestNeverExceedsMaxHitsInAnyWindow(t *testing.T) {
	r := newRand(3)
	for c := range propCases {
		rules := arbRules(r)
		gaps := make([]time.Duration, 1+r.IntN(59))
		for i := range gaps {
			gaps[i] = time.Duration(r.IntN(5_000)) * time.Millisecond
		}
		t.Run(fmt.Sprint(c), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				rl := NewRateLimiter()
				rl.Seed("p", rules)
				var starts []time.Duration
				for _, gap := range gaps {
					time.Sleep(gap)
					wait := rl.Reserve("p")
					starts = append(starts, rl.now()+wait)
				}
				sorted := slices.Clone(starts)
				slices.Sort(sorted)
				require.Equal(t, sorted, starts, "start times must be monotonic")
				checkWindows(t, rules, sorted)
			})
		})
	}
}

// Mixed reserve / try-reserve / guarded-and-cancelled sequences: the hits that were actually
// sent (committed or plain reserved) still respect every window, and refusals or
// cancellations never push later requests further out.
func TestCancellationsAndRefusalsNeverExceedMaxHits(t *testing.T) {
	r := newRand(4)
	type op struct {
		gap     time.Duration
		kind    int
		maxWait time.Duration
	}
	for c := range propCases {
		rules := arbRules(r)
		ops := make([]op, 1+r.IntN(59))
		for i := range ops {
			ops[i] = op{
				time.Duration(r.IntN(5_000)) * time.Millisecond,
				r.IntN(4),
				time.Duration(r.IntN(20_000)) * time.Millisecond,
			}
		}
		t.Run(fmt.Sprint(c), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				rl := NewRateLimiter()
				rl.Seed("p", rules)
				var sent []time.Duration
				for _, o := range ops {
					time.Sleep(o.gap)
					switch o.kind {
					case 0:
						sent = append(sent, rl.now()+rl.Reserve("p"))
					case 1:
						if wait, ok := rl.TryReserve("p", o.maxWait); ok {
							sent = append(sent, rl.now()+wait)
						}
					case 2:
						if slot, _, ok := rl.ReserveGuarded("p", o.maxWait); ok {
							sent = append(sent, rl.now()+slot.Wait())
							slot.Commit()
						}
					default:
						// Cancelled before sending: the slot must be handed back.
						if wait, ok := rl.TryReserve("p", durationMax); ok {
							sent = append(sent, rl.now()+wait)
						}
						probe, _, ok1 := rl.ReserveGuarded("p", durationMax)
						require.True(t, ok1)
						probeWait := probe.Wait()
						probe.Release()
						again, _, ok2 := rl.ReserveGuarded("p", durationMax)
						require.True(t, ok2)
						againWait := again.Wait()
						again.Release()
						require.Equal(t, probeWait, againWait, "released guard must release its slot")
					}
				}
				sorted := slices.Clone(sent)
				slices.Sort(sorted)
				checkWindows(t, rules, sorted)
			})
		})
	}
}

func TestEdgeMarginIsFivePercentWithFloor(t *testing.T) {
	assert.Equal(t, 250*time.Millisecond, EdgeMargin(time.Second))
	assert.Equal(t, 500*time.Millisecond, EdgeMargin(10*time.Second))
	assert.Equal(t, 3*time.Second, EdgeMargin(60*time.Second))
}

func TestHitsStayInWindowUntilMarginPasses(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rl := NewRateLimiter()
		rl.Seed("e", []Rule{rule(1, 10, 0)})
		assert.Equal(t, time.Duration(0), rl.Reserve("e"))
		// Exactly at the server window edge we still hold back for the margin.
		time.Sleep(10 * time.Second)
		wait, ok := rl.TryReserve("e", 0)
		assert.False(t, ok)
		assert.Equal(t, 500*time.Millisecond, wait)
		time.Sleep(500 * time.Millisecond)
		wait, ok = rl.TryReserve("e", 0)
		assert.True(t, ok)
		assert.Equal(t, time.Duration(0), wait)
	})
}

func TestRefusedTryReserveRecordsNothing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rl := NewRateLimiter()
		rl.Seed("r", []Rule{rule(1, 10, 0)})
		wait, ok := rl.TryReserve("r", 0)
		assert.True(t, ok)
		assert.Equal(t, time.Duration(0), wait)
		refused, ok := rl.TryReserve("r", time.Second)
		assert.False(t, ok)
		assert.Equal(t, 10_500*time.Millisecond, refused)
		// Repeated refusals do not push the next slot further out.
		for range 5 {
			w, ok := rl.TryReserve("r", time.Second)
			assert.False(t, ok)
			assert.Equal(t, refused, w)
		}
		w, ok := rl.TryReserve("r", durationMax)
		assert.True(t, ok)
		assert.Equal(t, refused, w)
	})
}

func TestDroppedReservationReleasesSlot(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rl := NewRateLimiter()
		rl.Seed("g", []Rule{rule(1, 10, 0)})
		slot, _, ok := rl.ReserveGuarded("g", durationMax)
		require.True(t, ok)
		assert.Equal(t, time.Duration(0), slot.Wait())
		slot.Release()
		// Cancelled: the slot is free again.
		slot, _, ok = rl.ReserveGuarded("g", durationMax)
		require.True(t, ok)
		assert.Equal(t, time.Duration(0), slot.Wait())
		slot.Commit()
		slot.Release() // no-op after commit (the Rust guard was consumed by commit)
		// Committed: the next one has to wait, and cancelling a later one keeps order.
		later, _, ok := rl.ReserveGuarded("g", durationMax)
		require.True(t, ok)
		assert.Equal(t, 10_500*time.Millisecond, later.Wait())
		latest, _, ok := rl.ReserveGuarded("g", durationMax)
		require.True(t, ok)
		assert.Equal(t, 21*time.Second, latest.Wait())
		later.Release()
		latest.Release()
		assert.Equal(t, 10_500*time.Millisecond, rl.Reserve("g"))
		// Refusal through the guarded API also records nothing.
		res, refused, ok := rl.ReserveGuarded("g", 0)
		assert.False(t, ok)
		assert.Nil(t, res)
		assert.Equal(t, 21*time.Second, refused)
		// Releasing for an unknown policy is a no-op.
		rl.release("unknown", 0)
	})
}
