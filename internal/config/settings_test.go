package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/BorrowedMirror/rust-king-ledger/internal/core"
	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
)

// ---------------------------------------------------------------- serde

// Go's TOML differs in layout from the Rust snapshot (keys sorted, literal strings), so the
// snapshot is a Go golden file; the Rust snapshot itself is checked to load as the defaults
// in TestRustDefaultFileLoadsAsDefaults. Regenerate with UPDATE_GOLDEN=1.
func TestDefaultSettingsTOMLSnapshot(t *testing.T) {
	got := toTOML(t, DefaultSettings())
	golden := filepath.Join("testdata", "go_default.toml")
	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if want := fixture(t, "go_default.toml"); got != want {
		t.Fatalf("default TOML changed:\n%s", got)
	}
	assertSettings(t, mustFromTOML(t, got), DefaultSettings())
}

func TestDefaultSettingsAreValid(t *testing.T) {
	s := DefaultSettings()
	if err := Validate(&s); err != nil {
		t.Fatal(err)
	}
}

func TestSettingsRoundtripThroughTOML(t *testing.T) {
	s := DefaultSettings()
	s.League.AutoDetect = false
	s.League.League = new(`Hardcore "Quoted"`)
	s.Hotkeys.OpenSettings = chord("F5", platform.Meta)
	s.Trade.Source = core.SourcePoe2Scout
	s.Trade.Contact = new("exile@example.com")
	s.Trade.Poesessid = new("abc123")
	s.Overlay.Side = SideLeft
	s.UI.Theme = ThemeLight
	s.Platform.BackendOverride = ptr(platform.BackendWayland)
	assertSettings(t, mustFromTOML(t, toTOML(t, s)), s)
}

func TestOnboardingDoneDefaultsToFalseAndRoundtrips(t *testing.T) {
	if DefaultSettings().UI.OnboardingDone {
		t.Fatal("onboarding_done defaults to true")
	}
	old := mustFromTOML(t, "version = 1\n[ui]\nlanguage = \"de\"\nstart_minimised = false\n")
	if old.UI.OnboardingDone {
		t.Fatal("old file has onboarding_done")
	}
	assertEqual(t, old.UI.Language, "de")
	s := DefaultSettings()
	s.UI.OnboardingDone = true
	if !mustFromTOML(t, toTOML(t, s)).UI.OnboardingDone {
		t.Fatal("onboarding_done lost")
	}
}

func TestPartialDocumentFillsDefaults(t *testing.T) {
	s := mustFromTOML(t, "version = 1\n[overlay]\nopacity = 0.5\n")
	assertEqual(t, s.Overlay.Opacity, 0.5)
	assertEqual(t, s.Overlay.AutoHideMs, DefaultSettings().Overlay.AutoHideMs)
	assertEqual(t, s.Trade, DefaultSettings().Trade)
}

func TestCheckUpdatesDefaultsOnAndSurvivesOlderFiles(t *testing.T) {
	if !DefaultSettings().UI.CheckUpdates {
		t.Fatal("check_updates defaults off")
	}
	s := mustFromTOML(t, "version = 1\n[ui]\nlanguage = \"de\"\n")
	if !s.UI.CheckUpdates {
		t.Fatal("files written before the field existed keep updates on")
	}
	off := mustFromTOML(t, "version = 1\n[ui]\ncheck_updates = false\n")
	if off.UI.CheckUpdates {
		t.Fatal("check_updates = false ignored")
	}
	if err := Validate(&off); err != nil {
		t.Fatal(err)
	}
}

// ---------------------------------------------------------------- serde semantics on koanf

