package trade

// Wire types for `/api/trade2/*`. Field names match the live JSON (see
// docs/research/trade2.md). Encoding matches serde (field order, Option -> null); decoding
// matches serde's derived Deserialize (see serde.go).

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/BorrowedMirror/rust-king-ledger/internal/core"
)

// League is one entry of `data/leagues`.
type League struct {
	ID    string  `json:"id"`
	Realm *string `json:"realm"`
	Text  *string `json:"text"`
}

// MarshalJSON writes serde-compatible JSON.
func (l League) MarshalJSON() ([]byte, error) {
	type plain League
	return core.MarshalSerde(plain(l))
}

// UnmarshalJSON decodes like serde.
func (l *League) UnmarshalJSON(data []byte) error {
	o, err := decodeObject(data, "struct League")
	if err != nil {
		return err
	}
	var v League
	if err := o.required("id", &v.ID); err != nil {
		return err
	}
	if err := o.optional("realm", &v.Realm); err != nil {
		return err
	}
	if err := o.optional("text", &v.Text); err != nil {
		return err
	}
	*l = v
	return nil
}

// ResultList is `{ "result": [...] }` (e.g. `GET /api/trade2/data/leagues`).
type ResultList[T any] struct {
	Result []T `json:"result"`
}

// MarshalJSON writes serde-compatible JSON.
func (r ResultList[T]) MarshalJSON() ([]byte, error) {
	return core.MarshalSerde(struct {
		Result []T `json:"result"`
	}{r.Result})
}

// UnmarshalJSON decodes like serde.
func (r *ResultList[T]) UnmarshalJSON(data []byte) error {
	o, err := decodeObject(data, "struct ResultList")
	if err != nil {
		return err
	}
	var v ResultList[T]
	if err := o.required("result", &v.Result); err != nil {
		return err
	}
	*r = v
	return nil
}

// SearchResponse is the `POST /api/trade2/search/poe2/{league}` response.
type SearchResponse struct {
	ID         string   `json:"id"`
	Complexity *uint32  `json:"complexity"`
	Result     []string `json:"result"`
	Total      uint32   `json:"total"`
}

// MarshalJSON writes serde-compatible JSON.
func (s SearchResponse) MarshalJSON() ([]byte, error) {
	type plain SearchResponse
	return core.MarshalSerde(plain(s))
}

// UnmarshalJSON decodes like serde.
func (s *SearchResponse) UnmarshalJSON(data []byte) error {
	o, err := decodeObject(data, "struct SearchResponse")
	if err != nil {
		return err
	}
	var v SearchResponse
	if err := o.required("id", &v.ID); err != nil {
		return err
	}
	if err := o.optional("complexity", &v.Complexity); err != nil {
		return err
	}
	if err := o.required("result", &v.Result); err != nil {
		return err
	}
	if err := o.required("total", &v.Total); err != nil {
		return err
	}
	*s = v
	return nil
}

func (s *SearchResponse) clone() *SearchResponse {
	c := *s
	c.Result = append([]string(nil), s.Result...)
	return &c
}

// FetchEntry is a `GET /api/trade2/fetch/{ids}?query={id}` response entry. Only fields we
// use are typed; Item is the raw item JSON (a serde_json::Value: nil when absent).
type FetchEntry struct {
	ID      string       `json:"id"`
	Listing FetchListing `json:"listing"`
	Item    any          `json:"item"`
}

// MarshalJSON writes serde-compatible JSON.
func (e FetchEntry) MarshalJSON() ([]byte, error) {
	type plain FetchEntry
	return core.MarshalSerde(plain(e))
}

// UnmarshalJSON decodes like serde.
func (e *FetchEntry) UnmarshalJSON(data []byte) error {
	o, err := decodeObject(data, "struct FetchEntry")
	if err != nil {
		return err
	}
	var v FetchEntry
	if err := o.required("id", &v.ID); err != nil {
		return err
	}
	if err := o.required("listing", &v.Listing); err != nil {
		return err
	}
	if raw, ok := o["item"]; ok {
		if v.Item, err = decodeValue(raw); err != nil {
			return fieldErr("item", err)
		}
	}
	*e = v
	return nil
}

// FetchListing is the `listing` of a [FetchEntry].
type FetchListing struct {
	Indexed *string `json:"indexed"`
	// Ready-made whisper ("@Seller Hi, I would like to buy ...").
	Whisper *string       `json:"whisper"`
	Price   *FetchPrice   `json:"price"`
	Account *FetchAccount `json:"account"`
}

