package trade

import (
	"encoding/json/v2"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExchangeResultAcceptsMapAndEmptyList(t *testing.T) {
	var full exchangeResponse
	require.NoError(t, json.Unmarshal(fixtureBytes(t, "exchange.json"), &full), "map form")
	assert.Len(t, full.Result, 3)
	assert.Equal(t, "divine", full.Result[0].Listing.Offers[0].Item.Currency)
	var empty exchangeResponse
	require.NoError(t, json.Unmarshal(fixtureBytes(t, "exchange.empty.json"), &empty), "list form")
	assert.Empty(t, empty.Result)
	var bad exchangeResponse
	err := json.Unmarshal([]byte(`{"id":"x","result":5,"total":0}`), &bad)
	require.ErrorContains(t, err, "map or list")
}

// The map form keeps document order (a Go map would not).
func TestExchangeResultKeepsDocumentOrder(t *testing.T) {
	var r exchangeResponse
	require.NoError(t, json.Unmarshal([]byte(`{"id":"x","total":2,"result":{
		"z":{"id":"second-key-first","listing":{"offers":[]}},
		"a":{"id":"then-this","listing":{}}}}`), &r))
	require.Len(t, r.Result, 2)
	assert.Equal(t, "second-key-first", r.Result[0].ID)
	assert.Equal(t, "then-this", r.Result[1].ID)
}

func TestWireTypesDecode(t *testing.T) {
	var l League
	require.NoError(t, json.Unmarshal([]byte(`{"ID":"x"}`), &l))
	assert.Empty(t, l.ID, "field names are case-sensitive")
	require.NoError(t, json.Unmarshal([]byte(`{"id":"Standard","realm":null}`), &l))
	assert.Nil(t, l.Realm)

	var p fetchPrice
	require.NoError(t, json.Unmarshal([]byte(`{"type":"~price","amount":2,"currency":"divine"}`), &p))
	assert.Equal(t, fetchPrice{Kind: "~price", Amount: 2, Currency: "divine"}, p)

	var s searchResponse
	require.Error(t, json.Unmarshal([]byte(`{"id":"x","result":[],"total":1.5}`), &s), "u32 from a fraction")

	var e fetchEntry
	require.NoError(t, json.Unmarshal([]byte(`{"id":"x","listing":{"account":{"name":"a","online":null}}}`), &e))
	assert.False(t, e.Listing.Account.online())
	require.NoError(t, json.Unmarshal([]byte(`{"id":"x","listing":{"account":{"name":"a","online":{"league":"Standard"}}}}`), &e))
	assert.True(t, e.Listing.Account.online())
	var missing fetchEntry
	require.NoError(t, json.Unmarshal([]byte(`{"id":"x","listing":{"account":{"name":"a"}}}`), &missing))
	assert.False(t, missing.Listing.Account.online(), "missing is offline")
}

// League and CurrencyIcon are sent to the frontend.
func TestFrontendTypesJSON(t *testing.T) {
	b, err := json.Marshal(League{ID: "Standard", Realm: new("poe2")})
	require.NoError(t, err)
	assert.JSONEq(t, `{"id":"Standard","realm":"poe2","text":null}`, string(b))
	b, err = json.Marshal(CurrencyIcon{ID: "a", Name: "A", Icon: "https://x/a.png"})
	require.NoError(t, err)
	assert.JSONEq(t, `{"id":"a","name":"A","icon":"https://x/a.png"}`, string(b))
}

func TestRecordedFetchDecodes(t *testing.T) {
	var list resultList[*fetchEntry]
	require.NoError(t, json.Unmarshal(fixtureBytes(t, "fetch.json"), &list))
	require.NotEmpty(t, list.Result)
	first := list.Result[0]
	require.NotNil(t, first.Item.ItemLevel)
	assert.Equal(t, uint32(40), *first.Item.ItemLevel)
}
