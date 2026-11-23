package trade

// Item search: `search` finds listing ids, `fetch` loads them.

import (
	"context"
	"encoding/json/v2"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"golang.org/x/sync/errgroup"

	"github.com/BorrowedMirror/divvy/internal/core"
)

// fetchChunk is the max ids per `fetch` call (server limit).
const fetchChunk = 10

// search is `POST /api/trade2/search/poe2/{league}`.
func (c *Client) search(ctx context.Context, league string, query *core.TradeQuery) (*searchResponse, error) {
	body, err := json.Marshal(query, json.Deterministic(true))
	if err != nil {
		return nil, decodeError("search query: %v", err)
	}
	return cached(ctx, c.searches, league+"\n"+string(body), func(ctx context.Context) (*searchResponse, error) {
		req, err := c.request(ctx, http.MethodPost, c.url("api", "trade2", "search", "poe2", league), body)
		if err != nil {
			return nil, err
		}
		raw, err := c.send(ctx, policySearch, req)
		if err != nil {
			return nil, err
		}
		var resp searchResponse
		if err := decode(raw, &resp); err != nil {
			return nil, err
		}
		return &resp, nil
	})
}

// fetch is `GET /api/trade2/fetch/{id,id,...}?query={searchID}`, in chunks of [fetchChunk].
// Listings that vanished between search and fetch (`null`) are skipped.
//
// Chunks are requested concurrently (each still takes its own rate-limiter slot), so the
// default 20 listings cost one round trip instead of two; results keep id order. The first
// failing chunk cancels the others.
func (c *Client) fetch(ctx context.Context, ids []string, searchID string) ([]fetchEntry, error) {
	var chunks [][]string
	for chunk := range slices.Chunk(ids, fetchChunk) {
		chunks = append(chunks, chunk)
	}
	results := make([][]fetchEntry, len(chunks))
	g, gctx := errgroup.WithContext(ctx)
	for i, chunk := range chunks {
		g.Go(func() error {
			r, err := c.fetchChunk(gctx, chunk, searchID)
			results[i] = r
			return err
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	out := make([]fetchEntry, 0, len(ids))
	for _, r := range results {
		out = append(out, r...)
	}
	return out, nil
}

func (c *Client) fetchChunk(ctx context.Context, chunk []string, searchID string) ([]fetchEntry, error) {
	joined := strings.Join(chunk, ",")
	return cached(ctx, c.fetches, searchID+"\n"+joined, func(ctx context.Context) ([]fetchEntry, error) {
		u := c.url("api", "trade2", "fetch", joined)
		u.RawQuery = url.Values{"query": {searchID}, "realm": {"poe2"}}.Encode()
		req, err := c.request(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		raw, err := c.send(ctx, policyFetch, req)
		if err != nil {
			return nil, err
		}
		var list resultList[*fetchEntry]
		if err := decode(raw, &list); err != nil {
			return nil, err
		}
		entries := make([]fetchEntry, 0, len(list.Result))
		for _, e := range list.Result {
			if e != nil {
				entries = append(entries, *e)
			}
		}
		return entries, nil
	})
}

// tradeURL is `https://www.pathofexile.com/trade2/search/poe2/{league}/{searchID}`.
func (c *Client) tradeURL(league, searchID string) string {
	return c.url("trade2", "search", "poe2", league, searchID).String()
}

// searchListings searches, then fetches the first FetchCount results and maps them to
// listings.
func (c *Client) searchListings(ctx context.Context, league string, query *core.TradeQuery) (*listingPage, error) {
	search, err := c.search(ctx, league, query)
	if err != nil {
		return nil, err
	}
	ids := search.Result[:min(len(search.Result), max(c.config.FetchCount, 0))]
	var entries []fetchEntry
	if len(ids) > 0 {
		if entries, err = c.fetch(ctx, ids, search.ID); err != nil {
			return nil, err
		}
	}
	listings := []core.Listing{}
	for i := range entries {
		if l, ok := listingFromFetch(&entries[i]); ok {
			listings = append(listings, l)
		}
	}
	return &listingPage{
		TradeURL: c.tradeURL(league, search.ID),
		SearchID: search.ID,
		Total:    search.Total,
		Listings: listings,
	}, nil
}

// listingFromFetch maps a fetch entry to a core listing; entries without a price are
// dropped.
func listingFromFetch(e *fetchEntry) (core.Listing, bool) {
	price := e.Listing.Price
	if price == nil {
		return core.Listing{}, false
	}
	l := core.Listing{
		ID:        e.ID,
		Price:     core.Price{Amount: price.Amount, Currency: price.Currency},
		Seller:    sellerOf(e.Listing.Account),
		Online:    e.Listing.Account.online(),
		Indexed:   e.Listing.Indexed,
		ItemLevel: e.Item.ItemLevel,
		StackSize: e.Item.StackSize,
	}
	if w := e.Listing.Whisper; w != nil && strings.TrimSpace(*w) != "" {
		l.Whisper = w
	}
	return l, true
}