func TestSerdeDefaultSemantics(t *testing.T) {
	t.Run("waystone flags table replaces the defaults", func(t *testing.T) {
		s := mustFromTOML(t, "[waystone.flags]\n\"Monsters fire # additional Projectiles\" = \"warn\"\n")
		assertEqual(t, s.Waystone.Flags, map[string]ModFlag{"Monsters fire # additional Projectiles": FlagWarn})
	})
	t.Run("empty waystone flags table means none", func(t *testing.T) {
		s := mustFromTOML(t, "[waystone.flags]\n")
		assertEqual(t, len(s.Waystone.Flags), 0)
		s = mustFromTOML(t, "[waystone]\nflags = {}\n")
		assertEqual(t, len(s.Waystone.Flags), 0)
	})
	t.Run("flag keys with dots", func(t *testing.T) {
		s := mustFromTOML(t, "[waystone.flags]\n\"1.5x Pack.Size\" = \"good\"\n")
		assertEqual(t, s.Waystone.Flags, map[string]ModFlag{"1.5x Pack.Size": FlagGood})
	})
	t.Run("empty waystone section keeps the defaults", func(t *testing.T) {
		s := mustFromTOML(t, "[waystone]\n")
		assertEqual(t, s.Waystone, DefaultSettings().Waystone)
	})
	t.Run("chat commands replace the defaults wholesale", func(t *testing.T) {
		s := mustFromTOML(t, "[[chat.commands]]\ntext = \"/exit\"\nsend = false\n[chat.commands.chord]\nmodifiers = []\nkey = \"F9\"\n")
		assertEqual(t, s.Chat.Commands, []ChatCommand{{Chord: chord("F9"), Text: "/exit"}})
		assertEqual(t, s.Chat.GameLog, "")
		s = mustFromTOML(t, "[chat]\ncommands = []\n")
		assertEqual(t, len(s.Chat.Commands), 0)
	})
	t.Run("listing columns replace the defaults", func(t *testing.T) {
		s := mustFromTOML(t, "[overlay]\nlisting_columns = [\"stock\"]\n")
		assertEqual(t, s.Overlay.ListingColumns, []ListingColumn{ColumnStock})
	})
	t.Run("links fill per field", func(t *testing.T) {
		s := mustFromTOML(t, "[overlay.links]\nwiki = false\n")
		assertEqual(t, s.Overlay.Links, LinkSettings{Poe2db: true, Wiki: false, CraftOfExile: true})
	})
	t.Run("integers are accepted for floats", func(t *testing.T) {
		s := mustFromTOML(t, "[trade]\nwiden_pct = 25\n")
		assertEqual(t, s.Trade.WidenPct, 25.0)
	})
	t.Run("unknown keys are ignored", func(t *testing.T) {
		s := mustFromTOML(t, "future = 1\n[trade]\nnew_thing = \"x\"\n")
		assertSettings(t, s, DefaultSettings())
	})
	rejects := []struct{ name, text, want string }{
		{"chord without modifiers", "[hotkeys.price_check]\nkey = \"KeyD\"\n", "missing field `modifiers`"},
		{"chord without key", "[hotkeys.open_trade]\nmodifiers = []\n", "missing field `key`"},
		{"chat command without send", "[[chat.commands]]\ntext = \"/exit\"\nchord = { modifiers = [], key = \"\" }\n", "missing field `send`"},
		{"chat command without chord", "[[chat.commands]]\ntext = \"/exit\"\nsend = true\n", "missing field `chord`"},
		{"stash search without text", "[[stash.searches]]\nchord = { modifiers = [], key = \"\" }\n", "missing field `text`"},
		{"unknown enum variant", "[ui]\ntheme = \"blue\"\n", "unknown variant `blue`"},
		{"unknown modifier", "[hotkeys.price_check]\nmodifiers = [\"hyper\"]\nkey = \"KeyD\"\n", "unknown variant `hyper`"},
		{"unknown backend", "[platform]\nbackend_override = \"beos\"\n", "unknown variant `beos`"},
		{"unknown flag", "[waystone.flags]\nx = \"meh\"\n", "unknown variant `meh`"},
		{"negative u32", "[overlay]\nauto_hide_ms = -1\n", "expected u32"},
		{"u32 overflow", "[trade]\nlisting_count = 4294967296\n", "expected u32"},
		{"float for u32", "[trade]\nlisting_count = 20.0\n", "expected u32"},
		{"string for float", "[overlay]\nopacity = \"high\"\n", "opacity"},
		{"string for bool", "[ui]\nautostart = \"yes\"\n", "autostart"},
		{"int for string", "[ui]\nlanguage = 3\n", "language"},
		{"section not a table", "trade = 1\n", "trade"},
	}
	for _, c := range rejects {
		t.Run(c.name, func(t *testing.T) {
			_, err := fromTOML(c.text)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("want error containing %q, got %v", c.want, err)
			}
		})
	}
}

