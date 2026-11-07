package trade

import (
	"io"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/BorrowedMirror/rust-king-ledger/internal/core"
	"github.com/BorrowedMirror/rust-king-ledger/internal/testkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Thin wiremock-style layer over testkit.MockServer: matchers, expected hit counts (checked
// at cleanup, like wiremock's `.expect(n)` on drop) and recorded requests.

type recorded struct {
	Method string
	Path   string
	Header http.Header
	Body   []byte
}

type mockServer struct {
	*testkit.MockServer
	t        *testing.T
	mu       sync.Mutex
	requests []recorded
	mocks    []*mounted
}

type mounted struct {
	spec mockSpec
	resp testkit.Response
	hits *atomic.Int64
}

// newMock starts a server whose single catch-all testkit route dispatches to the mounted
// specs in order: the first whose method, path and matchers all accept the request answers
// it (wiremock semantics), otherwise 404.
func newMock(t *testing.T) *mockServer {
	t.Helper()
	m := &mockServer{MockServer: testkit.NewMockServer(), t: t}
	m.Mount(testkit.Route{Respond: m.dispatch})
	t.Cleanup(m.Close)
	return m
}

func (m *mockServer) dispatch(r *http.Request) testkit.Response {
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
	return testkit.Response{Status: http.StatusNotFound}
}

func (m *mockServer) baseURL() *url.URL {
	u, err := url.Parse(m.URI())
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

func (m *mockServer) mount(spec mockSpec, resp testkit.Response) *atomic.Int64 {
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
	wantV, err := decodeValue(want)
	require.NoError(t, err)
	return func(_ *http.Request, body []byte) bool {
		got, err := decodeValue(body)
		return err == nil && assert.ObjectsAreEqual(wantV, got)
	}
}

func respond(status int, body []byte) testkit.Response {
	return testkit.Response{Status: status, Header: http.Header{}, Body: body}
}

func respondJSON(t *testing.T, status int, v any) testkit.Response {
	t.Helper()
	b, err := core.MarshalSerde(v)
	require.NoError(t, err)
	return testkit.Response{Status: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: b}
}

// withHeaders adds the recorded header set headers/{name}.txt.
func withHeaders(t *testing.T, r testkit.Response, name string) testkit.Response {
	t.Helper()
	h, err := testkit.FixtureHeaders(name)
	require.NoError(t, err)
	if r.Header == nil {
		r.Header = http.Header{}
	}
	maps.Copy(r.Header, h)
	return r
}

type testkitResponse = testkit.Response

func matchers(ms ...func(*http.Request, []byte) bool) []func(*http.Request, []byte) bool { return ms }

func mustURL(t *testing.T, s string) *url.URL {
	t.Helper()
	u, err := url.Parse(s)
	require.NoError(t, err)
	return u
}

func tradeFixturesDir() string { return testkit.TradeFixturesDir() }
