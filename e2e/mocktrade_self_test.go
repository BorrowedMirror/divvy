//go:build e2e

package e2e

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMockTrade checks the mock itself (no app or display needed).
func TestMockTrade(t *testing.T) {
	m := newMockTrade()
	defer m.Close()

	do := func(method, path string, body io.Reader) *http.Response {
		req, err := http.NewRequestWithContext(t.Context(), method, m.URL()+path, body)
		require.NoError(t, err)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		r, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		return r
	}
	get := func(path string) (int, map[string]any) {
		r := do(http.MethodGet, path, nil)
		defer r.Body.Close()
		var v map[string]any
		_ = json.NewDecoder(r.Body).Decode(&v)
		return r.StatusCode, v
	}
	type searchReply struct {
		ID     string   `json:"id"`
		Result []string `json:"result"`
		Total  int      `json:"total"`
	}
	search := func() (int, http.Header, searchReply) {
		r := do(http.MethodPost, "/api/trade2/search/poe2/Standard", strings.NewReader("{}"))
		defer r.Body.Close()
		var v searchReply
		_ = json.NewDecoder(r.Body).Decode(&v)
		return r.StatusCode, r.Header, v
	}

	_, leagues := get("/api/trade2/data/leagues")
	assert.Contains(t, leagues["result"], map[string]any{"id": "Standard", "realm": "poe2", "text": "Standard"})

	status, header, s := search()
	assert.Equal(t, http.StatusOK, status)
	assert.Equal(t, "Ip", header.Get("X-Rate-Limit-Rules"))
	assert.Len(t, s.Result, 3)

	_, f := get("/api/trade2/fetch/" + strings.Join(s.Result, ",") + "?query=" + s.ID)
	var amounts []float64
	for _, e := range f["result"].([]any) {
		amounts = append(amounts, e.(map[string]any)["listing"].(map[string]any)["price"].(map[string]any)["amount"].(float64))
	}
	assert.Equal(t, []float64{1, 2, 3}, amounts)

	m.Configure(mockConfig{Listings: []mockListing{{Amount: 7, Currency: "exalted"}}, Total: 50})
	m.RateLimit(endpointSearch, 1, 4)
	status, header, _ = search()
	assert.Equal(t, http.StatusTooManyRequests, status)
	assert.Equal(t, "4", header.Get("Retry-After"))
	status, _, s = search()
	assert.Equal(t, http.StatusOK, status)
	assert.Equal(t, 50, s.Total)
	assert.Equal(t, []string{listingID(0)}, s.Result)

	searches := 0
	for _, req := range m.Requests() {
		if strings.HasPrefix(req.Path, "/api/trade2/search") {
			searches++
		}
	}
	assert.Equal(t, 3, searches)
	status, _ = get("/nope")
	assert.Equal(t, http.StatusNotFound, status)
	assert.True(t, m.Hit(http.MethodGet, "/nope"), "unknown routes are recorded too")

	m.Reset()
	_, _, s = search()
	assert.Len(t, s.Result, 3)
	assert.Len(t, m.Requests(), 1)
}