// MarshalJSON writes serde-compatible JSON.
func (l FetchListing) MarshalJSON() ([]byte, error) {
	type plain FetchListing
	return core.MarshalSerde(plain(l))
}

// UnmarshalJSON decodes like serde.
func (l *FetchListing) UnmarshalJSON(data []byte) error {
	o, err := decodeObject(data, "struct FetchListing")
	if err != nil {
		return err
	}
	var v FetchListing
	for key, dst := range map[string]any{
		"indexed": &v.Indexed, "whisper": &v.Whisper, "price": &v.Price, "account": &v.Account,
	} {
		if err := o.optional(key, dst); err != nil {
			return err
		}
	}
	*l = v
	return nil
}

// FetchPrice is a listing's asking price.
type FetchPrice struct {
	Kind     *string `json:"type"`
	Amount   float64 `json:"amount"`
	Currency string  `json:"currency"`
}

// MarshalJSON writes serde-compatible JSON.
func (p FetchPrice) MarshalJSON() ([]byte, error) {
	type plain FetchPrice
	return core.MarshalSerde(plain(p))
}

// UnmarshalJSON decodes like serde.
func (p *FetchPrice) UnmarshalJSON(data []byte) error {
	o, err := decodeObject(data, "struct FetchPrice")
	if err != nil {
		return err
	}
	var v FetchPrice
	if err := o.optional("type", &v.Kind); err != nil {
		return err
	}
	if err := o.required("amount", &v.Amount); err != nil {
		return err
	}
	if err := o.required("currency", &v.Currency); err != nil {
		return err
	}
	*p = v
	return nil
}

// FetchAccount is the seller. Online is the raw `online` value: nil when offline (JSON null
// or absent), e.g. `{"league":"Standard"}` when online.
type FetchAccount struct {
	Name   string `json:"name"`
	Online any    `json:"online"`
}

// MarshalJSON writes serde-compatible JSON.
func (a FetchAccount) MarshalJSON() ([]byte, error) {
	type plain FetchAccount
	return core.MarshalSerde(plain(a))
}

// UnmarshalJSON decodes like serde.
func (a *FetchAccount) UnmarshalJSON(data []byte) error {
	o, err := decodeObject(data, "struct FetchAccount")
	if err != nil {
		return err
	}
	var v FetchAccount
	if err := o.required("name", &v.Name); err != nil {
		return err
	}
	if err := o.optionalValue("online", &v.Online); err != nil {
		return err
	}
	*a = v
	return nil
}

// ExchangeResponse is the `POST /api/trade2/exchange/poe2/{league}` response. `result` is
// an object keyed by listing id (server order = sort order), or `[]` when empty; both decode
// to a slice in document order. Decode-only, like the Rust type.
type ExchangeResponse struct {
	ID         string
	Complexity *uint32
	Result     []ExchangeEntry
	Total      uint32
}

// UnmarshalJSON decodes like serde.
func (r *ExchangeResponse) UnmarshalJSON(data []byte) error {
	o, err := decodeObject(data, "struct ExchangeResponse")
	if err != nil {
		return err
	}
	var v ExchangeResponse
	if err := o.required("id", &v.ID); err != nil {
		return err
	}
	if err := o.optional("complexity", &v.Complexity); err != nil {
		return err
	}
	raw, ok := o["result"]
	if !ok {
		return fmt.Errorf("missing field `result`")
	}
	if v.Result, err = orderedEntries(raw); err != nil {
		return fieldErr("result", err)
	}
	if err := o.required("total", &v.Total); err != nil {
		return err
	}
	*r = v
	return nil
}

func (r *ExchangeResponse) clone() *ExchangeResponse {
	c := *r
	c.Result = make([]ExchangeEntry, len(r.Result))
	for i, e := range r.Result {
		e.Listing.Offers = append([]ExchangeOffer(nil), e.Listing.Offers...)
		c.Result[i] = e
	}
	return &c
}

