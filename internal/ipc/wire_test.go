package ipc

// The wire format of every payload, marshalled the way Wails does (encoding/json v1): see
// wire.go.

import (
	stdjson "encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/BorrowedMirror/divvy/internal/config"
	"github.com/BorrowedMirror/divvy/internal/core"
	"github.com/BorrowedMirror/divvy/internal/platform"
	"github.com/BorrowedMirror/divvy/internal/trade"
)

// wailsJSON marshals v as Wails marshals a bound method's result or an event payload.
func wailsJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := stdjson.Marshal(v)
	require.NoError(t, err)
	return string(b)
}

const (
	zeroItem = `{"class":{"type":""},"category":"","rarity":"","name":null,"base_type":"","item_level":null,` +
		`"quality":null,"stack_size":null,"gem_level":null,"waystone_tier":null,"area_level":null,"sockets":0,` +
		`"requirements":{"level":null,"str":null,"dex":null,"int":null},` +
		`"stats":{"armour":null,"evasion":null,"energy_shield":null,"spirit":null,"block_chance":null,` +
		`"physical_dps":null,"elemental_dps":null,"total_dps":null,"attacks_per_second":null,"critical_hit_chance":null},` +
		`"properties":[],"modifiers":[],"corrupted":false,"unidentified":false,"mirrored":false,"sanctified":false,` +
		`"note":null,"advanced":false,"raw":""}`
	zeroRange   = `{"enabled":false,"min":null,"max":null}`
	zeroFilters = `{"league":"","status":"","name":null,"base_type":null,"category":null,"rarity":null,"stats":[],` +
		`"misc":{"item_level":` + zeroRange + `,"quality":` + zeroRange + `,"gem_level":` + zeroRange +
		`,"waystone_tier":` + zeroRange + `,"sockets":` + zeroRange + `,"corrupted":null,"identified":null},` +
		`"widen_pct":0,"route":"","unique_candidates":[]}`
	zeroSummary = `{"source":"","total":0,"listings":[],"min":null,"median":null,"max":null,"trade_url":null}`
	zeroError   = `{"kind":"","message":"","retry_after_ms":null}`
)

func TestPayloadsZeroValues(t *testing.T) {
	testCases := []struct {
		name     string
		have     any
		expected string
	}{
		{
			"Error",
			Error{},
			zeroError,
		},
		{
			"ErrorPointer",
			&Error{},
			zeroError,
		},
		{
			"Diagnostics",
			Diagnostics{},
			`{"version":"","os":"","backend":"","capabilities":{"window_tracking":false,` +
				`"overlay_always_on_top":false,"overlay_click_through":false,"global_hotkeys":false,"passive_hook":false,` +
				`"keystroke_injection":false,"clipboard":false,"missing_permissions":[],"notes":[]},"data_version":null,` +
				`"league":null,"settings_path":null,"log_path":null,"recent_errors":[]}`,
		},
		{
			"PriceCheckResponse",
			PriceCheckResponse{},
			`{"item":` + zeroItem + `,"filters":` + zeroFilters + `}`,
		},
		{
			"ItemCopied",
			ItemCopied{},
			`{"item":` + zeroItem + `,"filters":` + zeroFilters + `,"pinned":false}`,
		},
		{
			"NilItemCopiedPointer",
			(*ItemCopied)(nil),
			`null`,
		},
		{
			"PriceReady",
			PriceReady{},
			`{"summary":` + zeroSummary + `}`,
		},
		{
			"PriceFailed",
			PriceFailed{},
			`{"error":` + zeroError + `}`,
		},
		{
			"OverlayState",
			OverlayState{},
			`{"visible":false,"pinned":false}`,
		},
		{
			"OverlayLayout",
			OverlayLayout{},
			`{"cover":false,"x":0,"y":0}`,
		},
		{
			"UpdateAvailable",
			UpdateAvailable{},
			`{"version":"","notes":null,"date":null}`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.JSONEq(t, tc.expected, wailsJSON(t, tc.have))
		})
	}
}

