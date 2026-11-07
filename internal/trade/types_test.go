package trade

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/BorrowedMirror/rust-king-ledger/internal/core"
	"github.com/BorrowedMirror/rust-king-ledger/internal/testkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fixtureBytes(t testing.TB, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(testkit.TradeFixturesDir(), name))
	require.NoError(t, err)
	return b
}

func TestExchangeResultAcceptsMapAndEmptyList(t *testing.T) {
	var full ExchangeResponse
	require.NoError(t, json.Unmarshal(fixtureBytes(t, "exchange.json"), &full), "map form")
	assert.Len(t, full.Result, 3)
	assert.Equal(t, "divine", full.Result[0].Listing.Offers[0].Item.Currency)
	var empty ExchangeResponse
	require.NoError(t, json.Unmarshal(fixtureBytes(t, "exchange.empty.json"), &empty), "list form")
	assert.Empty(t, empty.Result)
	var bad ExchangeResponse
	err := json.Unmarshal([]byte(`{"id":"x","result":5,"total":0}`), &bad)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "map or list")
}

// Go-only: the map form keeps document order (Go maps would not), and decoding is as strict
// as serde's derive.
func TestExchangeResultKeepsDocumentOrder(t *testing.T) {
	var r ExchangeResponse
	require.NoError(t, json.Unmarshal([]byte(`{"id":"x","total":2,"result":{
		"z":{"id":"second-key-first","listing":{"offers":[]}},
		"a":{"id":"then-this","listing":{}}}}`), &r))
	require.Len(t, r.Result, 2)
	assert.Equal(t, "second-key-first", r.Result[0].ID)
	assert.Equal(t, "then-this", r.Result[1].ID)
}

func TestWireTypesDecodeLikeSerde(t *testing.T) {
	var l League
	require.Error(t, json.Unmarshal([]byte(`{"realm":"poe2"}`), &l), "missing id")
	require.Error(t, json.Unmarshal([]byte(`{"ID":"x"}`), &l), "field names are case-sensitive")
	require.Error(t, json.Unmarshal([]byte(`{"id":null}`), &l), "null for a String")
	require.NoError(t, json.Unmarshal([]byte(`{"id":"Standard","realm":null}`), &l))
	assert.Nil(t, l.Realm)

	var p FetchPrice
	require.Error(t, json.Unmarshal([]byte(`{"currency":"divine"}`), &p), "missing amount")
	require.NoError(t, json.Unmarshal([]byte(`{"type":"~price","amount":2,"currency":"divine"}`), &p))
	assert.Equal(t, "~price", *p.Kind)

	var el ExchangeListing
	require.Error(t, json.Unmarshal([]byte(`{"offers":null}`), &el), "default Vec rejects null")
	require.NoError(t, json.Unmarshal([]byte(`{}`), &el))

	var s SearchResponse
	require.Error(t, json.Unmarshal([]byte(`{"id":"x","result":[],"total":1.0}`), &s), "u32 from float")
	require.Error(t, json.Unmarshal([]byte(`null`), &s))

	var e FetchEntry
	require.NoError(t, json.Unmarshal([]byte(`{"id":"x","listing":{"account":{"name":"a","online":null}}}`), &e))
	assert.Nil(t, e.Item)
	assert.Nil(t, e.Listing.Account.Online)
}

func TestWireTypesEncodeLikeSerde(t *testing.T) {
	realm := "poe2"
	b, err := json.Marshal(League{ID: "Standard", Realm: &realm})
	require.NoError(t, err)
	assert.Equal(t, `{"id":"Standard","realm":"poe2","text":null}`, string(b))

	b, err = core.MarshalSerde(FetchPrice{Amount: 1, Currency: "divine"})
	require.NoError(t, err)
	assert.Equal(t, `{"type":null,"amount":1.0,"currency":"divine"}`, string(b))

	b, err = core.MarshalSerde(ResultList[League]{})
	require.NoError(t, err)
	assert.Equal(t, `{"result":[]}`, string(b))

	b, err = core.MarshalSerde(CurrencyIcon{ID: "a", Name: "A", Icon: "https://x/a.png"})
	require.NoError(t, err)
	assert.Equal(t, `{"id":"a","name":"A","icon":"https://x/a.png"}`, string(b))

	// Round trip of a recorded fetch entry keeps the item JSON (integers stay integers).
	var list ResultList[*FetchEntry]
	require.NoError(t, json.Unmarshal(fixtureBytes(t, "fetch.json"), &list))
	b, err = core.MarshalSerde(list.Result[0])
	require.NoError(t, err)
	var back FetchEntry
	require.NoError(t, json.Unmarshal(b, &back))
	assert.Equal(t, *list.Result[0], back)
	assert.Contains(t, string(b), `"ilvl":40`)
}
