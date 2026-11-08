package trade

import (
	"cmp"
	"encoding/json/v2"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A mock trade server: mounted responses picked by matchers, expected hit counts (checked at
// cleanup) and recorded requests.

// response is what a mounted mock answers.
type response struct {
	Status int
	Header http.Header
	Body   []byte
}

type recorded struct {
	Method string
	Path   string
	Header http.Header
	Body   []byte
}

type mockServer struct {
	server   *httptest.Server
	t        *testing.T
	mu       sync.Mutex
	requests []recorded
	mocks    []*mounted
}

type mounted struct {
	spec mockSpec
	resp response
	hits *atomic.Int64
}

// newMock starts a server answering from the mounted specs in order: the first whose method,
// path and matchers all accept the request answers it, otherwise 404.
func newMock(t *testing.T) *mockServer {
	t.Helper()
	m := &mockServer{t: t}
	m.server = httptest.NewServer(m)
	t.Cleanup(m.server.Close)
	return m
}

// URL is the server's base URL, e.g. `http://127.0.0.1:12345`.
func (m *mockServer) URL() string { return m.server.URL }

// ServeHTTP answers r from the mounted specs.
func (m *mockServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	resp := m.dispatch(r)
	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(cmp.Or(resp.Status, http.StatusOK))
	_, _ = w.Write(resp.Body)
}

func (m *mockServer) dispatch(r *http.Request) response {
	body, _ := io.ReadAll(r.Body)
	m.mu.Lock()
	m.requests = append(m.requests, recorded{r.Method, r.URL.EscapedPath(), r.Header.Clone(), body})
	mocks := slices.Clone(m.mocks)
	m.mu.Unlock()
	for _, mk := range mocks {
		if mk.spec.Method != "" && mk.spec.Method != r.Method {
			continue
		}
		if mk.spec.Path != "" && mk.spec.Path != r.URL.EscapedPath() {
			continue
		}
		ok := true
		for _, match := range mk.spec.Match {
			if !match(r, body) {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		mk.hits.Add(1)
		if mk.spec.Delay > 0 {
			time.Sleep(mk.spec.Delay)
		}
		return mk.resp
	}
	return response{Status: http.StatusNotFound}
}

func (m *mockServer) baseURL() *url.URL {
	u, err := url.Parse(m.URL())
	require.NoError(m.t, err)
	return u
}

// mockSpec is one mocked endpoint: Method/Path select it, Match adds further conditions
// (a request that fails them falls through to the next mock), Expect (when >= 0) is the exact number of
// matched requests at cleanup.
type mockSpec struct {
	Method string
	Path   string
	Match  []func(*http.Request, []byte) bool
	Delay  time.Duration
	Expect int
}

func (m *mockServer) mount(spec mockSpec, resp response) *atomic.Int64 {
	hits := &atomic.Int64{}
	m.mu.Lock()
	m.mocks = append(m.mocks, &mounted{spec, resp, hits})
	m.mu.Unlock()
	if spec.Expect >= 0 {
		t := m.t
		t.Cleanup(func() {
			assert.Equal(t, int64(spec.Expect), hits.Load(), "mock %s %s: wrong number of requests", spec.Method, spec.Path)
		})
	}
	return hits
}

func (m *mockServer) received() []recorded {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]recorded(nil), m.requests...)
}

// anyHits disables the hit-count check.
const anyHits = -1

func header(name, value string) func(*http.Request, []byte) bool {
	return func(r *http.Request, _ []byte) bool { return r.Header.Get(name) == value }
}

func queryParam(name, value string) func(*http.Request, []byte) bool {
	return func(r *http.Request, _ []byte) bool { return r.URL.Query().Get(name) == value }
}

// bodyJSON matches a body semantically equal to want's JSON.
func bodyJSON(t *testing.T, want []byte) func(*http.Request, []byte) bool {
	t.Helper()
	var wantV any
	require.NoError(t, json.Unmarshal(want, &wantV))
	return func(_ *http.Request, body []byte) bool {
		var got any
		return json.Unmarshal(body, &got) == nil && assert.ObjectsAreEqual(wantV, got)
	}
}

func respond(status int, body []byte) response {
	return response{Status: status, Header: http.Header{}, Body: body}
}

func respondJSON(t *testing.T, status int, v any) response {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return response{Status: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: b}
}

// withHeaders adds the recorded header set testdata/fixtures/headers/<name>.txt (`Name:
// value` per line).
func withHeaders(t *testing.T, r response, name string) response {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(fixturesDir, "headers", name+".txt"))
	require.NoError(t, err)
	h := http.Header{}
	for line := range strings.Lines(string(b)) {
		if strings.TrimSpace(line) == "" {
			continue
		}
		k, v, ok := strings.Cut(line, ":")
		require.True(t, ok, "header fixture %s: bad line %q", name, line)
		h.Set(strings.TrimSpace(k), strings.TrimSpace(v))
	}
	if r.Header == nil {
		r.Header = http.Header{}
	}
	maps.Copy(r.Header, h)
	return r
}

// inProcess serves requests with h without a network, so a client using it can run in a
// testing/synctest bubble.
type inProcess struct{ h http.Handler }

func (p inProcess) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Body == nil {
		r = r.Clone(r.Context())
		r.Body = http.NoBody
	}
	rec := httptest.NewRecorder()
	p.h.ServeHTTP(rec, r)
	return rec.Result(), nil
}

func matchers(ms ...func(*http.Request, []byte) bool) []func(*http.Request, []byte) bool { return ms }

func mustURL(t *testing.T, s string) *url.URL {
	t.Helper()
	u, err := url.Parse(s)
	require.NoError(t, err)
	return u
}

// fixturesDir holds the recorded trade2, poe.ninja and poe2scout responses.
const fixturesDir = "testdata/fixtures"

func fixtureBytes(tb testing.TB, name string) []byte {
	tb.Helper()
	b, err := os.ReadFile(filepath.Join(fixturesDir, name))
	require.NoError(tb, err)
	return b
}

// decodeJSON decodes a JSON document into generic values.
func decodeJSON(tb testing.TB, b []byte) any {
	tb.Helper()
	var v any
	require.NoError(tb, json.Unmarshal(b, &v))
	return v
}