func TestPayloadsPopulated(t *testing.T) {
	item, err := core.ParseItem("Item Class: Rings\nRarity: Rare\nGale Loop\nSapphire Ring\n--------\n" +
		"Item Level: 75\n--------\n+52 to maximum Life\n+12% to <Fire> & Resistance")
	require.NoError(t, err)
	filters := core.BuildFilters(item, &core.QueryOptions{League: "Standard", Status: core.StatusOnline, WidenPct: 10})
	got := wailsJSON(t, ItemCopied{Item: *item, Filters: filters, Pinned: true})
	assert.JSONEq(t, `{"item":{"class":{"type":"ring"},"category":"equipment","rarity":"rare","name":"Gale Loop",`+
		`"base_type":"Sapphire Ring","item_level":75,"quality":null,"stack_size":null,"gem_level":null,`+
		`"waystone_tier":null,"area_level":null,"sockets":0,`+
		`"requirements":{"level":null,"str":null,"dex":null,"int":null},`+
		`"stats":{"armour":null,"evasion":null,"energy_shield":null,"spirit":null,"block_chance":null,`+
		`"physical_dps":null,"elemental_dps":null,"total_dps":null,"attacks_per_second":null,"critical_hit_chance":null},`+
		`"properties":[],"modifiers":[`+
		`{"text":"+52 to maximum Life","template":"+# to maximum Life","kind":"explicit","values":[52],"stat_id":null,"advanced":null},`+
		`{"text":"+12% to <Fire> & Resistance","template":"+#% to <Fire> & Resistance","kind":"explicit","values":[12],"stat_id":null,"advanced":null}],`+
		`"corrupted":false,"unidentified":false,"mirrored":false,"sanctified":false,"note":null,"advanced":false,`+
		`"raw":"Item Class: Rings\nRarity: Rare\nGale Loop\nSapphire Ring\n--------\nItem Level: 75\n--------\n+52 to maximum Life\n+12% to <Fire> & Resistance"},`+
		`"filters":{"league":"Standard","status":"online","name":null,"base_type":"Sapphire Ring",`+
		`"category":"accessory.ring","rarity":"nonunique","stats":[{"stat_id":"pseudo.pseudo_total_life",`+
		`"text":"+52 total maximum Life","enabled":true,"min":46,"max":null,"item_values":[52],"tiers":[]}],`+
		`"misc":{"item_level":{"enabled":false,"min":75,"max":null},"quality":`+zeroRange+`,"gem_level":`+zeroRange+
		`,"waystone_tier":`+zeroRange+`,"sockets":`+zeroRange+`,"corrupted":null,"identified":null},`+
		`"widen_pct":10,"route":"search","unique_candidates":[]},"pinned":true}`, got)

	summary := core.Summarise(core.SourceTrade, 2, []core.Listing{
		{ID: "a", Price: core.Price{Amount: 1.5, Currency: "divine"}, Seller: new("seller#1"), Online: true},
	}, nil)
	summary.TradeURL = new("https://www.pathofexile.com/trade2/search/poe2/Standard/abc")
	assert.JSONEq(t, `{"summary":{"source":"trade","total":2,"listings":[{"id":"a","price":{"amount":1.5,"currency":"divine"},`+
		`"seller":"seller#1","online":true,"indexed":null,"item_level":null,"stack_size":null,"whisper":null,"equivalent":null}],`+
		`"min":{"amount":1.5,"currency":"divine"},"median":{"amount":1.5,"currency":"divine"},"max":{"amount":1.5,"currency":"divine"},`+
		`"trade_url":"https://www.pathofexile.com/trade2/search/poe2/Standard/abc"}}`, wailsJSON(t, PriceReady{Summary: summary}))

	assert.JSONEq(t, `{"error":{"kind":"rate_limited","message":"slow down","retry_after_ms":1500}}`,
		wailsJSON(t, PriceFailed{Error: Error{Kind: KindRateLimited, Message: "slow down", RetryAfterMS: new(uint64(1500))}}))
	assert.JSONEq(t, `{"cover":true,"x":12,"y":3.5}`, wailsJSON(t, OverlayLayout{Cover: true, X: 12, Y: 3.5}))
	assert.JSONEq(t, `{"visible":true,"pinned":true}`, wailsJSON(t, OverlayState{Visible: true, Pinned: true}))
	assert.JSONEq(t, `{"version":"1.2.3","notes":"fixes","date":"2026-11-01T00:00:00Z"}`,
		wailsJSON(t, UpdateAvailable{Version: "1.2.3", Notes: new("fixes"), Date: new("2026-11-01T00:00:00Z")}))
	assert.JSONEq(t, `{"version":"1.0","os":"linux","backend":"x11","capabilities":{"window_tracking":true,`+
		`"overlay_always_on_top":false,"overlay_click_through":false,"global_hotkeys":false,"passive_hook":false,`+
		`"keystroke_injection":false,"clipboard":true,"missing_permissions":[],"notes":["n"]},"data_version":"0.5",`+
		`"league":"Standard","settings_path":null,"log_path":null,"recent_errors":["boom"]}`,
		wailsJSON(t, Diagnostics{Version: "1.0", OS: "linux", Backend: platform.BackendX11,
			Capabilities: platform.Capabilities{WindowTracking: true, Clipboard: true, Notes: []string{"n"}},
			DataVersion:  new("0.5"), League: new("Standard"), RecentErrors: []string{"boom"}}))
}