func TestRustDefaultFileLoadsAsDefaults(t *testing.T) {
	paths := Under(t.TempDir())
	writeSettings(t, paths, fixture(t, "rust_default.toml"))
	s, err := Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	assertSettings(t, s, DefaultSettings())
}

func TestRustCustomFileLoads(t *testing.T) {
	paths := Under(t.TempDir())
	writeSettings(t, paths, fixture(t, "rust_custom.toml"))
	got, err := Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	want := Settings{
		Version: 1,
		League:  LeagueSettings{AutoDetect: false, League: new(`Hardcore "Quoted"`)},
		Hotkeys: HotkeySettings{
			PriceCheck:       chord("KeyD", platform.Alt),
			PriceCheckPinned: chord("KeyD", platform.Ctrl, platform.Shift),
			OpenSettings:     chord("F5", platform.Meta),
			OpenTrade:        chord("KeyT", platform.Ctrl),
			AdvancedCopy:     false,
		},
		Trade: TradeSettings{
			Source: core.SourcePoe2Scout, Sellers: core.StatusSecurable, WidenPct: 25.5,
			EnableAllStats: true, ListingCount: 40, ConvertPrices: false, RememberFilters: false,
			Contact: new("exile@example.com"), Poesessid: new("abc123"),
		},
		Overlay: OverlaySettings{
			AutoHideMs: 1500, Opacity: 0.8, Side: SideLast, GroupBySeller: false,
			ListingColumns: []ListingColumn{ColumnItemLevel, ColumnStock, ColumnSeller},
			Scale:          1.25, BackdropDim: 0, ClosePinnedOnGameFocus: true,
			Links:   LinkSettings{Poe2db: false, Wiki: true, CraftOfExile: false},
			Results: ResultsAuto,
		},
		Waystone: WaystoneSettings{Flags: map[string]ModFlag{
			"#% increased Pack Size in Map":          FlagGood,
			"Monsters fire # additional Projectiles": FlagWarn,
		}},
		Chat: ChatSettings{
			Commands: []ChatCommand{
				{Chord: chord("F6"), Text: "/hideout", Send: true},
				{Chord: chord("Enter", platform.Ctrl), Text: "@last ", Send: false},
			},
			GameLog: `C:\Games\Path of Exile 2\logs\Client.txt`,
		},
		Stash: StashSettings{
			Searches:       []StashSearch{{Chord: chord("Digit1", platform.Alt), Text: `"Rarity: Rare"`}},
			CtrlScrollTabs: true,
		},
		UI: UISettings{
			Language: "de", Theme: ThemeLight, StartMinimised: false, CheckUpdates: false,
			OnboardingDone: true, Autostart: true,
		},
		Platform: PlatformSettings{BackendOverride: ptr(platform.BackendWayland)},
	}
	assertSettings(t, got, want)

	// What Go writes back loads to the same settings.
	if err := Save(paths, &got); err != nil {
		t.Fatal(err)
	}
	again, err := Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	assertSettings(t, again, want)
}

// ---------------------------------------------------------------- JSON

