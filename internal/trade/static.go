package trade

// Data endpoints (`/api/trade2/data/*`): leagues and the exchange-able static items.

import (
	"context"
	"iter"
	"net/http"
	"strings"
)

// imageCDN is the base for the relative image paths in `data/static`.
const imageCDN = "https://web.poecdn.com"

// dataBody returns the raw `data/{which}` body, cached.
func (c *Client) dataBody(ctx context.Context, which string) ([]byte, error) {
	return cached(ctx, c.data, which, func(ctx context.Context) ([]byte, error) {
		req, err := c.request(ctx, http.MethodGet, c.url("api", "trade2", "data", which), nil)
		if err != nil {
			return nil, err
		}
		return c.send(ctx, policyData, req)
	})
}

// Leagues is `GET /api/trade2/data/leagues`.
func (c *Client) Leagues(ctx context.Context) ([]League, error) {
	body, err := c.dataBody(ctx, "leagues")
	if err != nil {
		return nil, err
	}
	var list resultList[League]
	if err := decode(body, &list); err != nil {
		return nil, err
	}
	if list.Result == nil {
		return nil, decodeError("leagues: no result")
	}
	return list.Result, nil
}

// staticEntries returns the entries of `data/static`.
func (c *Client) staticEntries(ctx context.Context) (iter.Seq[staticEntry], error) {
	body, err := c.dataBody(ctx, "static")
	if err != nil {
		return nil, err
	}
	var data staticData
	if err := decode(body, &data); err != nil {
		return nil, err
	}
	return data.entries(), nil
}

// entries iterates the entries of every group.
func (d *staticData) entries() iter.Seq[staticEntry] {
	return func(yield func(staticEntry) bool) {
		for _, g := range d.Result {
			for _, e := range g.Entries {
				if !yield(e) {
					return
				}
			}
		}
	}
}

// staticID is the exchange tag (`divine`, `exalted`, ...) for an item name, from
// `data/static`.
func (c *Client) staticID(ctx context.Context, text string) (string, bool, error) {
	entries, err := c.staticEntries(ctx)
	if err != nil {
		return "", false, err
	}
	for e := range entries {
		if e.Text == text {
			return e.ID, true, nil
		}
	}
	return "", false, nil
}

// CurrencyIcon is one currency (exchange tag) and its icon.
type CurrencyIcon struct {
	// ID is the exchange tag, as used in listing prices (`divine`, `exalted`, `chaos`, ...).
	ID string `json:"id"`
	// Name is the English item name (`Divine Orb`).
	Name string `json:"name"`
	// Icon is the absolute image URL.
	Icon string `json:"icon"`
}

// CurrencyIcons lists currency icons from `data/static`: every exchange tag that has an
// image, with its name and the absolute image URL on GGG's CDN (the URLs the official trade
// site uses; the images are not bundled).
func (c *Client) CurrencyIcons(ctx context.Context) ([]CurrencyIcon, error) {
	entries, err := c.staticEntries(ctx)
	if err != nil {
		return nil, err
	}
	return currencyIcons(entries), nil
}

// currencyIcons extracts icons from static entries. Entries without an image are skipped;
// the first entry wins for a repeated id.
func currencyIcons(entries iter.Seq[staticEntry]) []CurrencyIcon {
	out := []CurrencyIcon{}
	seen := map[string]bool{}
	for e := range entries {
		if e.ID == "" || e.Image == "" || seen[e.ID] {
			continue
		}
		icon := e.Image
		if !strings.HasPrefix(icon, "https://") {
			icon = imageCDN + icon
		}
		name := e.Text
		if name == "" {
			name = e.ID
		}
		seen[e.ID] = true
		out = append(out, CurrencyIcon{ID: e.ID, Name: name, Icon: icon})
	}
	return out
}
