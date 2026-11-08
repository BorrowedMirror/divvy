package trade

// HTTP and caching shared by the price sources.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/maypok86/otter/v2"
)

// requestTimeout bounds every request, connection to last body byte.
const requestTimeout = 20 * time.Second

// maxBodySize is the largest response body read (the biggest, `data/stats`, is a few MiB).
const maxBodySize = 32 << 20

// newHTTPClient is a client with its own transport (HTTP/2, gzip and proxy from the
// environment).
func newHTTPClient() *http.Client {
	tr, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return &http.Client{Timeout: requestTimeout}
	}
	tr = tr.Clone()
	tr.ForceAttemptHTTP2 = true
	return &http.Client{Transport: tr, Timeout: requestTimeout}
}

// checkBaseURL rejects a base URL requests cannot be built on (no http(s) scheme or host).
func checkBaseURL(u *url.URL) error {
	if u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return configError("base URL %q is not an http(s) URL", fmt.Sprint(u))
	}
	return nil
}

// joinPath appends path segments to base, escaping each (a `/` inside a segment stays part
// of it). Commas stay literal: RFC 3986 allows them in a segment and `fetch` takes a
// comma-separated id list.
func joinPath(base *url.URL, segments ...string) *url.URL {
	escaped := make([]string, len(segments))
	for i, s := range segments {
		escaped[i] = strings.ReplaceAll(url.PathEscape(s), "%2C", ",")
	}
	return base.JoinPath(escaped...)
}

// transportErr maps a send/read error: the caller's own cancellation is returned as
// ctx.Err(), everything else is a transport error.
func transportErr(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil && errors.Is(err, ctxErr) {
		return ctxErr
	}
	return transportError(err)
}

// readBody reads at most maxBodySize bytes of resp's body.
func readBody(ctx context.Context, resp *http.Response) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodySize))
	if err != nil {
		return nil, transportErr(ctx, err)
	}
	return body, nil
}

// get performs a GET with the User-Agent and returns the body of a successful response.
func get(ctx context.Context, client *http.Client, u *url.URL, userAgent string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, transportError(err)
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := client.Do(req)
	if err != nil {
		return nil, transportErr(ctx, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := readBody(ctx, resp)
	if err != nil {
		return nil, err
	}
	if !isSuccess(resp.StatusCode) {
		return nil, httpError(resp.StatusCode, bodyPrefix(body))
	}
	return body, nil
}

// bodyPrefix is the first 200 characters of body, for error messages.
func bodyPrefix(body []byte) string {
	s := []rune(string(body))
	return string(s[:min(len(s), 200)])
}

func isSuccess(status int) bool { return status >= 200 && status < 300 }

// newCache is a cache of up to capacity entries that expire ttl after they were written.
// A zero ttl disables caching (nil cache).
func newCache[K comparable, V any](capacity int, ttl time.Duration) *otter.Cache[K, V] {
	if ttl <= 0 {
		return nil
	}
	return otter.Must(&otter.Options[K, V]{
		MaximumSize:      capacity,
		ExpiryCalculator: otter.ExpiryWriting[K, V](ttl),
	})
}

// cached returns key's value from c, loading it with load on a miss. Concurrent misses for
// one key share a single load. A nil cache always loads.
func cached[K comparable, V any](ctx context.Context, c *otter.Cache[K, V], key K, load func(context.Context) (V, error)) (V, error) {
	if c == nil {
		return load(ctx)
	}
	return c.Get(ctx, key, otter.LoaderFunc[K, V](func(ctx context.Context, _ K) (V, error) { return load(ctx) }))
}