// defaultSettingsJSON is serde_json's output for `Settings::default()`, derived from the
// Rust serde definitions: declaration order, snake_case enums, None as null, f64 always
// with a fraction, BTreeMap keys sorted.
const defaultSettingsJSON = `{"version":1,` +
	`"league":{"auto_detect":true,"league":null},` +
	`"hotkeys":{"price_check":{"modifiers":["ctrl"],"key":"KeyD"},` +
	`"price_check_pinned":{"modifiers":["ctrl","alt"],"key":"KeyD"},` +
	`"open_settings":{"modifiers":["ctrl"],"key":"Space"},` +
	`"open_trade":{"modifiers":[],"key":""},"advanced_copy":true},` +
	`"trade":{"source":"trade","sellers":"any","widen_pct":10.0,"enable_all_stats":false,` +
	`"listing_count":20,"convert_prices":true,"remember_filters":true,"contact":null,"poesessid":null},` +
	`"overlay":{"auto_hide_ms":400,"opacity":0.95,"side":"cursor","group_by_seller":true,` +
	`"listing_columns":["seller","listed"],"scale":1.0,"backdrop_dim":0.6,` +
	`"close_pinned_on_game_focus":false,"links":{"poe2db":true,"wiki":true,"craft_of_exile":true},` +
	`"results":"below"},` +
	`"waystone":{"flags":{"Monsters fire # additional Projectiles":"danger",` +
	`"Players have #% less Recovery Rate of Life and Energy Shield":"danger",` +
	`"Players have -#% to all maximum Resistances":"danger"}},` +
	`"chat":{"commands":[` +
	`{"chord":{"modifiers":[],"key":"F5"},"text":"/hideout","send":true},` +
	`{"chord":{"modifiers":[],"key":""},"text":"/kingsmarch","send":true},` +
	`{"chord":{"modifiers":[],"key":""},"text":"/exit","send":true},` +
	`{"chord":{"modifiers":[],"key":""},"text":"@last ty&&/kick @last","send":true},` +
	`{"chord":{"modifiers":[],"key":""},"text":"/invite @last","send":true},` +
	`{"chord":{"modifiers":[],"key":""},"text":"/tradewith @last","send":true},` +
	`{"chord":{"modifiers":[],"key":""},"text":"/hideout @last","send":true}],"game_log":""},` +
	`"stash":{"searches":[],"ctrl_scroll_tabs":false},` +
	`"ui":{"language":"en","theme":"system","start_minimised":true,"check_updates":true,` +
	`"onboarding_done":false,"autostart":false},` +
	`"platform":{"backend_override":null}}`

func TestDefaultSettingsJSONMatchesSerde(t *testing.T) {
	got, err := DefaultSettings().MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != defaultSettingsJSON {
		t.Fatalf("JSON differs from serde\n got: %s\nwant: %s", got, defaultSettingsJSON)
	}
	// Through encoding/json too (the shape Wails sends). json.Marshal re-escapes `&` as
	// \u0026 (same JSON value); an Encoder without HTML escaping gives serde's exact bytes.
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(DefaultSettings()); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, strings.TrimSuffix(buf.String(), "\n"), defaultSettingsJSON)
}

func TestSettingsJSONShapeOfSetOptionsAndNilLists(t *testing.T) {
	s := DefaultSettings()
	s.League.League = new("Standard")
	s.Trade.Contact = new("me@example.com")
	s.Platform.BackendOverride = ptr(platform.BackendX11)
	s.Trade.WidenPct = 12.5
	s.Overlay.ListingColumns = nil
	s.Stash.Searches = nil
	s.Waystone.Flags = nil
	s.Hotkeys.OpenTrade = platform.Chord{}
	b, err := s.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`"league":{"auto_detect":true,"league":"Standard"}`,
		`"contact":"me@example.com","poesessid":null`,
		`"platform":{"backend_override":"x11"}`,
		`"widen_pct":12.5`,
		`"listing_columns":[]`,
		`"searches":[]`,
		`"waystone":{"flags":{}}`,
		`"open_trade":{"modifiers":[],"key":""}`,
	} {
		if !strings.Contains(string(b), want) {
			t.Errorf("missing %s in %s", want, b)
		}
	}
}

