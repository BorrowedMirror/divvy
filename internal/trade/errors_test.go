package trade

import (
	"encoding/json/v2"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFlattensEveryKind(t *testing.T) {
	cases := []struct {
		err   *Error
		kind  string
		retry *uint64
	}{
		{rateLimitedError(1500 * time.Millisecond), "rate_limited", new(uint64(1500))},
		{httpError(502, "bad"), "http", nil},
		{decodeError("eof"), "decode", nil},
		{unsupportedError("poe.ninja"), "unsupported", nil},
		{configError("nope"), "config", nil},
	}
	for _, c := range cases {
		info := c.err.Info()
		assert.Equal(t, c.kind, info.Kind)
		assert.Equal(t, c.retry, info.RetryAfterMS)
		assert.Equal(t, c.err.Error(), info.Message)
	}
	assert.Equal(t, "http 502: bad", httpError(502, "bad").Error())
}

func TestDefaultUserAgentHasVersionAndContact(t *testing.T) {
	ua := DefaultUserAgent("1.2.3")
	assert.True(t, strings.HasPrefix(ua, "divvy/1.2.3 "), ua)
	assert.Contains(t, ua, "github.com/BorrowedMirror/divvy")
}

func TestErrorMessagesAndInfoJSON(t *testing.T) {
	assert.Equal(t, "rate limited; retry after 2s", rateLimitedError(1500*time.Millisecond).Error())
	assert.Equal(t, "rate limited; retry after 60s", rateLimitedError(time.Minute).Error())
	assert.Equal(t, "rate limited; retry after 60s", rateLimitedError(59998123*time.Microsecond).Error())
	assert.Equal(t, "rate limited; retry after 250ms", rateLimitedError(250*time.Millisecond).Error())
	assert.Equal(t, "rate limited; retry after 0s", rateLimitedError(0).Error())
	assert.Equal(t, "item not supported by price source `poe2scout`", unsupportedError("poe2scout").Error())
	assert.Equal(t, "invalid configuration: x", configError("x").Error())
	assert.Equal(t, "invalid response: x", decodeError("x").Error())
	assert.Equal(t, "transport error: <nil>", transportError(nil).Error())

	b, err := json.Marshal(rateLimitedError(2 * time.Second).Info())
	require.NoError(t, err)
	assert.JSONEq(t, `{"kind":"rate_limited","message":"rate limited; retry after 2s","retry_after_ms":2000}`, string(b))
	b, err = json.Marshal(httpError(500, "x").Info())
	require.NoError(t, err)
	assert.JSONEq(t, `{"kind":"http","message":"http 500: x","retry_after_ms":null}`, string(b))
}
