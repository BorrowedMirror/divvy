package app

import (
	"encoding/json/v2"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLookupURLsUseFixedHostsAndEncodeNames(t *testing.T) {
	for _, c := range []struct {
		site LookupSite
		name string
		want string // "" = none
	}{
		{LookupPoe2db, "Gold Ring", "https://poe2db.tw/us/Gold_Ring"},
		{LookupWiki, "Ventor's Gamble", "https://www.poe2wiki.net/wiki/Ventor%27s_Gamble"},
		{LookupWiki, "a/b?c#d", "https://www.poe2wiki.net/wiki/a%2Fb%3Fc%23d"},
		{LookupPoe2db, "  ", ""},
		{LookupCraftOfExile, "", "https://beta.craftofexile.com/?game=poe2"},
		{LookupWiki, `100% "x" <y> {z} a\b`, "https://www.poe2wiki.net/wiki/100%25_%22x%22_%3Cy%3E_%7Bz%7D_a%5Cb"},
		{LookupPoe2db, "Kalandra's Touché", "https://poe2db.tw/us/Kalandra%27s_Touch%C3%A9"},
		{LookupPoe2db, "a\tb", "https://poe2db.tw/us/a%09b"},
	} {
		got, ok := lookupURL(c.site, c.name, "")
		assert.Equal(t, c.want != "", ok, "%s %q", c.site, c.name)
		assert.Equal(t, c.want, got, "%s %q", c.site, c.name)
	}
}

func TestLookupNamesNeverLeaveTheSite(t *testing.T) {
	for _, name := range []string{"..", "../../evil.com", "//evil.com", "@evil.com", `\\evil.com`, "a/../../b"} {
		for _, site := range []LookupSite{LookupPoe2db, LookupWiki} {
			raw, ok := lookupURL(site, name, "")
			require.True(t, ok, name)
			u, err := url.Parse(raw)
			require.NoError(t, err, raw)
			assert.Contains(t, []string{"poe2db.tw", "www.poe2wiki.net"}, u.Host, raw)
			assert.Nil(t, u.User, raw)
			assert.NotContains(t, strings.TrimPrefix(u.EscapedPath(), map[LookupSite]string{LookupPoe2db: "/us/", LookupWiki: "/wiki/"}[site]), "/", raw)
		}
	}
}

func TestCraftOfExileImportsTheItemTextFromTheURL(t *testing.T) {
	text := "Item Class: Rings\r\nRarity: Rare\r\nGale Coil\r\nGold Ring\r\n--------\r\n+20% to Fire Resistance & more"
	raw, ok := lookupURL(LookupCraftOfExile, "Gold Ring", text)
	require.True(t, ok)
	u, err := url.Parse(raw)
	require.NoError(t, err)
	assert.Equal(t, "beta.craftofexile.com", u.Host)
	q := parseQueryPairs(t, u.RawQuery)
	assert.Equal(t, [2]string{"game", "poe2"}, q[0])
	assert.Equal(t, [2]string{"eimport", strings.ReplaceAll(text, "\r\n", "\n")}, q[1], "round-trips exactly, with \\n line ends")

	got, _ := lookupURL(LookupCraftOfExile, "", "a b*~")
	assert.Equal(t, "https://beta.craftofexile.com/?game=poe2&eimport=a+b%2A~", got, "spaces as +")
	blank, _ := lookupURL(LookupCraftOfExile, "", "  ")
	assert.Equal(t, "https://beta.craftofexile.com/?game=poe2", blank, "blank text is not imported")
}

// parseQueryPairs decodes a form-urlencoded query in order.
func parseQueryPairs(t *testing.T, raw string) [][2]string {
	t.Helper()
	var out [][2]string
	for p := range strings.SplitSeq(raw, "&") {
		k, v, _ := strings.Cut(p, "=")
		k, err := url.QueryUnescape(k)
		require.NoError(t, err)
		v, err = url.QueryUnescape(v)
		require.NoError(t, err)
		out = append(out, [2]string{k, v})
	}
	return out
}

// bindingsTS returns web/src/bindings.ts, generated from the Go types by cmd/bindgen.
func bindingsTS(t *testing.T) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	b, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "web", "src", "bindings.ts"))
	require.NoError(t, err)
	// A Windows checkout may have CRLF line endings; the parsers below match "\n".
	return strings.ReplaceAll(string(b), "\r\n", "\n")
}

// tsUnion returns the string literals of `export type name = "a" | "b";`.
func tsUnion(t *testing.T, ts, name string) []string {
	t.Helper()
	m := regexp.MustCompile(`(?m)^export type ` + name + ` = ((?:"[^"]*"(?: \| )?)+);`).FindStringSubmatch(ts)
	require.NotNil(t, m, "no union %s in bindings.ts", name)
	var out []string
	for _, lit := range regexp.MustCompile(`"([^"]*)"`).FindAllStringSubmatch(m[1], -1) {
		out = append(out, lit[1])
	}
	return out
}

func enumStrings[T ~string](vs []T) []string {
	out := make([]string, len(vs))
	for i, v := range vs {
		out[i] = string(v)
	}
	return out
}

func TestLookupSiteMatchesBindings(t *testing.T) {
	assert.Equal(t, tsUnion(t, bindingsTS(t), "LookupSite"), enumStrings(lookupSites))
	for _, v := range lookupSites {
		b, err := json.Marshal(v)
		require.NoError(t, err)
		assert.JSONEq(t, `"`+string(v)+`"`, string(b))
		var back LookupSite
		require.NoError(t, json.Unmarshal(b, &back))
		assert.Equal(t, v, back)
	}
	for _, bad := range []string{`"Poe2db"`, `"craftOfExile"`, `""`, `1`} {
		var s LookupSite
		assert.Error(t, json.Unmarshal([]byte(bad), &s), bad)
	}
}
