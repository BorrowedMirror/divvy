package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
)

// LookupSite is a site an item can be looked up on from the panel. Sent to the frontend.
type LookupSite string

// LookupSite variants (serde snake_case).
const (
	LookupPoe2db       LookupSite = "poe2db"
	LookupWiki         LookupSite = "wiki"
	LookupCraftOfExile LookupSite = "craft_of_exile"
)

// LookupSiteValues lists every [LookupSite] in declaration order.
func LookupSiteValues() []LookupSite {
	return []LookupSite{LookupPoe2db, LookupWiki, LookupCraftOfExile}
}

// UnmarshalJSON accepts only the serde variant names.
func (s *LookupSite) UnmarshalJSON(b []byte) error {
	return unmarshalLogicEnum(b, s, "LookupSite", LookupSiteValues())
}

// unmarshalLogicEnum decodes a unit-variant string enum, rejecting unknown variants like serde.
func unmarshalLogicEnum[T ~string](b []byte, dst *T, name string, variants []T) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	for _, v := range variants {
		if string(v) == s {
			*dst = v
			return nil
		}
	}
	return fmt.Errorf("%s: unknown variant `%s`", name, s)
}

// LookupURL is the page for name (a unique's name or a base type) on site. The hosts are fixed
// here, so the web view can only open these three sites. Craft of Exile imports itemText (the
// game's copied item text, advanced Ctrl+Alt+C for mods) from its `eimport` query parameter,
// which its page submits to its Item Importer on load. ok is false when name is blank (except
// for Craft of Exile, which ignores it). The URL is serialised exactly as Rust's url crate did.
func LookupURL(site LookupSite, name, itemText string) (u string, ok bool) {
	if site == LookupCraftOfExile {
		u = "https://beta.craftofexile.com/?game=poe2"
		text := strings.ReplaceAll(itemText, "\r\n", "\n")
		if strings.TrimSpace(text) != "" {
			u += "&eimport=" + formURLEncode(text)
		}
		return u, true
	}
	page := strings.ReplaceAll(strings.TrimSpace(name), " ", "_")
	if page == "" {
		return "", false
	}
	base := "https://www.poe2wiki.net/wiki" // trailing empty segment popped
	if site == LookupPoe2db {
		base = "https://poe2db.tw/us"
	}
	return pushPathSegment(base, page), true
}

// pushPathSegment appends one segment to a special URL whose path has no trailing slash, like
// url::PathSegmentsMut::push: `.` and `..` are skipped, tabs and newlines dropped, the rest
// percent-encoded with the special-path-segment set, and a segment that is a dot segment only
// after dropping tabs/newlines resolved as one.
func pushPathSegment(base, segment string) string {
	if segment == "." || segment == ".." {
		return base
	}
	segment = strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' || r == '\r' {
			return -1
		}
		return r
	}, segment)
	switch segment {
	case ".":
		return base + "/"
	case "..":
		return base[:strings.LastIndexByte(base, '/')+1]
	}
	var b strings.Builder
	b.WriteString(base)
	b.WriteByte('/')
	for i := range len(segment) {
		c := segment[i]
		if c < 0x20 || c >= 0x7f || strings.IndexByte(" \"<>`#?{}/%\\", c) >= 0 {
			fmt.Fprintf(&b, "%%%02X", c)
		} else {
			b.WriteByte(c)
		}
	}
	return b.String()
}