// orderedEntries decodes a map (values in document order) or a list of exchange entries.
func orderedEntries(raw []byte) ([]ExchangeEntry, error) {
	out := []ExchangeEntry{}
	switch jsonKind(raw) {
	case "sequence":
		if err := json.Unmarshal(raw, &out); err != nil {
			return nil, err
		}
		return out, nil
	case "map":
		dec := json.NewDecoder(bytes.NewReader(raw))
		if _, err := dec.Token(); err != nil { // {
			return nil, err
		}
		for dec.More() {
			if _, err := dec.Token(); err != nil { // key
				return nil, err
			}
			var e ExchangeEntry
			if err := dec.Decode(&e); err != nil {
				return nil, err
			}
			out = append(out, e)
		}
		return out, nil
	}
	return nil, fmt.Errorf("invalid type: %s, expected a map or list of exchange entries", jsonKind(raw))
}

// ExchangeEntry is one bulk-exchange listing.
type ExchangeEntry struct {
	ID      string          `json:"id"`
	Listing ExchangeListing `json:"listing"`
}

// MarshalJSON writes serde-compatible JSON.
func (e ExchangeEntry) MarshalJSON() ([]byte, error) {
	type plain ExchangeEntry
	return core.MarshalSerde(plain(e))
}

// UnmarshalJSON decodes like serde.
func (e *ExchangeEntry) UnmarshalJSON(data []byte) error {
	o, err := decodeObject(data, "struct ExchangeEntry")
	if err != nil {
		return err
	}
	var v ExchangeEntry
	if err := o.required("id", &v.ID); err != nil {
		return err
	}
	if err := o.required("listing", &v.Listing); err != nil {
		return err
	}
	*e = v
	return nil
}

// ExchangeListing is the `listing` of an [ExchangeEntry].
type ExchangeListing struct {
	Indexed *string         `json:"indexed"`
	Account *FetchAccount   `json:"account"`
	Offers  []ExchangeOffer `json:"offers"`
}

// MarshalJSON writes serde-compatible JSON.
func (l ExchangeListing) MarshalJSON() ([]byte, error) {
	type plain ExchangeListing
	return core.MarshalSerde(plain(l))
}

// UnmarshalJSON decodes like serde.
func (l *ExchangeListing) UnmarshalJSON(data []byte) error {
	o, err := decodeObject(data, "struct ExchangeListing")
	if err != nil {
		return err
	}
	var v ExchangeListing
	if err := o.optional("indexed", &v.Indexed); err != nil {
		return err
	}
	if err := o.optional("account", &v.Account); err != nil {
		return err
	}
	if err := o.defaulted("offers", &v.Offers); err != nil {
		return err
	}
	*l = v
	return nil
}

// ExchangeOffer is one offer: the seller gives Item.Amount of Item.Currency for
// Exchange.Amount of Exchange.Currency.
type ExchangeOffer struct {
	Exchange ExchangeSide `json:"exchange"`
	Item     ExchangeSide `json:"item"`
}

// MarshalJSON writes serde-compatible JSON.
func (o ExchangeOffer) MarshalJSON() ([]byte, error) {
	type plain ExchangeOffer
	return core.MarshalSerde(plain(o))
}

// UnmarshalJSON decodes like serde.
func (o *ExchangeOffer) UnmarshalJSON(data []byte) error {
	obj, err := decodeObject(data, "struct ExchangeOffer")
	if err != nil {
		return err
	}
	var v ExchangeOffer
	if err := obj.required("exchange", &v.Exchange); err != nil {
		return err
	}
	if err := obj.required("item", &v.Item); err != nil {
		return err
	}
	*o = v
	return nil
}

// ExchangeSide is one side of an [ExchangeOffer].
type ExchangeSide struct {
	Currency string  `json:"currency"`
	Amount   float64 `json:"amount"`
	Stock    *uint32 `json:"stock"`
}

// MarshalJSON writes serde-compatible JSON.
func (s ExchangeSide) MarshalJSON() ([]byte, error) {
	type plain ExchangeSide
	return core.MarshalSerde(plain(s))
}

// UnmarshalJSON decodes like serde.
func (s *ExchangeSide) UnmarshalJSON(data []byte) error {
	o, err := decodeObject(data, "struct ExchangeSide")
	if err != nil {
		return err
	}
	var v ExchangeSide
	if err := o.required("currency", &v.Currency); err != nil {
		return err
	}
	if err := o.required("amount", &v.Amount); err != nil {
		return err
	}
	if err := o.optional("stock", &v.Stock); err != nil {
		return err
	}
	*s = v
	return nil
}
