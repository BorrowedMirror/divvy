package app

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mustParseURL(t *testing.T, s string) *url.URL {
	t.Helper()
	u, err := url.Parse(s)
	require.NoError(t, err)
	return u
}

func TestOverrideURLsMustBeHTTPSOrLoopback(t *testing.T) {
	for raw, want := range map[string]bool{
		"https://example.com":    true,
		"http://127.0.0.1:18080": true,
		"http://127.8.9.10":      true,
		"http://localhost:1":     true,
		"http://LOCALHOST:1":     true,
		"http://[::1]:1":         true,
		"http://example.com":     false,
		"http://10.0.0.1":        false,
		"file:///etc/passwd":     false,
		"ftp://127.0.0.1":        false,
		// Only canonical loopback spellings.
		"http://127.1":             false,
		"http://0x7f000001":        false,
		"http://2130706433":        false,
		"http://[::ffff:7f00:1]":   false,
		"http://[::1%25lo]:1":      false,
		"http://256.0.0.1":         false,
		"http://localhost.":        false,
		"http://localhost.evil.io": false,
		"http://user@127.0.0.1:1":  false,
	} {
		assert.Equal(t, want, isSafeOverride(mustParseURL(t, raw)), raw)
	}
	assert.False(t, isSafeOverride(nil))
}

func TestTradeURLAllowList(t *testing.T) {
	for raw, want := range map[string]bool{
		"https://www.pathofexile.com/trade2/search/poe2/Standard/abc": true,
		"https://pathofexile.com/trade/search/Standard/abc":           true,
		"https://www.pathofexile.com:443/trade2/search/x":             true,
		"HTTPS://WWW.PathOfExile.com/trade2/search/x":                 true,
		"https://www.pathofexile.com/trade2/x/..":                     true,
		" https://www.pathofexile.com/tra\tde2/search/x ":             true,
		`https://www.pathofexile.com\trade2\search\x`:                 true,
		"https://www.pathofexile.com/forum/view-thread/1":             false,
		"https://www.pathofexile.com/trade2":                          false,
		"http://www.pathofexile.com/trade2/search/x":                  false,
		"https://www.pathofexile.com:8443/trade2/search/x":            false,
		"https://evil.example.com/trade2/search/x":                    false,
		"not a url": false,
		"":          false,
		"https:www.pathofexile.com/trade2/search/x":                        false,
		"https://www.pathofexile.com/trade2/../forum/x":                    false,
		"https://www.pathofexile.com/trade2/%2e%2E/forum/x":                false,
		"https://www.pathofexile.com/trade2/%2E./forum/x":                  false,
		`https://www.pathofexile.com/trade2\..\forum`:                      false,
		"https://www.pathofexile.com/trade2%2Fsearch":                      false,
		"https://www.pathofexile.com/trade2/%5c..%5cforum":                 false,
		"https://www.pathofexile.com/forum?/trade2/x":                      false,
		"https://www.pathofexile.com/forum#/trade2/x":                      false,
		"https://www.pathofexile.com@evil.example.com/trade2/x":            false,
		"https://user:pw@www.pathofexile.com/trade2/x":                     false,
		`https://www.pathofexile.com\@evil.example.com/trade2/x`:           false,
		"https://evil.example.com#@www.pathofexile.com/trade2/x":           false,
		"https://www.pathofexile.com./trade2/search/x":                     false,
		"https://www.pathofexile.com.evil.example.com/trade2/search/x":     false,
		"https://www.pathofexіle.com/trade2/search/x":                      false, // Cyrillic і
		"https://xn--www.pathofexle-yz6g.com/trade2/search/x":              false,
		"javascript://www.pathofexile.com/trade2/%0aalert(1)":              false,
		"https://www.pathofexile.com:443@evil.example.com/trade2/search/x": false,
	} {
		_, got := tradeURL(raw, nil)
		assert.Equal(t, want, got, "%q", raw)
	}
	u, ok := tradeURL(" https://www.pathofexile.com\\trade2\\search\\x ", nil)
	require.True(t, ok)
	assert.Equal(t, "https://www.pathofexile.com/trade2/search/x", u, "the checked URL is what opens")

	local := mustParseURL(t, "http://127.0.0.1:18080")
	_, ok = tradeURL("http://127.0.0.1:18080/trade2/search/poe2/x", local)
	assert.True(t, ok, "override origin")
	_, ok = tradeURL("http://127.0.0.1:9999/trade2/search/poe2/x", local)
	assert.False(t, ok, "other port")
	_, ok = tradeURL("http://127.0.0.1:18080/forum/x", local)
	assert.False(t, ok, "override origin, other path")
	_, ok = tradeURL("https://www.pathofexile.com/trade2/search/x", local)
	assert.True(t, ok, "the official site stays allowed")
	remote := mustParseURL(t, "http://evil.example.com")
	_, ok = tradeURL("http://evil.example.com/trade2/search/x", remote)
	assert.False(t, ok, "unsafe override")
}