// formURLEncode is application/x-www-form-urlencoded byte serialisation (Rust's
// form_urlencoded::byte_serialize): space as `+`, `*-._` and alphanumerics kept.
func formURLEncode(s string) string {
	var b strings.Builder
	for i := range len(s) {
		c := s[i]
		switch {
		case c == ' ':
			b.WriteByte('+')
		case c == '*' || c == '-' || c == '.' || c == '_' ||
			'0' <= c && c <= '9' || 'A' <= c && c <= 'Z' || 'a' <= c && c <= 'z':
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

// webHost is an http(s) URL's host as the WHATWG URL parser sees it.
type webHost struct {
	domain string     // lowercase, when not an IP address
	ip     netip.Addr // IPv4 (all WHATWG number forms) or IPv6
}

// webOrigin is an http(s) URL's tuple origin with the port defaulted.
type webOrigin struct {
	scheme string
	host   webHost
	port   uint16
}

// parseWebHost interprets a hostname (brackets removed) like the url crate does for special
// schemes: IPv6 in brackets, IPv4 in any WHATWG number form, else an ASCII-lowercased domain.
// Hosts the WHATWG parser would map through IDNA are compared as given, which can only reject
// more URLs, never accept more.
func parseWebHost(hostport, hostname string) (webHost, bool) {
	if hostname == "" {
		return webHost{}, false
	}
	if strings.HasPrefix(hostport, "[") {
		ip, err := netip.ParseAddr(hostname)
		if err != nil || !ip.Is6() || ip.Zone() != "" {
			return webHost{}, false
		}
		return webHost{ip: ip}, true
	}
	if ip, isIPv4, ok := parseWHATWGIPv4(hostname); isIPv4 {
		if !ok {
			return webHost{}, false
		}
		return webHost{ip: ip}, true
	}
	return webHost{domain: asciiLowerString(hostname)}, true
}

// parseWHATWGIPv4 implements the URL standard's "ends in a number" check and IPv4 parser.
// isIPv4 reports whether host must be an IPv4 address; ok whether it is a valid one.
func parseWHATWGIPv4(host string) (addr netip.Addr, isIPv4, ok bool) {
	parts := strings.Split(host, ".")
	if parts[len(parts)-1] == "" {
		if len(parts) == 1 {
			return netip.Addr{}, false, false
		}
		parts = parts[:len(parts)-1]
	}
	last := parts[len(parts)-1]
	allDigits := last != "" && strings.Trim(last, "0123456789") == ""
	if _, numOK := parseIPv4Number(last); !allDigits && !numOK {
		return netip.Addr{}, false, false
	}
	if len(parts) > 4 {
		return netip.Addr{}, true, false
	}
	nums := make([]uint64, len(parts))
	for i, p := range parts {
		n, numOK := parseIPv4Number(p)
		if !numOK {
			return netip.Addr{}, true, false
		}
		nums[i] = n
	}
	for _, n := range nums[:len(nums)-1] {
		if n > 255 {
			return netip.Addr{}, true, false
		}
	}
	if nums[len(nums)-1] >= 1<<(8*(5-len(nums))) {
		return netip.Addr{}, true, false
	}
	v := nums[len(nums)-1]
	for i, n := range nums[:len(nums)-1] {
		v += n << (8 * (3 - i))
	}
	return netip.AddrFrom4([4]byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)}), true, true
}

// parseIPv4Number parses one IPv4 part: `0x` hex, leading-zero octal or decimal.
func parseIPv4Number(s string) (uint64, bool) {
	if s == "" {
		return 0, false
	}
	base := 10
	switch {
	case len(s) >= 2 && (s[:2] == "0x" || s[:2] == "0X"):
		s, base = s[2:], 16
	case len(s) >= 2 && s[0] == '0':
		s, base = s[1:], 8
	}
	if s == "" {
		return 0, true
	}
	if s[0] == '+' || s[0] == '-' {
		return 0, false
	}
	n, err := strconv.ParseUint(s, base, 64)
	if err != nil {
		// Out-of-range numbers still make the host an (invalid) IPv4 address.
		if errors.Is(err, strconv.ErrRange) {
			return 1 << 40, true
		}
		return 0, false
	}
	return n, true
}

// originOf is the tuple origin of an http(s) URL; ok is false for other schemes and for URLs
// the WHATWG parser would reject.
func originOf(u *url.URL) (webOrigin, bool) {
	if u == nil || u.Opaque != "" {
		return webOrigin{}, false
	}
	scheme := asciiLowerString(u.Scheme)
	var port uint16
	switch scheme {
	case "http":
		port = 80
	case "https":
		port = 443
	default:
		return webOrigin{}, false
	}
	host, ok := parseWebHost(u.Host, u.Hostname())
	if !ok {
		return webOrigin{}, false
	}
	if p := u.Port(); p != "" {
		n, err := strconv.ParseUint(p, 10, 16)
		if err != nil {
			return webOrigin{}, false
		}
		port = uint16(n)
	}
	return webOrigin{scheme: scheme, host: host, port: port}, true
}

// defaultPort reports whether o's port is its scheme's default (the url crate's port() is None).
func (o webOrigin) defaultPort() bool {
	return o.scheme == "http" && o.port == 80 || o.scheme == "https" && o.port == 443
}

// IsSafeOverride reports whether an env URL override is acceptable: https anywhere, or http on
// loopback only.
func IsSafeOverride(u *url.URL) bool {
	o, ok := originOf(u)
	if !ok {
		return false
	}
	if o.scheme == "https" {
		return true
	}
	switch {
	case o.host.domain != "":
		return o.host.domain == "localhost"
	case o.host.ip.Is4():
		return o.host.ip.As4()[0] == 127
	default:
		return o.host.ip == netip.IPv6Loopback()
	}
}

// IsAllowedTradeURL reports whether rawURL may be opened as a trade search: only the official
// trade site's search pages, or (debug/test builds) a vetted override origin. The path is
// checked after resolving dot segments, as the url crate did.
func IsAllowedTradeURL(rawURL string, overrideBase *url.URL) bool {
	cleaned, path, ok := splitSpecialURL(rawURL)
	if !ok {
		return false
	}
	parsed, err := url.Parse(cleaned)
	if err != nil {
		return false
	}
	o, ok := originOf(parsed)
	if !ok {
		return false
	}
	path = removeDotSegments(path)
	tradePath := strings.HasPrefix(path, "/trade2/") || strings.HasPrefix(path, "/trade/")
	switch o.host.domain {
	case "www.pathofexile.com", "pathofexile.com":
		return o.scheme == "https" && o.defaultPort() && tradePath
	}
	if overrideBase == nil || !IsSafeOverride(overrideBase) {
		return false
	}
	base, ok := originOf(overrideBase)
	return ok && base == o && tradePath
}

// splitSpecialURL applies the WHATWG preprocessing to an http(s) URL string (trim C0 controls
// and spaces, drop tabs/newlines, backslashes before the query as slashes) and returns it with
// its raw path. ok is false for other schemes or URLs without an authority.
func splitSpecialURL(raw string) (cleaned, path string, ok bool) {
	raw = strings.TrimFunc(raw, func(r rune) bool { return r <= 0x20 })
	raw = strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' || r == '\r' {
			return -1
		}
		return r
	}, raw)
	scheme, rest, found := strings.Cut(raw, ":")
	if !found {
		return "", "", false
	}
	if s := asciiLowerString(scheme); s != "http" && s != "https" {
		return "", "", false
	}
	end := strings.IndexAny(rest, "?#")
	if end < 0 {
		end = len(rest)
	}
	rest = strings.ReplaceAll(rest[:end], `\`, "/") + rest[end:]
	if !strings.HasPrefix(rest, "//") {
		return "", "", false
	}
	authEnd := strings.IndexAny(rest[2:], "/?#")
	if authEnd < 0 {
		return scheme + ":" + rest, "/", true
	}
	pathAndMore := rest[2+authEnd:]
	if i := strings.IndexAny(pathAndMore, "?#"); i >= 0 {
		path = pathAndMore[:i]
	} else {
		path = pathAndMore
	}
	if path == "" {
		path = "/"
	}
	return scheme + ":" + rest, path, true
}

// removeDotSegments resolves `.` and `..` segments (also percent-encoded as `%2e`) in an
// absolute path, as the WHATWG path parser does.
func removeDotSegments(path string) string {
	segs := strings.Split(strings.TrimPrefix(path, "/"), "/")
	out := make([]string, 0, len(segs))
	for i, s := range segs {
		lastSeg := i == len(segs)-1
		switch asciiLowerString(s) {
		case "..", ".%2e", "%2e.", "%2e%2e":
			if len(out) > 0 {
				out = out[:len(out)-1]
			}
			if lastSeg {
				out = append(out, "")
			}
		case ".", "%2e":
			if lastSeg {
				out = append(out, "")
			}
		default:
			out = append(out, s)
		}
	}
	return "/" + strings.Join(out, "/")
}