func TestSettingsJSONRoundtripAndSerdeDefaults(t *testing.T) {
	s := DefaultSettings()
	s.League.League = new("Standard")
	s.Platform.BackendOverride = ptr(platform.BackendWayland)
	s.Stash.Searches = []StashSearch{{Chord: chord("Digit1", platform.Alt), Text: "x"}}
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var back Settings
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	assertSettings(t, back, s)

	// Missing fields take defaults; null Options are None; flags replace the defaults.
	var partial Settings
	if err := json.Unmarshal([]byte(`{"trade":{"widen_pct":5,"contact":null},"waystone":{"flags":{"a.b":"good"}}}`), &partial); err != nil {
		t.Fatal(err)
	}
	want := DefaultSettings()
	want.Trade.WidenPct = 5
	want.Waystone.Flags = map[string]ModFlag{"a.b": FlagGood}
	assertSettings(t, partial, want)

	for _, bad := range []string{
		`[]`,
		`{"ui":{"theme":"blue"}}`,
		`{"overlay":{"opacity":null}}`,
		`{"overlay":{"auto_hide_ms":1.5}}`,
		`{"trade":{"listing_count":-1}}`,
		`{"chat":{"commands":[{"text":"x","send":true}]}}`,
		`{"hotkeys":{"price_check":{"key":"KeyD"}}}`,
	} {
		var s Settings
		if err := json.Unmarshal([]byte(bad), &s); err == nil {
			t.Errorf("%s: accepted", bad)
		}
	}
}

// Every field's JSON name must be its koanf (TOML) name.
func TestJSONAndKoanfTagsAgree(t *testing.T) {
	seen := map[reflect.Type]bool{}
	var walk func(reflect.Type)
	walk = func(rt reflect.Type) {
		for rt.Kind() == reflect.Pointer || rt.Kind() == reflect.Slice || rt.Kind() == reflect.Map {
			rt = rt.Elem()
		}
		if rt.Kind() != reflect.Struct || seen[rt] || rt.PkgPath() != reflect.TypeFor[Settings]().PkgPath() {
			return
		}
		seen[rt] = true
		for f := range rt.Fields() {
			j, _, _ := strings.Cut(f.Tag.Get("json"), ",")
			k, _, _ := strings.Cut(f.Tag.Get("koanf"), ",")
			if j == "" || j != k {
				t.Errorf("%s.%s: json %q, koanf %q", rt.Name(), f.Name, j, k)
			}
			walk(f.Type)
		}
	}
	walk(reflect.TypeFor[Settings]())
}

// ---------------------------------------------------------------- migrate

func TestMigrateV0FullFixture(t *testing.T) {
	doc, err := migrated(fixture(t, "v0_full.toml"))
	if err != nil {
		t.Fatal(err)
	}
	expected := table(t, `
        version = 1
        [league]
        auto_detect = false
        league = "Rise of the Abyssal"
        [hotkeys.price_check]
        modifiers = ["ctrl", "shift"]
        key = "KeyD"
        [trade]
        sellers = "any"
        [ui]
        theme = "dark"
        `)
	assertEqual(t, doc, expected)
	s, err := decodeDocument(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := Validate(&s); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, s.Hotkeys.PriceCheck, chord("KeyD", platform.Ctrl, platform.Shift))
	assertEqual(t, s.UI.Theme, ThemeDark)
}

func TestMigrateV0EmptyLeagueMeansAutoDetect(t *testing.T) {
	doc, err := migrated(fixture(t, "v0_auto_league.toml"))
	if err != nil {
		t.Fatal(err)
	}
	s, err := decodeDocument(doc)
	if err != nil {
		t.Fatal(err)
	}
	if !s.League.AutoDetect || s.League.League != nil {
		t.Fatalf("league: %+v", s.League)
	}
	assertEqual(t, s.Hotkeys.PriceCheck, chord("f5", platform.Alt))
	assertEqual(t, s.Trade.Sellers, core.StatusAny) // absent online_only keeps the default
}

func TestMigrateV0AlreadySectioned(t *testing.T) {
	doc, err := migrated(fixture(t, "v0_sectioned.toml"))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, doc["version"], any(int64(1)))
	s, err := decodeDocument(doc)
	if err != nil {
		t.Fatal(err)
	}
	if s.League.AutoDetect || s.League.League == nil || *s.League.League != "Standard" {
		t.Fatalf("league: %+v", s.League)
	}
	assertEqual(t, s.Trade.WidenPct, 25.0)
}

