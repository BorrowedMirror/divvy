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
	testCases := []struct {
		name     string
		have     string
		expected bool
	}{
		{
			"HTTPS",
			"https://example.com",
			true,
		},
		{
			"LoopbackIPv4",
			"http://127.0.0.1:18080",
			true,
		},
		{
			"LoopbackRangeIPv4",
			"http://127.8.9.10",
			true,
		},
		{
			"Localhost",
			"http://localhost:1",
			true,
		},
		{
			"UppercaseLocalhost",
			"http://LOCALHOST:1",
			true,
		},
		{
			"LoopbackIPv6",
			"http://[::1]:1",
			true,
		},
		{
			"RejectsPlainHTTP",
			"http://example.com",
			false,
		},
		{
			"RejectsPrivateIPv4",
			"http://10.0.0.1",
			false,
		},
		{
			"RejectsFileScheme",
			"file:///etc/passwd",
			false,
		},
		{
			"RejectsFTPScheme",
			"ftp://127.0.0.1",
			false,
		},
		// Only canonical loopback spellings.
		{
			"RejectsShortIPv4",
			"http://127.1",
			false,
		},
		{
			"RejectsHexIPv4",
			"http://0x7f000001",
			false,
		},
		{
			"RejectsDecimalIPv4",
			"http://2130706433",
			false,
		},
		{
			"RejectsIPv4MappedIPv6",
			"http://[::ffff:7f00:1]",
			false,
		},
		{
			"RejectsIPv6Zone",
			"http://[::1%25lo]:1",
			false,
		},
		{
			"RejectsInvalidIPv4",
			"http://256.0.0.1",
			false,
		},
		{
			"RejectsTrailingDotLocalhost",
			"http://localhost.",
			false,
		},
		{
			"RejectsLocalhostSubdomain",
			"http://localhost.evil.io",
			false,
		},
		{
			"RejectsUserInfo",
			"http://user@127.0.0.1:1",
			false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, isSafeOverride(mustParseURL(t, tc.have)))
		})
	}

	assert.False(t, isSafeOverride(nil))
}

func TestTradeURLAllowList(t *testing.T) {
	testCases := []struct {
		name     string
		have     string
		expected bool
	}{
		{
			"Trade2Search",
			"https://www.pathofexile.com/trade2/search/poe2/Standard/abc",
			true,
		},
		{
			"BareDomainTrade",
			"https://pathofexile.com/trade/search/Standard/abc",
			true,
		},
		{
			"ExplicitDefaultPort",
			"https://www.pathofexile.com:443/trade2/search/x",
			true,
		},
		{
			"UppercaseSchemeAndHost",
			"HTTPS://WWW.PathOfExile.com/trade2/search/x",
			true,
		},
		{
			"DotDotStayingInTrade",
			"https://www.pathofexile.com/trade2/x/..",
			true,
		},
		{
			"SurroundingSpacesAndTab",
			" https://www.pathofexile.com/tra\tde2/search/x ",
			true,
		},
		{
			"Backslashes",
			`https://www.pathofexile.com\trade2\search\x`,
			true,
		},
		{
			"RejectsForum",
			"https://www.pathofexile.com/forum/view-thread/1",
			false,
		},
		{
			"RejectsBareTrade2",
			"https://www.pathofexile.com/trade2",
			false,
		},
		{
			"RejectsHTTP",
			"http://www.pathofexile.com/trade2/search/x",
			false,
		},
		{
			"RejectsOtherPort",
			"https://www.pathofexile.com:8443/trade2/search/x",
			false,
		},
		{
			"RejectsOtherHost",
			"https://evil.example.com/trade2/search/x",
			false,
		},
		{
			"RejectsNonURL",
			"not a url",
			false,
		},
		{
			"RejectsEmpty",
			"",
			false,
		},
		{
			"RejectsOpaqueURL",
			"https:www.pathofexile.com/trade2/search/x",
			false,
		},
		{
			"RejectsDotDotEscape",
			"https://www.pathofexile.com/trade2/../forum/x",
			false,
		},
		{
			"RejectsEncodedDotDotEscape",
			"https://www.pathofexile.com/trade2/%2e%2E/forum/x",
			false,
		},
		{
			"RejectsPartlyEncodedDotDotEscape",
			"https://www.pathofexile.com/trade2/%2E./forum/x",
			false,
		},
		{
			"RejectsBackslashDotDotEscape",
			`https://www.pathofexile.com/trade2\..\forum`,
			false,
		},
		{
			"RejectsEncodedSlash",
			"https://www.pathofexile.com/trade2%2Fsearch",
			false,
		},
		{
			"RejectsEncodedBackslashEscape",
			"https://www.pathofexile.com/trade2/%5c..%5cforum",
			false,
		},
		{
			"RejectsTradeInQuery",
			"https://www.pathofexile.com/forum?/trade2/x",
			false,
		},
		{
			"RejectsTradeInFragment",
			"https://www.pathofexile.com/forum#/trade2/x",
			false,
		},
		{
			"RejectsHostAsUserInfo",
			"https://www.pathofexile.com@evil.example.com/trade2/x",
			false,
		},
		{
			"RejectsUserInfo",
			"https://user:pw@www.pathofexile.com/trade2/x",
			false,
		},
		{
			"RejectsBackslashUserInfo",
			`https://www.pathofexile.com\@evil.example.com/trade2/x`,
			false,
		},
		{
			"RejectsHostInFragment",
			"https://evil.example.com#@www.pathofexile.com/trade2/x",
			false,
		},
		{
			"RejectsTrailingDotHost",
			"https://www.pathofexile.com./trade2/search/x",
			false,
		},
		{
			"RejectsLookalikeSubdomain",
			"https://www.pathofexile.com.evil.example.com/trade2/search/x",
			false,
		},
		{
			"RejectsHomoglyphHost",
			"https://www.pathofexіle.com/trade2/search/x", // Cyrillic і
			false,
		},
		{
			"RejectsPunycodeHost",
			"https://xn--www.pathofexle-yz6g.com/trade2/search/x",
			false,
		},
		{
			"RejectsJavaScriptScheme",
			"javascript://www.pathofexile.com/trade2/%0aalert(1)",
			false,
		},
		{
			"RejectsPortAsUserInfo",
			"https://www.pathofexile.com:443@evil.example.com/trade2/search/x",
			false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, got := tradeURL(tc.have, nil)
			assert.Equal(t, tc.expected, got)
		})
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
