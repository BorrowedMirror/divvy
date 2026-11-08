package trade

// Wire types of `/api/trade2/*`. Field names match the live JSON (see
// docs/research/trade2.md); only the fields RKL uses are decoded.

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
)

// League is one entry of `data/leagues`.
type League struct {
	ID    string  `json:"id"`
	Realm *string `json:"realm"`
	Text  *string `json:"text"`
}

// resultList is `{ "result": [...] }` (e.g. `GET /api/trade2/data/leagues`).
type resultList[T any] struct {
	Result []T `json:"result"`
}

// searchResponse is the `POST /api/trade2/search/poe2/{league}` response.
type searchResponse struct {
	ID         string   `json:"id"`
	Complexity *uint32  `json:"complexity"`
	Result     []string `json:"result"`
	Total      uint32   `json:"total"`
}

// fetchEntry is a `GET /api/trade2/fetch/{ids}?query={id}` response entry.
type fetchEntry struct {
	ID      string       `json:"id"`
	Listing fetchListing `json:"listing"`
	Item    fetchItem    `json:"item"`
}

// fetchItem is the part of a listed item's JSON a listing shows.
type fetchItem struct {
	ItemLevel *uint32 `json:"ilvl"`
	StackSize *uint32 `json:"stackSize"`
}

// fetchListing is the `listing` of a [fetchEntry].
type fetchListing struct {
	Indexed *string `json:"indexed"`
	// Ready-made whisper ("@Seller Hi, I would like to buy ...").
	Whisper *string       `json:"whisper"`
	Price   *fetchPrice   `json:"price"`
	Account *fetchAccount `json:"account"`
}

// fetchPrice is a listing's asking price.
type fetchPrice struct {
	Kind     string  `json:"type"`
	Amount   float64 `json:"amount"`
	Currency string  `json:"currency"`
}

// fetchAccount is the seller. Online is the raw `online` value: null or missing when
// offline, e.g. `{"league":"Standard"}` when online.
type fetchAccount struct {
	Name   string         `json:"name"`
	Online jsontext.Value `json:"online"`
}

func (a *fetchAccount) online() bool {
	return a != nil && len(a.Online) > 0 && a.Online.Kind() != 'n'
}

// exchangeResponse is the `POST /api/trade2/exchange/poe2/{league}` response.
type exchangeResponse struct {
	ID         string          `json:"id"`
	Complexity *uint32         `json:"complexity"`
	Result     exchangeEntries `json:"result"`
	Total      uint32          `json:"total"`
}

// exchangeEntries is the `result` of an exchange: an object keyed by listing id (server
// order = sort order), or `[]` when empty; both decode in document order.
type exchangeEntries []exchangeEntry

// UnmarshalJSONFrom decodes the object or array form.
func (e *exchangeEntries) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	out := exchangeEntries{}
	switch dec.PeekKind() {
	case '[':
		var list []exchangeEntry
		if err := json.UnmarshalDecode(dec, &list); err != nil {
			return err
		}
		out = append(out, list...)
	case '{':
		if _, err := dec.ReadToken(); err != nil {
			return err
		}
		for dec.PeekKind() != '}' {
			if _, err := dec.ReadToken(); err != nil { // the listing id
				return err
			}
			var entry exchangeEntry
			if err := json.UnmarshalDecode(dec, &entry); err != nil {
				return err
			}
			out = append(out, entry)
		}
		if _, err := dec.ReadToken(); err != nil {
			return err
		}
	default:
		return errors.New("expected a map or list of exchange entries")
	}
	*e = out
	return nil
}

// exchangeEntry is one bulk-exchange listing.
type exchangeEntry struct {
	ID      string          `json:"id"`
	Listing exchangeListing `json:"listing"`
}

// exchangeListing is the `listing` of an [exchangeEntry].
type exchangeListing struct {
	Indexed *string         `json:"indexed"`
	Account *fetchAccount   `json:"account"`
	Offers  []exchangeOffer `json:"offers"`
}

// exchangeOffer is one offer: the seller gives Item.Amount of Item.Currency for
// Exchange.Amount of Exchange.Currency.
type exchangeOffer struct {
	Exchange exchangeSide `json:"exchange"`
	Item     exchangeSide `json:"item"`
}

// exchangeSide is one side of an [exchangeOffer].
type exchangeSide struct {
	Currency string  `json:"currency"`
	Amount   float64 `json:"amount"`
	Stock    *uint32 `json:"stock"`
}

// apiError is the error body of a failed trade2 request.
type apiError struct {
	Error struct {
		Message *string `json:"message"`
	} `json:"error"`
}

// staticData is `data/static`: groups of exchange-able items.
type staticData struct {
	Result []struct {
		Entries []staticEntry `json:"entries"`
	} `json:"result"`
}

// staticEntry is one exchange-able item: its exchange tag, name and image path.
type staticEntry struct {
	ID    string `json:"id"`
	Text  string `json:"text"`
	Image string `json:"image"`
}