func TestMigrateEmptyDocumentGetsCurrentVersion(t *testing.T) {
	doc, err := migrated("")
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, doc["version"], any(int64(SettingsVersion)))
}

func TestMigrateCurrentVersionIsIdentity(t *testing.T) {
	text := "version = 1\n[ui]\nlanguage = \"de\"\n"
	doc, err := migrated(text)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, doc, table(t, text))
}

func TestMigrateRejectsNewerVersion(t *testing.T) {
	_, err := migrated("version = 2")
	var tooNew *TooNewError
	if !errors.As(err, &tooNew) || tooNew.Found != 2 {
		t.Fatalf("%v", err)
	}
	if !strings.Contains(err.Error(), "newer") {
		t.Fatal(err)
	}
}

func TestMigrateRejectsBadVersionValues(t *testing.T) {
	for _, text := range []string{"version = -1", "version = 4294967296", "version = \"1\""} {
		_, err := migrated(text)
		assertInvalid(t, err, "version")
	}
}

func TestMigrateV0RejectsMalformedLegacyKeys(t *testing.T) {
	cases := []struct{ text, field string }{
		{"league = 3", "league"},
		{"hotkey = 3", "hotkey"},
		{"hotkey = \"Ctrl+\"", "hotkey"},
		{"online_only = \"yes\"", "online_only"},
		{"online_only = true\ntrade = 1", "trade"},
	}
	for _, c := range cases {
		_, err := migrated(c.text)
		assertInvalid(t, err, c.field)
	}
}

// Each v0 key on its own, including the replace-not-merge behaviour of Table::insert.
func TestMigrateV0Steps(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"league string", `league = "Standard"`, "version = 1\n[league]\nauto_detect = false\nleague = \"Standard\"\n"},
		{"blank league", `league = "  "`, "version = 1\n[league]\nauto_detect = true\n"},
		{"sectioned league kept", "[league]\nleague = \"X\"\n", "version = 1\n[league]\nleague = \"X\"\n"},
		{"hotkey", `hotkey = "ctrl+a"`, "version = 1\n[hotkeys.price_check]\nmodifiers = [\"ctrl\"]\nkey = \"KeyA\"\n"},
		{"hotkey replaces a price_check table", "hotkey = \"F2\"\n[hotkeys]\nadvanced_copy = false\n[hotkeys.price_check]\nmodifiers = [\"alt\"]\nkey = \"KeyX\"\nextra = 1\n",
			"version = 1\n[hotkeys]\nadvanced_copy = false\n[hotkeys.price_check]\nmodifiers = []\nkey = \"F2\"\n"},
		{"online_only true", "online_only = true\n[trade]\nwiden_pct = 5.0\n", "version = 1\n[trade]\nwiden_pct = 5.0\nsellers = \"online\"\n"},
		{"online_only false", "online_only = false", "version = 1\n[trade]\nsellers = \"any\"\n"},
		{"version 0 is v0", "version = 0\nleague = \"A\"", "version = 1\n[league]\nauto_detect = false\nleague = \"A\"\n"},
		{"v1 keeps legacy-looking keys", "version = 1\nhotkey = \"F2\"", "version = 1\nhotkey = \"F2\"\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := migrated(c.in)
			if err != nil {
				t.Fatal(err)
			}
			assertEqual(t, got, table(t, c.want))
		})
	}
}

// ---------------------------------------------------------------- validate

func TestValidateRanges(t *testing.T) {
	check := func(f func(*Settings), field string) {
		t.Helper()
		s := DefaultSettings()
		f(&s)
		assertInvalid(t, Validate(&s), field)
	}
	check(func(s *Settings) { s.Trade.WidenPct = -0.1 }, "trade.widen_pct")
	check(func(s *Settings) { s.Trade.WidenPct = 100.5 }, "trade.widen_pct")
	check(func(s *Settings) { s.Trade.WidenPct = math.NaN() }, "trade.widen_pct")
	check(func(s *Settings) { s.Overlay.Opacity = 0.05 }, "overlay.opacity")
	check(func(s *Settings) { s.Overlay.Opacity = 1.01 }, "overlay.opacity")
	check(func(s *Settings) { s.Overlay.AutoHideMs = 10_001 }, "overlay.auto_hide_ms")

	s := DefaultSettings()
	s.Trade.WidenPct = 100.0
	s.Overlay.Opacity = 0.1
	s.Overlay.AutoHideMs = 10_000
	if err := Validate(&s); err != nil {
		t.Fatal(err)
	}
}

