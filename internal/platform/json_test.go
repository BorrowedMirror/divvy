package platform

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The JSON below is the frontend contract.
func TestJSONShapes(t *testing.T) {
	testCases := []struct {
		name     string
		have     any
		expected string
	}{
		{
			"PriceCheckAction",
			Action(PriceCheck),
			`"price_check"`,
		},
		{
			"PinnedPriceCheckAction",
			Action(PriceCheckPinned),
			`"price_check_pinned"`,
		},
		{
			"ChatAction",
			ChatAction(2),
			`{"chat":2}`,
		},
		{
			"StashSearchAction",
			StashSearchAction(0),
			`{"stash_search":0}`,
		},
		{
			"HotkeyEvent",
			HotkeyEvent{Action: Action(OpenTrade)},
			`{"action":"open_trade"}`,
		},
		{
			"WindowFound",
			WindowEvent{Type: WindowFound, Info: WindowInfo{Title: "Path of Exile 2", Rect: Rect{1, 2, 3, 4}, Focused: true, NativeID: 9}},
			`{"type":"found","title":"Path of Exile 2","rect":{"x":1,"y":2,"width":3,"height":4},"focused":true,"native_id":9}`,
		},
		{
			"WindowMoved",
			WindowEvent{Type: WindowMoved, Rect: Rect{-5, 0, 10, 10}},
			`{"type":"moved","rect":{"x":-5,"y":0,"width":10,"height":10}}`,
		},
		{
			"WindowFocusChanged",
			WindowEvent{Type: WindowFocusChanged, Focused: false},
			`{"type":"focus_changed","focused":false}`,
		},
		{
			"WindowLost",
			WindowEvent{Type: WindowLost},
			`{"type":"lost"}`,
		},
		{
			"KeyDown",
			InputEvent{Type: KeyDown, Key: "Escape"},
			`{"type":"key_down","key":"Escape"}`,
		},
		{
			"Wheel",
			InputEvent{Type: Wheel, Delta: -1},
			`{"type":"wheel","delta":-1}`,
		},
		{
			"EmptyCapabilities",
			Capabilities{},
			`{"window_tracking":false,"overlay_always_on_top":false,"overlay_click_through":false,"global_hotkeys":false,"passive_hook":false,"keystroke_injection":false,"clipboard":false,"missing_permissions":[],"notes":[]}`,
		},
		{
			"BackendKind",
			BackendX11,
			`"x11"`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			b, err := json.Marshal(tc.have)
			require.NoError(t, err)
			assert.JSONEq(t, tc.expected, string(b))
		})
	}
}

func TestJSONRoundTrips(t *testing.T) {
	for _, a := range []HotkeyAction{Action(PriceCheck), Action(OpenSettings), ChatAction(7), StashSearchAction(3)} {
		b, err := json.Marshal(a)
		require.NoError(t, err)
		var back HotkeyAction
		require.NoError(t, json.Unmarshal(b, &back))
		assert.Equal(t, a, back)
	}
	var bad HotkeyAction
	require.Error(t, json.Unmarshal([]byte(`"nope"`), &bad))
	require.Error(t, json.Unmarshal([]byte(`{"chat":1,"stash_search":2}`), &bad))
	require.Error(t, json.Unmarshal([]byte(`"chat"`), &bad))

	for _, e := range []WindowEvent{
		{Type: WindowFound, Info: WindowInfo{Title: "x", Rect: Rect{1, 2, 3, 4}, NativeID: 5}},
		{Type: WindowMoved, Rect: Rect{1, 1, 1, 1}},
		{Type: WindowFocusChanged, Focused: true},
		{Type: WindowLost},
	} {
		b, err := json.Marshal(e)
		require.NoError(t, err)
		var back WindowEvent
		require.NoError(t, json.Unmarshal(b, &back))
		assert.Equal(t, e, back)
	}

	var k BackendKind
	require.NoError(t, json.Unmarshal([]byte(`"wayland"`), &k))
	assert.Equal(t, BackendWayland, k)
	assert.Error(t, json.Unmarshal([]byte(`"Wayland"`), &k))
}
