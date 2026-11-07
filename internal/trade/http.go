package trade

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/maypok86/otter/v2"
)

// newHTTPClient is a client with its own transport (HTTP/2, gzip and proxy-from-env like
// reqwest's defaults).
func newHTTPClient(timeout time.Duration) *http.Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.ForceAttemptHTTP2 = true
	return &http.Client{Transport: tr, Timeout: timeout}
}

// cannotBeABase is url's `Url::cannot_be_a_base` (e.g. `mailto:a@b.c`, `data:text/plain,x`).
func cannotBeABase(u *url.URL) bool { return u == nil || u.Scheme == "" || u.Opaque != "" }

// encodeSegment percent-encodes a path segment with the url crate's PATH_SEGMENT set
// (controls, space, `"#<>?\`{}/%` and non-ASCII), so `,` and `:` stay literal.
func encodeSegment(s string) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 0x20 || c >= 0x7f || strings.IndexByte(" \"#<>?`{}/%", c) >= 0 {
			b.WriteByte('%')
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&15])
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

// formEncode is application/x-www-form-urlencoded byte serialisation (url's
// form_urlencoded: alphanumerics and `*-._` literal, space as `+`).
func formEncode(s string) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', strings.IndexByte("*-._", c) >= 0:
			b.WriteByte(c)
		case c == ' ':
			b.WriteByte('+')
		default:
			b.WriteByte('%')
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&15])
		}
	}
	return b.String()
}

// appendSegments is `path_segments_mut().pop_if_empty().extend(segments)`.
func appendSegments(base *url.URL, segments ...string) *url.URL {
	u := *base
	var p strings.Builder
	p.WriteString(strings.TrimSuffix(base.EscapedPath(), "/"))
	for _, s := range segments {
		p.WriteString("/" + encodeSegment(s))
	}
	u.RawPath = p.String()
	u.Path, _ = url.PathUnescape(p.String())
	return &u
}

// appendQuery is `query_pairs_mut().append_pair(k, v)...`.
func appendQuery(u *url.URL, pairs ...[2]string) {
	q := u.RawQuery
	for _, kv := range pairs {
		if q != "" {
			q += "&"
		}
		q += formEncode(kv[0]) + "=" + formEncode(kv[1])
	}
	u.RawQuery = q
}

// lossy is String::from_utf8_lossy.
func lossy(b []byte) string { return strings.ToValidUTF8(string(b), "�") }

// firstChars is `s.chars().take(n).collect()`.
func firstChars(s string, n int) string {
	i := 0
	for pos := range s {
		if i == n {
			return s[:pos]
		}
		i++
	}
	return s
}

// transportErr maps a send/read error: the caller's own cancellation is returned as
// ctx.Err() (Rust callers dropped the future instead), everything else is a transport error.
func transportErr(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil && errors.Is(err, ctxErr) {
		return ctxErr
	}
	return TransportError(err)
}

// getBody performs req and reads the body.
func getBody(ctx context.Context, client *http.Client, req *http.Request) (*http.Response, []byte, error) {
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, transportErr(ctx, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, transportErr(ctx, err)
	}
	return resp, body, nil
}

func isSuccess(status int) bool { return status >= 200 && status < 300 }

// newCache is moka's `Cache::builder().max_capacity(n).time_to_live(ttl)`: entries expire
// ttl after they were written. A zero ttl disables caching (nil cache).
func newCache[K comparable, V any](capacity int, ttl time.Duration) *otter.Cache[K, V] {
	if ttl <= 0 {
		return nil
	}
	return otter.Must(&otter.Options[K, V]{
		MaximumSize:      capacity,
		ExpiryCalculator: otter.ExpiryWriting[K, V](ttl),
	})
}

func cacheGet[K comparable, V any](c *otter.Cache[K, V], key K) (V, bool) {
	if c == nil {
		var zero V
		return zero, false
	}
	return c.GetIfPresent(key)
}

func cacheSet[K comparable, V any](c *otter.Cache[K, V], key K, v V) {
	if c != nil {
		c.Set(key, v)
	}
}