func TestValidateLeagueRequiredWithoutAutoDetect(t *testing.T) {
	s := DefaultSettings()
	s.League.AutoDetect = false
	assertInvalid(t, Validate(&s), "league.league")
	s.League.League = new("   ")
	assertInvalid(t, Validate(&s), "league.league")
	s.League.League = new("Standard")
	if err := Validate(&s); err != nil {
		t.Fatal(err)
	}
}

func TestValidateHotkeysDistinctIgnoringModifierOrder(t *testing.T) {
	s := DefaultSettings()
	s.Hotkeys.OpenSettings = chord("KeyD", platform.Alt, platform.Ctrl)
	err := Validate(&s)
	var inv *InvalidError
	if !errors.As(err, &inv) || inv.Field != "hotkeys.open_settings" || !strings.Contains(inv.Reason, "price_check_pinned") {
		t.Fatalf("%v", err)
	}
	assertEqual(t, inv.Reason, "`Alt+Ctrl+D` is already bound to hotkeys.price_check_pinned")

	s = DefaultSettings()
	s.Hotkeys.PriceCheckPinned = cloneChord(s.Hotkeys.PriceCheck)
	assertInvalid(t, Validate(&s), "hotkeys.price_check_pinned")
}

func TestValidateChordNeedsNonModifierKey(t *testing.T) {
	for _, key := range []string{"", "  ", "ShiftLeft", "control", "MetaRight"} {
		s := DefaultSettings()
		s.Hotkeys.PriceCheck = chord(key, platform.Ctrl)
		assertInvalid(t, Validate(&s), "hotkeys.price_check")
	}
}

func TestValidateContactEmail(t *testing.T) {
	for _, bad := range []string{"", "exile", "@example.com", "exile@", "exile@localhost", "a@b@c.com", "ex ile@a.com", "e@a..com"} {
		s := DefaultSettings()
		s.Trade.Contact = new(bad)
		assertInvalid(t, Validate(&s), "trade.contact")
	}
	s := DefaultSettings()
	s.Trade.Contact = new("exile.42+poe@mail.example.org")
	if err := Validate(&s); err != nil {
		t.Fatal(err)
	}
}

func TestValidateRejectsFutureVersion(t *testing.T) {
	s := DefaultSettings()
	s.Version = SettingsVersion + 1
	if _, ok := errors.AsType[*TooNewError](Validate(&s)); !ok {
		t.Fatal("future version accepted")
	}
}

func TestRejectsHeaderUnsafeSecretsAndContacts(t *testing.T) {
	s := DefaultSettings()
	s.Trade.Poesessid = new("abc;def")
	assertInvalid(t, Validate(&s), "trade.poesessid")
	s.Trade.Poesessid = new("0123abcdef")
	if err := Validate(&s); err != nil {
		t.Fatal(err)
	}
	s.Trade.Contact = new("me@example.com;x")
	assertInvalid(t, Validate(&s), "trade.contact")
	s.Trade.Contact = new("me@exa\u0007mple.com")
	if Validate(&s) == nil {
		t.Fatal("control character accepted")
	}
}

func TestSellersDefaultToAny(t *testing.T) {
	assertEqual(t, DefaultSettings().Trade.Sellers, core.StatusAny)
	s := mustFromTOML(t, "version = 1\n[trade]\nsellers = \"securable\"\n")
	assertEqual(t, s.Trade.Sellers, core.StatusSecurable)
}

