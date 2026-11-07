package trade

import (
	"strings"
	"testing"
	"time"

	"github.com/BorrowedMirror/rust-king-ledger/internal/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func u64p(v uint64) *uint64 { return &v }

func TestFlattensEveryVariant(t *testing.T) {
	cases := []struct {
		err   *Error
		kind  string
		retry *uint64
	}{
		{RateLimitedError(1500 * time.Millisecond), "rate_limited", u64p(1500)},
		{HTTPError(502, "bad"), "http", nil},
		{DecodeError("eof"), "decode", nil},
		{UnsupportedError("poe.ninja"), "unsupported", nil},
		{ConfigError("nope"), "config", nil},
	}
	for _, c := range cases {
		info := c.err.Info()
		assert.Equal(t, c.kind, info.Kind)
		assert.Equal(t, c.retry, info.RetryAfterMs)
		assert.Equal(t, c.err.Error(), info.Message)
	}
	assert.Equal(t, "http 502: bad", HTTPError(502, "bad").Error())
}

func TestDefaultUserAgentHasVersionAndContact(t *testing.T) {
	ua := DefaultUserAgent()
	assert.True(t, strings.HasPrefix(ua, "rkl/"+Version))
	assert.Contains(t, ua, "github.com/BorrowedMirror/rust-king-ledger")
}

// Go-only: display strings match thiserror + Duration's Debug, and ErrorInfo's JSON
// matches serde.
func TestErrorDisplayAndInfoJSON(t *testing.T) {
	assert.Equal(t, "rate limited; retry after 1.5s", RateLimitedError(1500*time.Millisecond).Error())
	assert.Equal(t, "rate limited; retry after 60s", RateLimitedError(time.Minute).Error())
	assert.Equal(t, "rate limited; retry after 59.998123s", RateLimitedError(59998123*time.Microsecond).Error())
	assert.Equal(t, "rate limited; retry after 250ms", RateLimitedError(250*time.Millisecond).Error())
	assert.Equal(t, "rate limited; retry after 1.5µs", RateLimitedError(1500*time.Nanosecond).Error())
	assert.Equal(t, "rate limited; retry after 0ns", RateLimitedError(0).Error())
	assert.Equal(t, "item not supported by price source `poe2scout`", UnsupportedError("poe2scout").Error())
	assert.Equal(t, "invalid configuration: x", ConfigError("x").Error())
	assert.Equal(t, "invalid response: x", DecodeError("x").Error())

	b, err := core.MarshalSerde(RateLimitedError(2 * time.Second).Info())
	require.NoError(t, err)
	assert.Equal(t, `{"kind":"rate_limited","message":"rate limited; retry after 2s","retry_after_ms":2000}`, string(b))
	b, err = core.MarshalSerde(HTTPError(500, "x").Info())
	require.NoError(t, err)
	assert.Equal(t, `{"kind":"http","message":"http 500: x","retry_after_ms":null}`, string(b))
}
