package app

import (
	"cmp"
	"log/slog"
	"net/netip"
	"net/url"
	"os"
	"path"
	"strings"
)

// envOverride is an endpoint override from the environment variable key (e2e tests, mock
// servers). Only test-hook builds honour it, and only for https or loopback http URLs, so a
// release build can't be pointed at another host.
func envOverride(key string) (*url.URL, bool) {
	if !testHookBuild {
		return nil, false
	}
	raw := os.Getenv(key)
	if raw == "" {
		return nil, false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, false
	}
	if !isSafeOverride(u) {
		slog.Warn("ignoring endpoint override: only https or loopback http URLs are allowed", "var", key)
		return nil, false
	}
	return u, true
}

// webOrigin is an http(s) URL's origin: lowercase scheme and host, port defaulted.
type webOrigin struct {
	scheme, host, port string
}

// originOf is the origin of an absolute http(s) URL with a plain ASCII host. ok is false for
// other schemes, user info, non-ASCII (IDN) hosts and IPv6 zones.
func originOf(u *url.URL) (webOrigin, bool) {
	if u == nil || u.Opaque != "" || u.User != nil {
		return webOrigin{}, false
	}
	o := webOrigin{scheme: strings.ToLower(u.Scheme), host: u.Hostname(), port: u.Port()}
	switch o.scheme {
	case "http":
		o.port = cmp.Or(o.port, "80")
	case "https":
		o.port = cmp.Or(o.port, "443")
	default:
		return webOrigin{}, false
	}
	if o.host == "" || strings.ContainsFunc(o.host, func(r rune) bool { return r >= 0x80 }) || strings.Contains(o.host, "%") {
		return webOrigin{}, false
	}
	o.host = strings.ToLower(o.host)
	return o, true
}

// isSafeOverride reports whether an env URL override is acceptable: https anywhere, or http on
// loopback only (`localhost`, 127.0.0.0/8 or ::1, spelled canonically).
func isSafeOverride(u *url.URL) bool {
	o, ok := originOf(u)
	if !ok {
		return false
	}
	if o.scheme == "https" {
		return true
	}
	if o.host == "localhost" {
		return true
	}
	ip, err := netip.ParseAddr(o.host)
	return err == nil && !ip.Is4In6() && ip.IsLoopback()
}

// tradeURL returns rawURL as it may be opened as a trade search: only the official trade
// site's search pages over https, or (test-hook builds) the vetted override origin. The URL is
// first cleaned the way a browser reads it (surrounding spaces and controls trimmed, tabs and
// newlines dropped, backslashes before the query read as slashes), and its path is checked
// after resolving dot segments. Percent-encoded dots, slashes and backslashes in the path are
// refused outright. The result is the canonical form of the checked URL.
func tradeURL(rawURL string, overrideBase *url.URL) (string, bool) {
	u, err := url.Parse(browserClean(rawURL))
	if err != nil {
		return "", false
	}
	o, ok := originOf(u)
	if !ok || !tradePath(u) {
		return "", false
	}
	switch o.host {
	case "www.pathofexile.com", "pathofexile.com":
		if o.scheme != "https" || o.port != "443" {
			return "", false
		}
	default:
		base, ok := originOf(overrideBase)
		if !ok || !isSafeOverride(overrideBase) || base != o {
			return "", false
		}
	}
	return u.String(), true
}

// browserClean applies the URL standard's preprocessing for http(s) URLs: leading and trailing
// C0 controls and spaces are trimmed, tabs and newlines removed, and backslashes before the
// query or fragment read as slashes.
func browserClean(raw string) string {
	raw = strings.TrimFunc(raw, func(r rune) bool { return r <= 0x20 })
	raw = strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' || r == '\r' {
			return -1
		}
		return r
	}, raw)
	end := strings.IndexAny(raw, "?#")
	if end < 0 {
		end = len(raw)
	}
	return strings.ReplaceAll(raw[:end], `\`, "/") + raw[end:]
}

// tradePath reports whether u's path, with dot segments resolved, is under `/trade2/` or
// `/trade/`.
func tradePath(u *url.URL) bool {
	escaped := strings.ToLower(u.EscapedPath())
	for _, enc := range []string{"%2e", "%2f", "%5c"} {
		if strings.Contains(escaped, enc) {
			return false
		}
	}
	p := path.Clean("/" + u.Path)
	// A path ending in a slash or a dot segment names the directory, as in a browser.
	if p != "/" && (strings.HasSuffix(u.Path, "/") || strings.HasSuffix(u.Path, "/.") || strings.HasSuffix(u.Path, "/..")) {
		p += "/"
	}
	return strings.HasPrefix(p, "/trade2/") || strings.HasPrefix(p, "/trade/")
}
