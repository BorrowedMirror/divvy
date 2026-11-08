package platform

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The JSON below is the frontend contract.
func TestJSONShapes(t *testing.T) {
	cases := []struct {
		v    any
		want string
	}{
		{Action(PriceCheck), `"price_check"`},
		{Action(PriceCheckPinned), `"price_check_pinned"`},
		{ChatAction(2), `{"chat":2}`},
		{StashSearchAction(0), `{"stash_search":0}`},
		{HotkeyEvent{Action: Action(OpenTrade)}, `{"action":"open_trade"}`},
		{WindowEvent{Type: WindowFound, Info: WindowInfo{Title: "Path of Exile 2", Rect: Rect{1, 2, 3, 4}, Focused: true, NativeID: 9}},
			`{"type":"found","title":"Path of Exile 2","rect":{"x":1,"y":2,"width":3,"height":4},"focused":true,"native_id":9}`},
		{WindowEvent{Type: WindowMoved, Rect: Rect{-5, 0, 10, 10}}, `{"type":"moved","rect":{"x":-5,"y":0,"width":10,"height":10}}`},
		{WindowEvent{Type: WindowFocusChanged, Focused: false}, `{"type":"focus_changed","focused":false}`},
		{WindowEvent{Type: WindowLost}, `{"type":"lost"}`},
		{InputEvent{Type: KeyDown, Key: "Escape"}, `{"type":"key_down","key":"Escape"}`},
		{InputEvent{Type: Wheel, Delta: -1}, `{"type":"wheel","delta":-1}`},
		{Capabilities{}, `{"window_tracking":false,"overlay_always_on_top":false,"overlay_click_through":false,"global_hotkeys":false,"passive_hook":false,"keystroke_injection":false,"clipboard":false,"missing_permissions":[],"notes":[]}`},
		{BackendX11, `"x11"`},
	}
	for _, c := range cases {
		b, err := json.Marshal(c.v)
		require.NoError(t, err)
		assert.JSONEq(t, c.want, string(b))
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