func TestListingCountAndColumnsAreValidated(t *testing.T) {
	s := DefaultSettings()
	assertEqual(t, [3]any{s.Trade.ListingCount, s.Trade.ConvertPrices, s.Overlay.GroupBySeller}, [3]any{uint32(20), true, true})
	if !s.Hotkeys.OpenTrade.IsUnbound() {
		t.Fatal("open_trade bound by default")
	}
	for _, ok := range []uint32{10, 40, 100} {
		s.Trade.ListingCount = ok
		if err := Validate(&s); err != nil {
			t.Fatal(err)
		}
	}
	for _, bad := range []uint32{0, 5, 25, 110} {
		s.Trade.ListingCount = bad
		if Validate(&s) == nil {
			t.Fatalf("%d accepted", bad)
		}
	}
	s.Trade.ListingCount = 20
	s.Overlay.ListingColumns = []ListingColumn{ColumnSeller, ColumnSeller}
	if Validate(&s) == nil {
		t.Fatal("duplicate column accepted")
	}
	s.Overlay.ListingColumns = []ListingColumn{}
	if err := Validate(&s); err != nil {
		t.Fatal(err)
	}
}

func TestPanelScaleAndBackdropDimAreValidated(t *testing.T) {
	s := DefaultSettings()
	assertEqual(t, s.Overlay.Scale, 1.0)
	assertEqual(t, s.Overlay.BackdropDim, 0.6)
	if s.UI.Autostart || s.Overlay.ClosePinnedOnGameFocus {
		t.Fatal("autostart / close_pinned_on_game_focus on by default")
	}
	for _, c := range []struct {
		scale, dim float64
		ok         bool
	}{
		{0.75, 0.0, true},
		{1.5, 0.9, true},
		{0.5, 0.6, false},
		{1.6, 0.6, false},
		{1.0, 0.95, false},
		{1.0, -0.1, false},
	} {
		s.Overlay.Scale, s.Overlay.BackdropDim = c.scale, c.dim
		if got := Validate(&s) == nil; got != c.ok {
			t.Errorf("scale %v dim %v: ok = %v", c.scale, c.dim, got)
		}
	}
}

func TestWaystoneFlagsDefaultAndValidate(t *testing.T) {
	s := DefaultSettings()
	assertEqual(t, s.Waystone.Flags["Monsters fire # additional Projectiles"], FlagDanger)
	assertEqual(t, len(s.Waystone.Flags), len(DefaultDangerousWaystoneMods))
	s.Waystone.Flags["#% increased Pack Size in Map"] = FlagGood
	if err := Validate(&s); err != nil {
		t.Fatal(err)
	}
	back := mustFromTOML(t, toTOML(t, s))
	assertEqual(t, back.Waystone, s.Waystone) // round-trips through TOML
	s.Waystone.Flags["  "] = FlagWarn
	if Validate(&s) == nil {
		t.Fatal("blank flag key accepted")
	}
}

func TestChatCommandsAndStashSearchesValidate(t *testing.T) {
	s := DefaultSettings()
	assertEqual(t, s.Chat.Commands[0].Text, "/hideout")
	assertEqual(t, s.Chat.Commands[0].Chord, chord("F5"))
	if len(s.Stash.Searches) != 0 || s.Stash.CtrlScrollTabs {
		t.Fatal("stash defaults")
	}
	if err := Validate(&s); err != nil {
		t.Fatal(err)
	}
	s.Stash.Searches = append(s.Stash.Searches, StashSearch{Chord: chord("F5"), Text: `"Rarity: Rare"`})
	err := Validate(&s)
	if err == nil || !strings.Contains(err.Error(), "already bound") {
		t.Fatalf("F5 is used twice: %v", err)
	}
	s.Stash.Searches[0].Chord = chord("Digit1", platform.Alt)
	if err := Validate(&s); err != nil {
		t.Fatal(err)
	}
	s.Chat.Commands[1].Text = "a\nb"
	if Validate(&s) == nil {
		t.Fatal("one line only")
	}
	s.Chat.Commands[1].Text = "/kingsmarch"
	for i := range 20 {
		s.Chat.Commands = append(s.Chat.Commands, ChatCommand{Chord: platform.UnboundChord(), Text: fmt.Sprintf("/c%d", i), Send: true})
	}
	if Validate(&s) == nil {
		t.Fatal("too many")
	}
}
