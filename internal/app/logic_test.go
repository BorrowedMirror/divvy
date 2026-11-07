package app

// Port of the test module of apps/desktop/src-tauri/src/logic.rs, plus JSON-shape tests for
// the types logic.rs sends to the frontend (checked against web/src/bindings.ts).

import (
	"encoding/json"
	"errors"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/BorrowedMirror/rust-king-ledger/internal/config"
	"github.com/BorrowedMirror/rust-king-ledger/internal/core"
	"github.com/BorrowedMirror/rust-king-ledger/internal/data"
	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
	"github.com/BorrowedMirror/rust-king-ledger/internal/testkit"
	"github.com/BorrowedMirror/rust-king-ledger/internal/trade"
)

type xy[T int32 | float64] struct{ x, y T }

func pair[T int32 | float64](x, y T) xy[T] { return xy[T]{x, y} }

func TestLogicCardPositionInsideTheCoveringWindow(t *testing.T) {
	game := platform.Rect{X: 2560, Y: 0, Width: 3440, Height: 1440}
	for _, c := range []struct {
		card   xy[int32]
		game   platform.Rect
		scale  float64
		points bool
		want   xy[float64]
	}{
		{pair[int32](3000, 300), game, 1, false, pair(440.0, 300.0)},
		{pair[int32](3000, 300), game, 2, false, pair(220.0, 150.0)},
		{pair[int32](100, 50), game, 1, false, pair(0.0, 50.0)},
		{pair[int32](400, 100), platform.Rect{X: 100, Y: 25, Width: 800, Height: 600}, 2, true, pair(100.0, 25.0)},
	} {
		if got := pair(CardInWindow(c.card.x, c.card.y, c.game, c.scale, c.points)); got != c.want {
			t.Errorf("CardInWindow(%v, %v, %v, %v) = %v, want %v", c.card, c.game, c.scale, c.points, got, c.want)
		}
	}
}

func TestLogicPanelSideSettingPlacesTheCard(t *testing.T) {
	area := Area{X: 2560, Y: 0, Width: 3440, Height: 1440}
	for _, c := range []struct {
		side config.OverlaySide
		w    int32
		want xy[int32]
	}{
		{config.SideCursor, 420, pair[int32](3024, 400)},
		{config.SideLeft, 420, pair[int32](2568, 400)},
		{config.SideRight, 420, pair[int32](5572, 400)},
	} {
		if got := pair(PanelPosition(c.side, 3000, 700, c.w, 600, area)); got != c.want {
			t.Errorf("PanelPosition(%s) = %v, want %v", c.side, got, c.want)
		}
	}
	if x, _ := PanelPosition(config.SideRight, 3000, 700, 5000, 600, area); x != 2560 {
		t.Errorf("too wide for the right edge: x = %d, want 2560", x)
	}
}

