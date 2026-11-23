package trade

// Bulk exchange (currency): one request returns listings with their offers.

import (
	"context"
	"encoding/json/v2"
	"net/http"

	"github.com/BorrowedMirror/divvy/internal/core"
)

// exchangeRequest is the bulk-exchange body, as the trade site sends it.
type exchangeRequest struct {
	Query  exchangeQuery `json:"query"`
	Sort   exchangeSort  `json:"sort"`
	Engine string        `json:"engine"`
}

type exchangeQuery struct {
	Status core.Option `json:"status"`
	Have   []string    `json:"have"`
	Want   []string    `json:"want"`
}

type exchangeSort struct {
	Have string `json:"have"`
}

// exchangeBody asks for want priced in exalted and divine (except the currency being priced
// itself). The exchange has no "securable" status, so anything but [core.StatusAny] is
// `online`.
func exchangeBody(want string, status core.OnlineStatus) exchangeRequest {
	have := []string{}
	for _, c := range []string{"exalted", "divine"} {
		if c != want {
			have = append(have, c)
		}
	}
	option := "online"
	if status == core.StatusAny {
		option = "any"
	}
	return exchangeRequest{
		Query:  exchangeQuery{Status: core.Option{Option: option}, Have: have, Want: []string{want}},
		Sort:   exchangeSort{Have: "asc"},
		Engine: "new",
	}
}

// exchange is `POST /api/trade2/exchange/poe2/{league}`.
func (c *Client) exchange(ctx context.Context, league string, body *exchangeRequest) (*exchangeResponse, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, decodeError("exchange body: %v", err)
	}
	return cached(ctx, c.exchanges, league+"\n"+string(b), func(ctx context.Context) (*exchangeResponse, error) {
		req, err := c.request(ctx, http.MethodPost, c.url("api", "trade2", "exchange", "poe2", league), b)
		if err != nil {
			return nil, err
		}
		raw, err := c.send(ctx, policyExchange, req)
		if err != nil {
			return nil, err
		}
		var resp exchangeResponse
		if err := decode(raw, &resp); err != nil {
			return nil, err
		}
		return &resp, nil
	})
}

// exchangeURL is `https://www.pathofexile.com/trade2/exchange/poe2/{league}/{searchID}`.
func (c *Client) exchangeURL(league, searchID string) string {
	return c.url("trade2", "exchange", "poe2", league, searchID).String()
}

// exchangeListings is a bulk-exchange lookup for an item sold by exchange tag want.
func (c *Client) exchangeListings(ctx context.Context, league, want string, status core.OnlineStatus) (*listingPage, error) {
	body := exchangeBody(want, status)
	resp, err := c.exchange(ctx, league, &body)
	if err != nil {
		return nil, err
	}
	listings := []core.Listing{}
	for i := range resp.Result {
		if len(listings) >= c.config.FetchCount {
			break
		}
		if l, ok := listingFromExchange(&resp.Result[i]); ok {
			listings = append(listings, l)
		}
	}
	return &listingPage{
		TradeURL: c.exchangeURL(league, resp.ID),
		SearchID: resp.ID,
		Total:    resp.Total,
		Listings: listings,
	}, nil
}

// listingFromExchange maps an exchange entry to a per-unit listing; StackSize is the
// seller's stock.
func listingFromExchange(e *exchangeEntry) (core.Listing, bool) {
	if len(e.Listing.Offers) == 0 {
		return core.Listing{}, false
	}
	offer := e.Listing.Offers[0]
	if offer.Item.Amount <= 0 {
		return core.Listing{}, false
	}
	return core.Listing{
		ID:        e.ID,
		Price:     core.Price{Amount: offer.Exchange.Amount / offer.Item.Amount, Currency: offer.Exchange.Currency},
		Seller:    sellerOf(e.Listing.Account),
		Online:    e.Listing.Account.online(),
		Indexed:   e.Listing.Indexed,
		StackSize: offer.Item.Stock,
	}, true
}