// Values a command returns directly, outside a payload type, are built non-nil.
func TestDirectResults(t *testing.T) {
	assert.JSONEq(t, `{"source":"trade","total":0,"listings":[],"min":null,"median":null,"max":null,"trade_url":null}`,
		wailsJSON(t, core.Summarise(core.SourceTrade, 0, nil, nil)))
	assert.JSONEq(t, `{"id":"Standard","realm":null,"text":null}`, wailsJSON(t, trade.League{ID: "Standard"}))
	assert.JSONEq(t, `{"id":"divine","name":"Divine Orb","icon":"https://x/d.png"}`,
		wailsJSON(t, trade.CurrencyIcon{ID: "divine", Name: "Divine Orb", Icon: "https://x/d.png"}))
}

// Every list and map in every payload, nested ones included, is written as [] or {} when
// nil, never null.
func TestPayloadsHaveNoNullCollections(t *testing.T) {
	for _, v := range []any{
		Error{}, Diagnostics{}, PriceCheckResponse{}, ItemCopied{}, PriceReady{}, PriceFailed{},
		SettingsChanged{}, SettingsChanged{Settings: config.DefaultSettings()}, OverlayState{}, OverlayLayout{},
		UpdateAvailable{},
	} {
		t.Run(reflect.TypeOf(v).Name(), func(t *testing.T) {
			var decoded any
			require.NoError(t, stdjson.Unmarshal([]byte(wailsJSON(t, v)), &decoded))
			assertNoNullCollections(t, reflect.ValueOf(v), decoded, reflect.TypeOf(v).Name())
		})
	}
}

// assertNoNullCollections walks v alongside its decoded JSON and fails on a slice or map
// field written as null. Keys the JSON does not have (renamed or custom-marshalled fields)
// are skipped.
func assertNoNullCollections(t *testing.T, v reflect.Value, j any, path string) {
	t.Helper()
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if !v.IsNil() {
			assertNoNullCollections(t, v.Elem(), j, path)
		}
	case reflect.Slice, reflect.Map:
		if j == nil {
			t.Errorf("%s is null", path)
		}
		if a, ok := j.([]any); ok && v.Kind() == reflect.Slice {
			for i := range min(v.Len(), len(a)) {
				assertNoNullCollections(t, v.Index(i), a[i], path+"[]")
			}
		}
	case reflect.Struct:
		obj, ok := j.(map[string]any)
		if !ok {
			return
		}
		for i := range v.NumField() {
			f := v.Type().Field(i)
			if !f.IsExported() {
				continue
			}
			name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
			if name == "" {
				name = f.Name
			}
			if jv, ok := obj[name]; ok {
				assertNoNullCollections(t, v.Field(i), jv, path+"."+name)
			}
		}
	default:
		// Scalars cannot be a null collection.
	}
}