func TestLogicUnidentifiedUniquesListTheirPossibleUniques(t *testing.T) {
	gameData, err := data.Bundled()
	if err != nil {
		t.Fatal(err)
	}
	text := "Item Class: Rings\nRarity: Unique\nGold Ring\n--------\nItem Level: 75\n--------\nUnidentified\n"
	defaults := config.DefaultSettings()
	opts := QueryOptionsFor(&defaults, "Standard")
	_, f, err := ProcessItemText(text, gameData, &opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.UniqueCandidates) < 2 {
		t.Errorf("unique candidates %q", f.UniqueCandidates)
	}
	if f.Name != nil || f.BaseType == nil || *f.BaseType != "Gold Ring" || f.Misc.Identified == nil || *f.Misc.Identified {
		t.Errorf("name %v, base type %v, identified %v", f.Name, f.BaseType, f.Misc.Identified)
	}
	_, rare, err := ProcessItemText("Item Class: Rings\nRarity: Rare\nX\nGold Ring\n", gameData, &opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(rare.UniqueCandidates) != 0 {
		t.Errorf("rare unique candidates %q", rare.UniqueCandidates)
	}
}

func TestLogicPointInRectIncludesEdges(t *testing.T) {
	in := func(px, py float64) bool { return PointInRect(px, py, 100, 50, 420, 600) }
	if !in(100, 50) || !in(520, 650) || !in(300, 300) {
		t.Error("edges and inside points must be in")
	}
	if in(99, 300) || in(300, 651) {
		t.Error("outside points must be out")
	}
}

func TestLogicClampIntoKeepsPanelsOnScreen(t *testing.T) {
	area := Area{X: 0, Y: 0, Width: 1920, Height: 1080}
	for _, c := range []struct {
		p, size, want xy[int32]
	}{
		{pair[int32](100, 100), pair[int32](420, 600), pair[int32](100, 100)},
		{pair[int32](1800, 900), pair[int32](420, 600), pair[int32](1500, 480)},
		{pair[int32](-50, -10), pair[int32](420, 600), pair[int32](0, 0)},
		{pair[int32](10, 10), pair[int32](3000, 600), pair[int32](0, 10)}, // too wide: pin to the left edge
	} {
		if got := pair(ClampInto(c.p.x, c.p.y, c.size.x, c.size.y, area)); got != c.want {
			t.Errorf("ClampInto(%v, %v) = %v, want %v", c.p, c.size, got, c.want)
		}
	}
}

func TestLogicLookupURLsUseFixedHostsAndEncodeNames(t *testing.T) {
	for _, c := range []struct {
		site LookupSite
		name string
		want string // "" = none
	}{
		{LookupPoe2db, "Gold Ring", "https://poe2db.tw/us/Gold_Ring"},
		{LookupWiki, "Ventor's Gamble", "https://www.poe2wiki.net/wiki/Ventor's_Gamble"},
		{LookupWiki, "a/b?c#d", "https://www.poe2wiki.net/wiki/a%2Fb%3Fc%23d"},
		{LookupPoe2db, "  ", ""},
		{LookupCraftOfExile, "", "https://beta.craftofexile.com/?game=poe2"},
		// Beyond the Rust test: the url crate's path-segment encoding.
		{LookupWiki, `100% "x" <y> {z} a\b`, "https://www.poe2wiki.net/wiki/100%25_%22x%22_%3Cy%3E_%7Bz%7D_a%5Cb"},
		{LookupPoe2db, "Kalandra's Touché", "https://poe2db.tw/us/Kalandra's_Touch%C3%A9"},
		{LookupPoe2db, "a\tb", "https://poe2db.tw/us/ab"},
		{LookupPoe2db, ".", "https://poe2db.tw/us"},
		{LookupPoe2db, "..", "https://poe2db.tw/us"},
		{LookupPoe2db, ".\t.", "https://poe2db.tw/"},
	} {
		got, ok := LookupURL(c.site, c.name, "")
		if ok != (c.want != "") || got != c.want {
			t.Errorf("LookupURL(%s, %q) = %q, %v, want %q", c.site, c.name, got, ok, c.want)
		}
	}
}

func TestLogicCraftOfExileImportsTheItemTextFromTheURL(t *testing.T) {
	text := "Item Class: Rings\r\nRarity: Rare\r\nGale Coil\r\nGold Ring\r\n--------\r\n+20% to Fire Resistance & more"
	raw, ok := LookupURL(LookupCraftOfExile, "Gold Ring", text)
	if !ok {
		t.Fatal("no url")
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if u.Host != "beta.craftofexile.com" {
		t.Errorf("host %q", u.Host)
	}
	q := parseQueryPairs(t, u.RawQuery)
	if q[0] != [2]string{"game", "poe2"} {
		t.Errorf("first pair %q", q[0])
	}
	if want := strings.ReplaceAll(text, "\r\n", "\n"); q[1] != [2]string{"eimport", want} {
		t.Errorf("round-trips exactly, with \\n line ends: %q", q[1])
	}
	// form_urlencoded keeps `*-._`, escapes `~`, and writes spaces as `+`.
	if got, _ := LookupURL(LookupCraftOfExile, "", "a b*~"); got != "https://beta.craftofexile.com/?game=poe2&eimport=a+b*%7E" {
		t.Errorf("form encoding: %q", got)
	}
	blank, _ := LookupURL(LookupCraftOfExile, "", "  ")
	for _, p := range parseQueryPairs(t, mustParseURL(t, blank).RawQuery) {
		if p[0] == "eimport" {
			t.Error("blank text is not imported")
		}
	}
}

// parseQueryPairs decodes a form-urlencoded query in order.
func parseQueryPairs(t *testing.T, raw string) [][2]string {
	t.Helper()
	var out [][2]string
	for p := range strings.SplitSeq(raw, "&") {
		k, v, _ := strings.Cut(p, "=")
		k, err1 := url.QueryUnescape(k)
		v, err2 := url.QueryUnescape(v)
		if err := errors.Join(err1, err2); err != nil {
			t.Fatal(err)
		}
		out = append(out, [2]string{k, v})
	}
	return out
}

func mustParseURL(t *testing.T, s string) *url.URL {
	t.Helper()
	u, err := url.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

type waystoneEntry struct {
	text  string
	group WaystoneModGroup
}

func TestLogicWaystoneCatalogGroupsDedupesAndAddsExtras(t *testing.T) {
	stats := []string{
		"Monsters fire # additional Projectiles",
		"Players have #% less Recovery Rate of Life and Energy Shield",
		"#% increased Pack Size in Map",
		"+# to maximum Life",
		"Monsters fire # additional Projectiles",
		"Area has patches of Shocked Ground",
	}
	c := WaystoneCatalog(slices.Values(stats))
	entries := make([]waystoneEntry, len(c))
	for i, m := range c {
		entries[i] = waystoneEntry{m.Text, m.Group}
	}
	if !slices.Contains(entries, waystoneEntry{"Players have -#% to all maximum Resistances", WaystonePlayer}) {
		t.Error("extras included")
	}
	if !slices.Contains(entries, waystoneEntry{"#% increased Pack Size in Map", WaystoneArea}) {
		t.Error("pack size is an area mod")
	}
	if slices.ContainsFunc(entries, func(e waystoneEntry) bool { return e.text == "+# to maximum Life" }) {
		t.Error("ordinary item mods are left out")
	}
	n := 0
	for _, e := range entries {
		if e.text == "Monsters fire # additional Projectiles" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("deduplicated: %d copies", n)
	}
	if !slices.IsSortedFunc(c, func(a, b WaystoneMod) int {
		if g := a.Group.Compare(b.Group); g != 0 {
			return g
		}
		return strings.Compare(a.Text, b.Text)
	}) {
		t.Errorf("sorted: %v", c)
	}
	if c[0].Group != WaystonePlayer || c[len(c)-1].Group != WaystoneArea {
		t.Errorf("groups sort in declaration order (player, monster, area): %v", c)
	}
	for _, m := range config.DefaultDangerousWaystoneMods {
		if _, ok := WaystoneGroup(m); !ok {
			t.Errorf("%s must be in the catalogue", m)
		}
	}
}

func TestLogicIntersectAreas(t *testing.T) {
	game := Area{X: 2560, Y: 0, Width: 3440, Height: 1440}
	monitor := Area{X: 2560, Y: 0, Width: 3440, Height: 1440}
	if got := Intersect(game, monitor); got != game {
		t.Errorf("same areas: %v", got)
	}
	half := Area{X: 2560, Y: 0, Width: 1000, Height: 1440}
	if got := Intersect(game, half); got != half {
		t.Errorf("half: %v", got)
	}
	apart := Area{X: 0, Y: 0, Width: 10, Height: 10}
	if got := Intersect(game, apart); got != game {
		t.Errorf("no overlap keeps the first area: %v", got)
	}
}

func key(k string, mods ...platform.KeyModifier) MacroStep {
	return PressStep(platform.NewChord(k, mods...))
}

func checkSteps(t *testing.T, what string, got []MacroStep, err error, want ...MacroStep) {
	t.Helper()
	if err != nil {
		t.Errorf("%s: %v", what, err)
		return
	}
	if !slices.EqualFunc(got, want, MacroStep.Equal) {
		t.Errorf("%s:\n got %+v\nwant %+v", what, got, want)
	}
}

func TestLogicChatPlaceholdersAndChaining(t *testing.T) {
	enter, ctrlA := key("Enter"), key("KeyA", platform.Ctrl)
	paste := PasteStep
	known := ChatContext{Last: new("Buyer"), Area: new("Felled Hideout")}
	if got := ChatLines(" @last ty&& /kick @last &&"); !slices.Equal(got, []string{"@last ty", "/kick @last"}) {
		t.Errorf("ChatLines = %q", got)
	}
	if last, area := ChatPlaceholders("/hideout @last"); !last || area {
		t.Errorf("ChatPlaceholders = %v, %v", last, area)
	}
	// Known whisper: a leading @last whispers, elsewhere it is the bare name. All but the last
	// line are sent even without send.
	steps, err := ChatSteps("@last ty @last&&/kick @last", false, known)
	checkSteps(t, "known whisper", steps, err,
		enter, ctrlA, paste("@Buyer ty Buyer"), enter, enter, ctrlA, paste("/kick Buyer"))
	steps, err = ChatSteps("Now in @area", true, known)
	checkSteps(t, "area", steps, err, enter, ctrlA, paste("Now in Felled Hideout"), enter)
	// No log: Ctrl+Enter makes the game type `@Name `.
	none := ChatContext{}
	steps, err = ChatSteps("@last ty", true, none)
	checkSteps(t, "reply", steps, err, key("Enter", platform.Ctrl), key("End"), paste("ty"), enter)
	steps, err = ChatSteps("/invite @last", true, none)
	checkSteps(t, "invite", steps, err,
		key("Enter", platform.Ctrl), key("Home"), key("Delete"), paste("/invite "), enter)
	if _, err := ChatSteps("@last hi @last", true, none); !errors.Is(err, ChatNoWhisper) {
		t.Errorf("two @last without a whisper: %v", err)
	}
	_, err = ChatSteps("in @area", true, none)
	if e, ok := errors.AsType[ChatError](err); !ok || e != ChatNoArea {
		t.Errorf("@area without an area: %v", err)
	}
}

func TestLogicChatStashAndWheelSteps(t *testing.T) {
	enter, ctrlA := key("Enter"), key("KeyA", platform.Ctrl)
	none := ChatContext{}
	steps, err := ChatSteps("/hideout", true, none)
	checkSteps(t, "send", steps, err, enter, ctrlA, PasteStep("/hideout"), enter)
	steps, err = ChatSteps("hi", false, none)
	checkSteps(t, "no send", steps, err, enter, ctrlA, PasteStep("hi"))
	steps, err = ChatSteps(" && ", true, none)
	checkSteps(t, "empty", steps, err)
	checkSteps(t, "stash", StashSteps(`"Rarity: Rare"`), nil,
		key("KeyF", platform.Ctrl), key("KeyA", platform.Ctrl), PasteStep(`"Rarity: Rare"`), enter)
	k1, ok1 := WheelTabKey(1)
	k2, ok2 := WheelTabKey(-3)
	k3, ok3 := WheelTabKey(0)
	if k1 != "ArrowLeft" || !ok1 || k2 != "ArrowRight" || !ok2 || k3 != "" || ok3 {
		t.Errorf("WheelTabKey: %q %v, %q %v, %q %v", k1, ok1, k2, ok2, k3, ok3)
	}
	if !IsCtrlKey("ControlLeft") || !IsCtrlKey("ControlRight") || IsCtrlKey("ShiftLeft") {
		t.Error("IsCtrlKey")
	}
	if got := HotkeyEffectFor(platform.ChatAction(2), true, true); got != (HotkeyEffect{Kind: EffectRunChat, Index: 2}) {
		t.Errorf("chat: %+v", got)
	}
	if got := HotkeyEffectFor(platform.StashSearchAction(0), false, false); got != (HotkeyEffect{Kind: EffectRunStash, Index: 0}) {
		t.Errorf("stash: %+v", got)
	}
}

func TestLogicHotkeysToggleTheirWindow(t *testing.T) {
	a := platform.Action
	for _, c := range []struct {
		action                    platform.HotkeyAction
		overlayOpen, settingsOpen bool
		want                      HotkeyEffect
	}{
		{a(platform.PriceCheck), false, false, HotkeyEffect{Kind: EffectPriceCheck, Pinned: false}},
		{a(platform.PriceCheckPinned), false, true, HotkeyEffect{Kind: EffectPriceCheck, Pinned: true}},
		{a(platform.PriceCheck), true, false, HotkeyEffect{Kind: EffectHideOverlay}},
		{a(platform.PriceCheckPinned), true, false, HotkeyEffect{Kind: EffectHideOverlay}},
		{a(platform.OpenSettings), true, false, HotkeyEffect{Kind: EffectShowSettings}},
		{a(platform.OpenSettings), false, true, HotkeyEffect{Kind: EffectHideSettings}},
		{a(platform.OpenTrade), true, true, HotkeyEffect{Kind: EffectOpenTrade}},
	} {
		if got := HotkeyEffectFor(c.action, c.overlayOpen, c.settingsOpen); got != c.want {
			t.Errorf("HotkeyEffectFor(%v, %v, %v) = %+v, want %+v", c.action, c.overlayOpen, c.settingsOpen, got, c.want)
		}
	}
}

func TestLogicOverlayHeightFitsContentWithinTheScreen(t *testing.T) {
	for _, c := range []struct{ wanted, monitor, want float64 }{
		{431.2, 1440, 432},
		{50, 1440, MinOverlayHeight},
		{5000, 1440, 1440 - 2*OverlayScreenMargin},
		{nan(), 1440, MinOverlayHeight},
		{500, 100, MinOverlayHeight},
		// Beyond the Rust test: f64::max ignores a NaN monitor height.
		{500, nan(), MinOverlayHeight},
	} {
		if got := FitOverlayHeight(c.wanted, c.monitor); got != c.want {
			t.Errorf("FitOverlayHeight(%v, %v) = %v, want %v", c.wanted, c.monitor, got, c.want)
		}
	}
}

func nan() float64 { return math.NaN() }

func TestLogicKeepsWindowInsideTheMonitorVertically(t *testing.T) {
	for _, c := range []struct{ y, h, want int32 }{{100, 400, 100}, {1200, 400, 1040}, {-20, 400, 0}, {100, 2000, 0}} {
		if got := KeepOnScreenY(c.y, c.h, 0, 1440); got != c.want {
			t.Errorf("KeepOnScreenY(%d, %d) = %d, want %d", c.y, c.h, got, c.want)
		}
	}
}

func TestLogicSettingsShownWhenUnreachable(t *testing.T) {
	if MustShowSettings(true, true) || MustShowSettings(false, true) || MustShowSettings(true, false) {
		t.Error("reachable through the tray or a hotkey")
	}
	if !MustShowSettings(false, false) {
		t.Error("unreachable")
	}
}

func TestLogicFlowGateSerialisesAndDebounces(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		gate := NewFlowGate(FlowGateDebounce)
		t0 := time.Now()
		ms := func(n int) time.Time { return t0.Add(time.Duration(n) * time.Millisecond) }
		p, ok := gate.TryBegin(t0)
		if !ok {
			t.Fatal("first press runs")
		}
		if _, ok := gate.TryBegin(ms(500)); ok {
			t.Error("dropped while in flight")
		}
		p.Release()
		if _, ok := gate.TryBegin(ms(100)); ok {
			t.Error("debounced")
		}
		p2, ok := gate.TryBegin(ms(200))
		if !ok {
			t.Fatal("after debounce")
		}
		p.Release() // a stale permit does not end the running flow
		if _, ok := gate.TryBegin(ms(1000)); ok {
			t.Error("stale release let a second flow in")
		}
		p2.Release()
		if _, ok := gate.TryBegin(t0); ok {
			t.Error("earlier instant is too soon")
		}
		time.Sleep(time.Second)
		if _, ok := gate.TryBegin(time.Now()); !ok {
			t.Error("a press a second later runs")
		}
	})
}

func TestLogicCopyRequiresFocusOnlyWithTracking(t *testing.T) {
	if !MayCopy(false, func() bool { return false }) {
		t.Error("no tracking => allow")
	}
	tracker := testkit.NewFakeWindowTracker()
	if MayCopy(true, tracker.IsGameFocused) {
		t.Error("no game window")
	}
	tracker.SetWindow(new(testkit.GameWindow(platform.Rect{X: 0, Y: 0, Width: 10, Height: 10})))
	if !MayCopy(true, tracker.IsGameFocused) {
		t.Error("game focused")
	}
	tracker.SetFocused(false)
	if MayCopy(true, tracker.IsGameFocused) {
		t.Error("terminal focused")
	}
	if !MayCopy(false, tracker.IsGameFocused) {
		t.Error("no tracking => allow")
	}
}

func TestLogicLogFilterCapsHTTPStack(t *testing.T) {
	if got := LogFilter(""); got != "info,reqwest=info,hyper=info,hyper_util=info,h2=info,rustls=info,enigo=error" {
		t.Errorf("default: %q", got)
	}
	if strings.Contains(LogFilter("debug,enigo=debug"), "enigo=error") {
		t.Error("an explicit enigo level is kept")
	}
	f := LogFilter("debug")
	if !strings.HasPrefix(f, "debug,") || !strings.Contains(f, "hyper=info") || !strings.Contains(f, "reqwest=info") {
		t.Errorf("debug: %q", f)
	}
	if got := LogFilter("  "); got != LogFilter("") {
		t.Errorf("blank env: %q", got)
	}
	// The Rust test also checked the string with tracing_subscriber::EnvFilter::try_new; there
	// is no EnvFilter in Go.
}

func TestLogicOverrideURLsMustBeHTTPSOrLoopback(t *testing.T) {
	for raw, want := range map[string]bool{
		"https://example.com":    true,
		"http://127.0.0.1:18080": true,
		"http://localhost:1":     true,
		"http://[::1]:1":         true,
		"http://example.com":     false,
		"http://10.0.0.1":        false,
		"file:///etc/passwd":     false,
		// Beyond the Rust test: WHATWG host forms.
		"http://LOCALHOST:1":     true,
		"http://127.1":           true,
		"http://0x7f000001":      true,
		"http://2130706433":      true,
		"http://[::ffff:7f00:1]": false,
		"http://256.0.0.1":       false,
		"http://localhost.":      false,
	} {
		if got := IsSafeOverride(mustParseURL(t, raw)); got != want {
			t.Errorf("IsSafeOverride(%q) = %v, want %v", raw, got, want)
		}
	}
	if IsSafeOverride(nil) {
		t.Error("nil")
	}
}

func TestLogicTradeURLAllowList(t *testing.T) {
	for raw, want := range map[string]bool{
		"https://www.pathofexile.com/trade2/search/poe2/Standard/abc": true,
		"https://pathofexile.com/trade/search/Standard/abc":           true,
		"https://www.pathofexile.com/forum/view-thread/1":             false,
		"https://www.pathofexile.com/trade2":                          false,
		"http://www.pathofexile.com/trade2/search/x":                  false,
		"https://www.pathofexile.com:8443/trade2/search/x":            false,
		"https://evil.example.com/trade2/search/x":                    false,
		"not a url": false,
		// Beyond the Rust test: the url crate's normalisation.
		"https://www.pathofexile.com:443/trade2/search/x":   true,
		"HTTPS://WWW.PathOfExile.com/trade2/search/x":       true,
		"https://www.pathofexile.com/trade2/../forum/x":     false,
		"https://www.pathofexile.com/trade2/%2e%2E/forum/x": false,
		`https://www.pathofexile.com/trade2\..\forum`:       false,
		`https://www.pathofexile.com\trade2\search\x`:       true,
		"https://www.pathofexile.com/trade2/x/..":           true,
		"https://www.pathofexile.com/trade2%2Fsearch":       false,
		" https://www.pathofexile.com/tra\tde2/search/x ":   true,
		"https://www.pathofexile.com/forum?/trade2/x":       false,
	} {
		if got := IsAllowedTradeURL(raw, nil); got != want {
			t.Errorf("IsAllowedTradeURL(%q) = %v, want %v", raw, got, want)
		}
	}
	local := mustParseURL(t, "http://127.0.0.1:18080")
	if !IsAllowedTradeURL("http://127.0.0.1:18080/trade2/search/poe2/x", local) {
		t.Error("override origin")
	}
	if IsAllowedTradeURL("http://127.0.0.1:9999/trade2/search/poe2/x", local) {
		t.Error("other port")
	}
	if !IsAllowedTradeURL("http://127.1:18080/trade2/search/poe2/x", local) {
		t.Error("same origin spelled differently")
	}
	remote := mustParseURL(t, "http://evil.example.com")
	if IsAllowedTradeURL("http://evil.example.com/trade2/search/x", remote) {
		t.Error("unsafe override")
	}
}

func TestLogicOversizedItemTextIsRejected(t *testing.T) {
	defaults := config.DefaultSettings()
	opts := QueryOptionsFor(&defaults, "Standard")
	_, _, err := ProcessItemText(strings.Repeat("x", MaxItemText+1), nil, &opts)
	e, ok := errors.AsType[*ItemTextError](err)
	if !ok || e.IPCKind() != "parse" || e.Error() != "item text too large" {
		t.Fatalf("err = %v", err)
	}
	_, _, err = ProcessItemText("", nil, &opts)
	if e, ok := errors.AsType[*ItemTextError](err); !ok || e.IPCKind() != "parse" || e.Err == nil || e.Error() != e.Err.Error() {
		t.Errorf("parse error: %v", err)
	}
}

func testLeague(id string) trade.League {
	return trade.League{ID: id, Realm: new("poe2"), Text: new(id)}
}

func TestLogicAutoLeaguePrefersChallengeSoftcore(t *testing.T) {
	check := func(ls []trade.League, want string, wantOK bool) {
		t.Helper()
		if got, ok := AutoLeague(ls); got != want || ok != wantOK {
			t.Errorf("AutoLeague = %q, %v, want %q, %v", got, ok, want, wantOK)
		}
	}
	check([]trade.League{testLeague("Standard"), testLeague("Hardcore"), testLeague("Rise of the Abyssal"), testLeague("HC Rise of the Abyssal")},
		"Rise of the Abyssal", true)
	check([]trade.League{testLeague("Standard"), testLeague("Hardcore")}, "Standard", true)
	check(nil, "", false)
	check([]trade.League{testLeague("SSF Standard")}, "", false)
}

func TestLogicResolveLeagueRespectsManualSetting(t *testing.T) {
	s := config.DefaultSettings()
	s.League.AutoDetect = false
	s.League.League = new("Standard")
	dawn := []trade.League{testLeague("Dawn")}
	if got := ResolveLeague(&s, dawn); got != "Standard" {
		t.Errorf("manual: %q", got)
	}
	s.League.AutoDetect = true
	if got := ResolveLeague(&s, dawn); got != "Dawn" {
		t.Errorf("auto: %q", got)
	}
	if got := ResolveLeague(&s, nil); got != "Standard" {
		t.Errorf("offline: %q", got)
	}
	s.League.League = nil
	if got := ResolveLeague(&s, nil); got != "Standard" {
		t.Errorf("offline, nothing saved: %q", got)
	}
}

func TestLogicOverlayFlipsNearRightEdgeAndClamps(t *testing.T) {
	screen := Area{X: 0, Y: 0, Width: 1920, Height: 1080}
	tiny := Area{X: 0, Y: 0, Width: 300, Height: 200}
	for _, c := range []struct {
		cursor xy[int32]
		screen Area
		want   xy[int32]
	}{
		{pair[int32](100, 500), screen, pair[int32](124, 220)},
		{pair[int32](1800, 500), screen, pair[int32](1356, 220)},
		{pair[int32](1800, 10), screen, pair[int32](1356, 0)},
		{pair[int32](1800, 1070), screen, pair[int32](1356, 520)},
		{pair[int32](10, 10), tiny, pair[int32](0, 0)},
	} {
		if got := pair(OverlayPosition(c.cursor.x, c.cursor.y, 420, 560, c.screen)); got != c.want {
			t.Errorf("OverlayPosition(%v, %v) = %v, want %v", c.cursor, c.screen, got, c.want)
		}
	}
}

func TestLogicUserAgentAppendsContact(t *testing.T) {
	s := config.DefaultSettings()
	if strings.HasSuffix(UserAgent(&s), "me@example.com") {
		t.Error("no contact by default")
	}
	s.Trade.Contact = new("me@example.com")
	if got := UserAgent(&s); !strings.HasSuffix(got, " me@example.com") || !strings.HasPrefix(got, trade.DefaultUserAgent()) {
		t.Errorf("with contact: %q", got)
	}
}

func TestLogicQueryOptionsFromSettings(t *testing.T) {
	s := config.DefaultSettings()
	o := QueryOptionsFor(&s, "Dawn")
	if o.League != "Dawn" || o.Status != core.StatusAny {
		t.Errorf("%+v", o)
	}
}

// --- JSON shapes of the types sent to the frontend --------------------------------------------

// bindingsTS returns web/src/bindings.ts, which tauri-specta generated from the Rust types.
func bindingsTS(t *testing.T) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	b, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "web", "src", "bindings.ts"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// tsUnion returns the string literals of `export type name = "a" | "b";`.
func tsUnion(t *testing.T, ts, name string) []string {
	t.Helper()
	m := regexp.MustCompile(`(?m)^export type ` + name + ` = ((?:"[^"]*"(?: \| )?)+);`).FindStringSubmatch(ts)
	if m == nil {
		t.Fatalf("no union %s in bindings.ts", name)
	}
	var out []string
	for _, lit := range regexp.MustCompile(`"([^"]*)"`).FindAllStringSubmatch(m[1], -1) {
		out = append(out, lit[1])
	}
	return out
}

// tsFields returns the field names of `export type name = { ... };`, in order.
func tsFields(t *testing.T, ts, name string) []string {
	t.Helper()
	m := regexp.MustCompile(`(?s)\nexport type ` + name + ` = \{\n(.*?)\n\};`).FindStringSubmatch(ts)
	if m == nil {
		t.Fatalf("no object type %s in bindings.ts", name)
	}
	var out []string
	for _, f := range regexp.MustCompile(`(?m)^\t(\w+)\??: `).FindAllStringSubmatch(m[1], -1) {
		out = append(out, f[1])
	}
	return out
}

func enumStrings[T ~string](vs []T) []string {
	out := make([]string, len(vs))
	for i, v := range vs {
		out[i] = string(v)
	}
	return out
}

func TestLogicFrontendEnumsMatchBindings(t *testing.T) {
	ts := bindingsTS(t)
	if got, want := enumStrings(LookupSiteValues()), tsUnion(t, ts, "LookupSite"); !slices.Equal(got, want) {
		t.Errorf("LookupSite %q, bindings.ts %q", got, want)
	}
	if got, want := enumStrings(WaystoneModGroupValues()), tsUnion(t, ts, "WaystoneModGroup"); !slices.Equal(got, want) {
		t.Errorf("WaystoneModGroup %q, bindings.ts %q", got, want)
	}
	for _, v := range LookupSiteValues() {
		b, err := json.Marshal(v)
		if err != nil || string(b) != `"`+string(v)+`"` {
			t.Errorf("marshal %s: %s %v", v, b, err)
		}
		var back LookupSite
		if err := json.Unmarshal(b, &back); err != nil || back != v {
			t.Errorf("round trip %s: %s %v", v, back, err)
		}
	}
	for _, bad := range []string{`"Poe2db"`, `"craftOfExile"`, `""`, `1`} {
		var s LookupSite
		if err := json.Unmarshal([]byte(bad), &s); err == nil {
			t.Errorf("LookupSite accepted %s", bad)
		}
		var g WaystoneModGroup
		if err := json.Unmarshal([]byte(bad), &g); err == nil {
			t.Errorf("WaystoneModGroup accepted %s", bad)
		}
	}
	var g WaystoneModGroup
	if err := json.Unmarshal([]byte(`"monster"`), &g); err != nil || g != WaystoneMonster {
		t.Errorf("WaystoneModGroup: %s %v", g, err)
	}
}

func TestLogicWaystoneModJSONMatchesSerde(t *testing.T) {
	if got, want := tsFields(t, bindingsTS(t), "WaystoneMod"), []string{"text", "group"}; !slices.Equal(got, want) {
		t.Errorf("bindings.ts WaystoneMod fields %q, want %q", got, want)
	}
	mods := []WaystoneMod{
		{Text: "Players have -#% to all maximum Resistances", Group: WaystonePlayer},
		{Text: `Monsters deal <b> & "more"`, Group: WaystoneMonster},
	}
	b, err := core.MarshalSerde(mods)
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"text":"Players have -#% to all maximum Resistances","group":"player"},` +
		`{"text":"Monsters deal <b> & \"more\"","group":"monster"}]`
	if string(b) != want {
		t.Errorf("serde JSON\n got %s\nwant %s", b, want)
	}
	if b, err := mods[1].MarshalJSON(); err != nil || !strings.Contains(string(b), "<b> &") {
		t.Errorf("MarshalJSON writes the serde bytes: %s %v", b, err)
	}
	var back []WaystoneMod
	if err := json.Unmarshal([]byte(want), &back); err != nil || !slices.Equal(back, mods) {
		t.Errorf("round trip %v %v", back, err)
	}
	for _, bad := range []string{`{"text":"x"}`, `{"group":"area"}`, `{"text":"x","group":"Area"}`} {
		var m WaystoneMod
		if err := json.Unmarshal([]byte(bad), &m); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
	// An empty catalogue is `[]`, never null.
	if b, err := core.MarshalSerde([]WaystoneMod(nil)); err != nil || string(b) != "[]" {
		t.Errorf("empty: %s %v", b, err)
	}
}
