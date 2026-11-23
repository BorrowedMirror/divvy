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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/BorrowedMirror/divvy/internal/core"
	"github.com/BorrowedMirror/divvy/internal/platform"
)

// ---------------------------------------------------------------- TOML

// The TOML written for the defaults is pinned by a golden file; a defaults file written by
// earlier releases (different layout) is checked to load as the defaults in
// TestEarlierDefaultFileLoadsAsDefaults. Regenerate with UPDATE_GOLDEN=1.
func TestDefaultSettingsTOMLSnapshot(t *testing.T) {
	got := toTOML(t, DefaultSettings())
	golden := filepath.Join("testdata", "go_default.toml")
	if os.Getenv("UPDATE_GOLDEN") != "" {
		require.NoError(t, os.WriteFile(golden, []byte(got), 0o644))
	}
	require.Equal(t, fixture(t, "go_default.toml"), got, "default TOML changed")
	assertSettings(t, mustFromTOML(t, got), DefaultSettings())
}

func TestDefaultSettingsAreValid(t *testing.T) {
	s := DefaultSettings()
	require.NoError(t, Validate(&s))
}

func TestSettingsRoundtripThroughTOML(t *testing.T) {
	s := DefaultSettings()
	s.League.AutoDetect = false
	s.League.League = new(`Hardcore "Quoted"`)
	s.Hotkeys.OpenSettings = chord("F5", platform.Meta)
	s.Trade.Source = core.SourcePoe2Scout
	s.Trade.Contact = new("exile@example.com")
	s.Trade.POESESSID = new("abc123")
	s.Overlay.Side = SideLeft
	s.UI.Theme = ThemeLight
	s.Platform.BackendOverride = new(platform.BackendWayland)
	assertSettings(t, mustFromTOML(t, toTOML(t, s)), s)
}

func TestOnboardingDoneDefaultsToFalseAndRoundtrips(t *testing.T) {
	require.False(t, DefaultSettings().UI.OnboardingDone, "onboarding_done defaults to true")
	old := mustFromTOML(t, "version = 1\n[ui]\nlanguage = \"de\"\nstart_minimised = false\n")
	require.False(t, old.UI.OnboardingDone, "old file has onboarding_done")
	assert.Equal(t, "de", old.UI.Language)
	s := DefaultSettings()
	s.UI.OnboardingDone = true
	require.True(t, mustFromTOML(t, toTOML(t, s)).UI.OnboardingDone, "onboarding_done lost")
}

func TestPartialDocumentFillsDefaults(t *testing.T) {
	s := mustFromTOML(t, "version = 1\n[overlay]\nopacity = 0.5\n")
	assert.InDelta(t, 0.5, s.Overlay.Opacity, 0)
	assert.Equal(t, DefaultSettings().Overlay.AutoHideMS, s.Overlay.AutoHideMS)
	assert.Equal(t, DefaultSettings().Trade, s.Trade)
}

func TestCheckUpdatesDefaultsOnAndSurvivesOlderFiles(t *testing.T) {
	require.True(t, DefaultSettings().UI.CheckUpdates, "check_updates defaults off")
	s := mustFromTOML(t, "version = 1\n[ui]\nlanguage = \"de\"\n")
	require.True(t, s.UI.CheckUpdates, "files written before the field existed keep updates on")
	off := mustFromTOML(t, "version = 1\n[ui]\ncheck_updates = false\n")
	require.False(t, off.UI.CheckUpdates, "check_updates = false ignored")
	require.NoError(t, Validate(&off))
}

// ---------------------------------------------------------------- documents over the defaults

func TestDocumentOverDefaults(t *testing.T) {
	t.Run("WaystoneFlagsReplaceDefaults", func(t *testing.T) {
		s := mustFromTOML(t, "[waystone.flags]\n\"Monsters fire # additional Projectiles\" = \"warn\"\n")
		assert.Equal(t, map[string]ModFlag{"Monsters fire # additional Projectiles": FlagWarn}, s.Waystone.Flags)
	})
	t.Run("EmptyWaystoneFlagsMeansNone", func(t *testing.T) {
		s := mustFromTOML(t, "[waystone.flags]\n")
		assert.Empty(t, s.Waystone.Flags)
		s = mustFromTOML(t, "[waystone]\nflags = {}\n")
		assert.Empty(t, s.Waystone.Flags)
	})
	t.Run("AcceptsFlagKeysWithDots", func(t *testing.T) {
		s := mustFromTOML(t, "[waystone.flags]\n\"1.5x Pack.Size\" = \"good\"\n")
		assert.Equal(t, map[string]ModFlag{"1.5x Pack.Size": FlagGood}, s.Waystone.Flags)
	})
	t.Run("EmptyWaystoneSectionKeepsDefaults", func(t *testing.T) {
		s := mustFromTOML(t, "[waystone]\n")
		assert.Equal(t, DefaultSettings().Waystone, s.Waystone)
	})
	t.Run("ChatCommandsReplaceDefaults", func(t *testing.T) {
		s := mustFromTOML(t, "[[chat.commands]]\ntext = \"/exit\"\nsend = false\n[chat.commands.chord]\nmodifiers = []\nkey = \"F9\"\n")
		assert.Equal(t, []ChatCommand{{Chord: chord("F9"), Text: "/exit"}}, s.Chat.Commands)
		assert.Empty(t, s.Chat.GameLog)
		s = mustFromTOML(t, "[chat]\ncommands = []\n")
		assert.Empty(t, s.Chat.Commands)
	})
	t.Run("ListingColumnsReplaceDefaults", func(t *testing.T) {
		s := mustFromTOML(t, "[overlay]\nlisting_columns = [\"stock\"]\n")
		assert.Equal(t, []ListingColumn{ColumnStock}, s.Overlay.ListingColumns)
	})
	t.Run("LinksFillPerField", func(t *testing.T) {
		s := mustFromTOML(t, "[overlay.links]\nwiki = false\n")
		assert.Equal(t, LinkSettings{Poe2db: true, Wiki: false, CraftOfExile: true}, s.Overlay.Links)
	})
	t.Run("AcceptsIntegersForFloats", func(t *testing.T) {
		s := mustFromTOML(t, "[trade]\nwiden_pct = 25\n")
		assert.InDelta(t, 25.0, s.Trade.WidenPct, 0)
	})
	t.Run("IgnoresUnknownKeys", func(t *testing.T) {
		s := mustFromTOML(t, "future = 1\n[trade]\nnew_thing = \"x\"\n")
		assertSettings(t, s, DefaultSettings())
	})

	testCases := []struct {
		name string
		have string
		err  string
	}{
		{
			"RejectsChordWithoutModifiers",
			"[hotkeys.price_check]\nkey = \"KeyD\"\n",
			"missing field `modifiers`",
		},
		{
			"RejectsChordWithoutKey",
			"[hotkeys.open_trade]\nmodifiers = []\n",
			"missing field `key`",
		},
		{
			"RejectsChatCommandWithoutSend",
			"[[chat.commands]]\ntext = \"/exit\"\nchord = { modifiers = [], key = \"\" }\n",
			"missing field `send`",
		},
		{
			"RejectsChatCommandWithoutChord",
			"[[chat.commands]]\ntext = \"/exit\"\nsend = true\n",
			"missing field `chord`",
		},
		{
			"RejectsStashSearchWithoutText",
			"[[stash.searches]]\nchord = { modifiers = [], key = \"\" }\n",
			"missing field `text`",
		},
		{
			"RejectsUnknownEnumValue",
			"[ui]\ntheme = \"blue\"\n",
			"unknown variant `blue`",
		},
		{
			"RejectsUnknownModifier",
			"[hotkeys.price_check]\nmodifiers = [\"hyper\"]\nkey = \"KeyD\"\n",
			"unknown variant `hyper`",
		},
		{
			"RejectsUnknownBackend",
			"[platform]\nbackend_override = \"beos\"\n",
			"unknown variant `beos`",
		},
		{
			"RejectsUnknownFlag",
			"[waystone.flags]\nx = \"meh\"\n",
			"unknown variant `meh`",
		},
		{
			"RejectsNegativeUint32",
			"[overlay]\nauto_hide_ms = -1\n",
			"out of range for uint32",
		},
		{
			"RejectsUint32Overflow",
			"[trade]\nlisting_count = 4294967296\n",
			"out of range for uint32",
		},
		{
			"RejectsFloatForUint32",
			"[trade]\nlisting_count = 20.0\n",
			"expected uint32",
		},
		{
			"RejectsNumberForEnum",
			"[platform]\nbackend_override = 3\n",
			"expected a string",
		},
		{
			"RejectsStringForFloat",
			"[overlay]\nopacity = \"high\"\n",
			"opacity",
		},
		{
			"RejectsStringForBool",
			"[ui]\nautostart = \"yes\"\n",
			"autostart",
		},
		{
			"RejectsIntForString",
			"[ui]\nlanguage = 3\n",
			"language",
		},
		{
			"RejectsSectionNotTable",
			"trade = 1\n",
			"trade",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := fromTOML(tc.have)
			require.ErrorContains(t, err, tc.err)
		})
	}
}

func TestEarlierDefaultFileLoadsAsDefaults(t *testing.T) {
	paths := Under(t.TempDir())
	writeSettings(t, paths, fixture(t, "v1_default.toml"))
	s, err := Load(paths)
	require.NoError(t, err)
	assertSettings(t, s, DefaultSettings())
}

func TestEarlierCustomFileLoads(t *testing.T) {
	paths := Under(t.TempDir())
	writeSettings(t, paths, fixture(t, "v1_custom.toml"))
	got, err := Load(paths)
	require.NoError(t, err)
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
			Contact: new("exile@example.com"), POESESSID: new("abc123"),
		},
		Overlay: OverlaySettings{
			AutoHideMS: 1500, Opacity: 0.8, Side: SideLast, GroupBySeller: false,
			ListingColumns: []ListingColumn{ColumnItemLevel, ColumnStock, ColumnSeller},
			Scale:          1.25, BackdropDim: 0, ClosePinnedOnGameFocus: true,
			// Not in files written before the setting existed: the default applies.
			ReadyNotice: true,
			Links:       LinkSettings{Poe2db: false, Wiki: true, CraftOfExile: false},
			Results:     ResultsAuto,
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
		Platform: PlatformSettings{BackendOverride: new(platform.BackendWayland)},
	}
	assertSettings(t, got, want)

	// What Save writes back loads to the same settings.
	require.NoError(t, Save(paths, &got))
	again, err := Load(paths)
	require.NoError(t, err)
	assertSettings(t, again, want)
}

// ---------------------------------------------------------------- JSON

// defaultSettingsJSON is the JSON the frontend gets for [DefaultSettings]: snake_case keys
// and enum values, unset options as null.
const defaultSettingsJSON = `{"version":1,` +
	`"league":{"auto_detect":true,"league":null},` +
	`"hotkeys":{"price_check":{"modifiers":["ctrl"],"key":"KeyD"},` +
	`"price_check_pinned":{"modifiers":["ctrl","alt"],"key":"KeyD"},` +
	`"open_settings":{"modifiers":["ctrl"],"key":"Space"},` +
	`"open_trade":{"modifiers":[],"key":""},"advanced_copy":true},` +
	`"trade":{"source":"trade","sellers":"any","widen_pct":10,"enable_all_stats":false,` +
	`"listing_count":20,"convert_prices":true,"remember_filters":true,"contact":null,"poesessid":null},` +
	`"overlay":{"auto_hide_ms":400,"opacity":0.95,"side":"cursor","group_by_seller":true,` +
	`"listing_columns":["seller","listed"],"scale":1,"backdrop_dim":0.6,` +
	`"close_pinned_on_game_focus":false,"ready_notice":true,"links":{"poe2db":true,"wiki":true,"craft_of_exile":true},` +
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

func TestDefaultSettingsJSON(t *testing.T) {
	got, err := DefaultSettings().MarshalJSON()
	require.NoError(t, err)
	assert.JSONEq(t, defaultSettingsJSON, string(got))
	// Through encoding/json too (the shape Wails sends).
	var buf bytes.Buffer
	require.NoError(t, json.NewEncoder(&buf).Encode(DefaultSettings()))
	assert.JSONEq(t, defaultSettingsJSON, buf.String())
}

func TestSettingsJSONShapeOfSetOptionsAndNilLists(t *testing.T) {
	s := DefaultSettings()
	s.League.League = new("Standard")
	s.Trade.Contact = new("me@example.com")
	s.Platform.BackendOverride = new(platform.BackendX11)
	s.Trade.WidenPct = 12.5
	s.Overlay.ListingColumns = nil
	s.Stash.Searches = nil
	s.Waystone.Flags = nil
	s.Hotkeys.OpenTrade = platform.Chord{}
	b, err := s.MarshalJSON()
	require.NoError(t, err)
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
		assert.Contains(t, string(b), want)
	}
}

func TestSettingsJSONRoundtripAndDefaults(t *testing.T) {
	s := DefaultSettings()
	s.League.League = new("Standard")
	s.Platform.BackendOverride = new(platform.BackendWayland)
	s.Stash.Searches = []StashSearch{{Chord: chord("Digit1", platform.Alt), Text: "x"}}
	b, err := json.Marshal(s)
	require.NoError(t, err)
	var back Settings
	require.NoError(t, json.Unmarshal(b, &back))
	assertSettings(t, back, s)

	// Missing fields take defaults; null clears an option; flags replace the defaults.
	var partial Settings
	require.NoError(t, json.Unmarshal([]byte(`{"trade":{"widen_pct":5,"contact":null},"waystone":{"flags":{"a.b":"good"}}}`), &partial))
	want := DefaultSettings()
	want.Trade.WidenPct = 5
	want.Waystone.Flags = map[string]ModFlag{"a.b": FlagGood}
	assertSettings(t, partial, want)

	for _, bad := range []string{
		`[]`,
		`{} {}`,
		`{"ui":{"theme":"blue"}}`,
		`{"overlay":{"opacity":null}}`,
		`{"overlay":{"auto_hide_ms":1.5}}`,
		`{"trade":{"listing_count":-1}}`,
		`{"chat":{"commands":[{"text":"x","send":true}]}}`,
		`{"hotkeys":{"price_check":{"key":"KeyD"}}}`,
	} {
		var s Settings
		assert.Error(t, json.Unmarshal([]byte(bad), &s), bad)
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
			assert.NotEmpty(t, j, "%s.%s", rt.Name(), f.Name)
			assert.Equal(t, j, k, "%s.%s", rt.Name(), f.Name)
			walk(f.Type)
		}
	}
	walk(reflect.TypeFor[Settings]())
}

// ---------------------------------------------------------------- migrate

func TestMigrateV0FullFixture(t *testing.T) {
	doc, err := migrated(fixture(t, "v0_full.toml"))
	require.NoError(t, err)
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
	assert.Equal(t, expected, doc)
	s, err := decodeDocument(doc)
	require.NoError(t, err)
	require.NoError(t, Validate(&s))
	assert.Equal(t, chord("KeyD", platform.Ctrl, platform.Shift), s.Hotkeys.PriceCheck)
	assert.Equal(t, ThemeDark, s.UI.Theme)
}

func TestMigrateV0EmptyLeagueMeansAutoDetect(t *testing.T) {
	doc, err := migrated(fixture(t, "v0_auto_league.toml"))
	require.NoError(t, err)
	s, err := decodeDocument(doc)
	require.NoError(t, err)
	assert.True(t, s.League.AutoDetect)
	assert.Nil(t, s.League.League)
	assert.Equal(t, chord("f5", platform.Alt), s.Hotkeys.PriceCheck)
	assert.Equal(t, core.StatusAny, s.Trade.Sellers) // absent online_only keeps the default
}

func TestMigrateV0AlreadySectioned(t *testing.T) {
	doc, err := migrated(fixture(t, "v0_sectioned.toml"))
	require.NoError(t, err)
	assert.Equal(t, any(int64(1)), doc["version"])
	s, err := decodeDocument(doc)
	require.NoError(t, err)
	assert.False(t, s.League.AutoDetect)
	assert.Equal(t, new("Standard"), s.League.League)
	assert.InDelta(t, 25.0, s.Trade.WidenPct, 0)
}

func TestMigrateEmptyDocumentGetsCurrentVersion(t *testing.T) {
	doc, err := migrated("")
	require.NoError(t, err)
	assert.Equal(t, any(int64(SettingsVersion)), doc["version"])
}

func TestMigrateCurrentVersionIsIdentity(t *testing.T) {
	text := "version = 1\n[ui]\nlanguage = \"de\"\n"
	doc, err := migrated(text)
	require.NoError(t, err)
	assert.Equal(t, table(t, text), doc)
}

func TestMigrateRejectsNewerVersion(t *testing.T) {
	_, err := migrated("version = 2")
	tooNew, ok := errors.AsType[*TooNewError](err)
	require.True(t, ok, "%v", err)
	assert.Equal(t, uint32(2), tooNew.Found)
	assert.ErrorContains(t, err, "newer")
}

func TestMigrateRejectsBadVersionValues(t *testing.T) {
	for _, text := range []string{"version = -1", "version = 4294967296", "version = \"1\""} {
		_, err := migrated(text)
		requireInvalid(t, err, "version")
	}
}

func TestMigrateV0RejectsMalformedLegacyKeys(t *testing.T) {
	testCases := []struct {
		name     string
		have     string
		expected string
	}{
		{
			"RejectsNonStringLeague",
			"league = 3",
			"league",
		},
		{
			"RejectsNonStringHotkey",
			"hotkey = 3",
			"hotkey",
		},
		{
			"RejectsHotkeyWithoutKey",
			"hotkey = \"Ctrl+\"",
			"hotkey",
		},
		{
			"RejectsNonBoolOnlineOnly",
			"online_only = \"yes\"",
			"online_only",
		},
		{
			"RejectsNonTableTrade",
			"online_only = true\ntrade = 1",
			"trade",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := migrated(tc.have)
			requireInvalid(t, err, tc.expected)
		})
	}
}

// Each v0 key on its own, including the replace-not-merge behaviour of Table::insert.
func TestMigrateV0Steps(t *testing.T) {
	testCases := []struct {
		name     string
		have     string
		expected string
	}{
		{
			"MigratesLeagueString",
			`league = "Standard"`,
			"version = 1\n[league]\nauto_detect = false\nleague = \"Standard\"\n",
		},
		{
			"BlankLeagueAutoDetects",
			`league = "  "`,
			"version = 1\n[league]\nauto_detect = true\n",
		},
		{
			"KeepsSectionedLeague",
			"[league]\nleague = \"X\"\n",
			"version = 1\n[league]\nleague = \"X\"\n",
		},
		{
			"MigratesHotkey",
			`hotkey = "ctrl+a"`,
			"version = 1\n[hotkeys.price_check]\nmodifiers = [\"ctrl\"]\nkey = \"KeyA\"\n",
		},
		{
			"HotkeyReplacesPriceCheckTable",
			"hotkey = \"F2\"\n[hotkeys]\nadvanced_copy = false\n[hotkeys.price_check]\nmodifiers = [\"alt\"]\nkey = \"KeyX\"\nextra = 1\n",
			"version = 1\n[hotkeys]\nadvanced_copy = false\n[hotkeys.price_check]\nmodifiers = []\nkey = \"F2\"\n",
		},
		{
			"MigratesOnlineOnlyTrue",
			"online_only = true\n[trade]\nwiden_pct = 5.0\n",
			"version = 1\n[trade]\nwiden_pct = 5.0\nsellers = \"online\"\n",
		},
		{
			"MigratesOnlineOnlyFalse",
			"online_only = false",
			"version = 1\n[trade]\nsellers = \"any\"\n",
		},
		{
			"TreatsVersionZeroAsV0",
			"version = 0\nleague = \"A\"",
			"version = 1\n[league]\nauto_detect = false\nleague = \"A\"\n",
		},
		{
			"V1KeepsLegacyLookingKeys",
			"version = 1\nhotkey = \"F2\"",
			"version = 1\nhotkey = \"F2\"\n",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := migrated(tc.have)
			require.NoError(t, err)
			assert.Equal(t, table(t, tc.expected), got)
		})
	}
}

// ---------------------------------------------------------------- validate

func TestValidateRanges(t *testing.T) {
	check := func(f func(*Settings), field string) {
		t.Helper()
		s := DefaultSettings()
		f(&s)
		requireInvalid(t, Validate(&s), field)
	}
	check(func(s *Settings) { s.Trade.WidenPct = -0.1 }, "trade.widen_pct")
	check(func(s *Settings) { s.Trade.WidenPct = 100.5 }, "trade.widen_pct")
	check(func(s *Settings) { s.Trade.WidenPct = math.NaN() }, "trade.widen_pct")
	check(func(s *Settings) { s.Overlay.Opacity = 0.05 }, "overlay.opacity")
	check(func(s *Settings) { s.Overlay.Opacity = 1.01 }, "overlay.opacity")
	check(func(s *Settings) { s.Overlay.AutoHideMS = 10_001 }, "overlay.auto_hide_ms")

	s := DefaultSettings()
	s.Trade.WidenPct = 100.0
	s.Overlay.Opacity = 0.1
	s.Overlay.AutoHideMS = 10_000
	require.NoError(t, Validate(&s))
}

func TestValidateLeagueRequiredWithoutAutoDetect(t *testing.T) {
	s := DefaultSettings()
	s.League.AutoDetect = false
	requireInvalid(t, Validate(&s), "league.league")
	s.League.League = new("   ")
	requireInvalid(t, Validate(&s), "league.league")
	s.League.League = new("Standard")
	require.NoError(t, Validate(&s))
}

func TestValidateHotkeysDistinctIgnoringModifierOrder(t *testing.T) {
	s := DefaultSettings()
	s.Hotkeys.OpenSettings = chord("KeyD", platform.Alt, platform.Ctrl)
	inv := requireInvalid(t, Validate(&s), "hotkeys.open_settings")
	assert.Equal(t, "`Alt+Ctrl+D` is already bound to hotkeys.price_check_pinned", inv.Reason)

	s = DefaultSettings()
	s.Hotkeys.PriceCheckPinned = cloneChord(s.Hotkeys.PriceCheck)
	requireInvalid(t, Validate(&s), "hotkeys.price_check_pinned")
}

func TestValidateChordNeedsNonModifierKey(t *testing.T) {
	for _, key := range []string{"", "  ", "ShiftLeft", "control", "MetaRight"} {
		s := DefaultSettings()
		s.Hotkeys.PriceCheck = chord(key, platform.Ctrl)
		requireInvalid(t, Validate(&s), "hotkeys.price_check")
	}
}

func TestValidateContactEmail(t *testing.T) {
	for _, bad := range []string{"", "exile", "@example.com", "exile@", "exile@localhost", "a@b@c.com", "ex ile@a.com", "e@a..com"} {
		s := DefaultSettings()
		s.Trade.Contact = new(bad)
		requireInvalid(t, Validate(&s), "trade.contact")
	}
	s := DefaultSettings()
	s.Trade.Contact = new("exile.42+poe@mail.example.org")
	require.NoError(t, Validate(&s))
}

func TestValidateRejectsFutureVersion(t *testing.T) {
	s := DefaultSettings()
	s.Version = SettingsVersion + 1
	assert.ErrorAs(t, Validate(&s), new(*TooNewError))
}

func TestRejectsHeaderUnsafeSecretsAndContacts(t *testing.T) {
	s := DefaultSettings()
	s.Trade.POESESSID = new("abc;def")
	requireInvalid(t, Validate(&s), "trade.poesessid")
	s.Trade.POESESSID = new("0123abcdef")
	require.NoError(t, Validate(&s))
	s.Trade.Contact = new("me@example.com;x")
	requireInvalid(t, Validate(&s), "trade.contact")
	s.Trade.Contact = new("me@exa\u0007mple.com")
	require.Error(t, Validate(&s), "control character accepted")
}

func TestSellersDefaultToAny(t *testing.T) {
	assert.Equal(t, core.StatusAny, DefaultSettings().Trade.Sellers)
	s := mustFromTOML(t, "version = 1\n[trade]\nsellers = \"securable\"\n")
	assert.Equal(t, core.StatusSecurable, s.Trade.Sellers)
}

func TestListingCountAndColumnsAreValidated(t *testing.T) {
	s := DefaultSettings()
	assert.Equal(t, [3]any{uint32(20), true, true}, [3]any{s.Trade.ListingCount, s.Trade.ConvertPrices, s.Overlay.GroupBySeller})
	require.True(t, s.Hotkeys.OpenTrade.IsUnbound(), "open_trade bound by default")
	for _, ok := range []uint32{10, 40, 100} {
		s.Trade.ListingCount = ok
		require.NoError(t, Validate(&s))
	}
	for _, bad := range []uint32{0, 5, 25, 110} {
		s.Trade.ListingCount = bad
		require.Error(t, Validate(&s), bad)
	}
	s.Trade.ListingCount = 20
	s.Overlay.ListingColumns = []ListingColumn{ColumnSeller, ColumnSeller}
	require.Error(t, Validate(&s), "duplicate column accepted")
	s.Overlay.ListingColumns = []ListingColumn{}
	require.NoError(t, Validate(&s))
}

func TestPanelScaleAndBackdropDimAreValidated(t *testing.T) {
	s := DefaultSettings()
	assert.InDelta(t, 1.0, s.Overlay.Scale, 0)
	assert.InDelta(t, 0.6, s.Overlay.BackdropDim, 0)
	assert.False(t, s.UI.Autostart)
	assert.False(t, s.Overlay.ClosePinnedOnGameFocus)

	testCases := []struct {
		name     string
		scale    float64
		dim      float64
		expected bool
	}{
		{
			"AcceptsMinimumScaleAndDim",
			0.75,
			0.0,
			true,
		},
		{
			"AcceptsMaximumScaleAndDim",
			1.5,
			0.9,
			true,
		},
		{
			"RejectsScaleBelowMinimum",
			0.5,
			0.6,
			false,
		},
		{
			"RejectsScaleAboveMaximum",
			1.6,
			0.6,
			false,
		},
		{
			"RejectsDimAboveMaximum",
			1.0,
			0.95,
			false,
		},
		{
			"RejectsNegativeDim",
			1.0,
			-0.1,
			false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			s.Overlay.Scale, s.Overlay.BackdropDim = tc.scale, tc.dim
			assert.Equal(t, tc.expected, Validate(&s) == nil)
		})
	}
}

func TestWaystoneFlagsDefaultAndValidate(t *testing.T) {
	s := DefaultSettings()
	assert.Equal(t, FlagDanger, s.Waystone.Flags["Monsters fire # additional Projectiles"])
	assert.Len(t, s.Waystone.Flags, len(DefaultDangerousWaystoneMods))
	s.Waystone.Flags["#% increased Pack Size in Map"] = FlagGood
	require.NoError(t, Validate(&s))
	back := mustFromTOML(t, toTOML(t, s))
	assert.Equal(t, s.Waystone, back.Waystone) // round-trips through TOML
	s.Waystone.Flags["  "] = FlagWarn
	require.Error(t, Validate(&s), "blank flag key accepted")
}

func TestChatCommandsAndStashSearchesValidate(t *testing.T) {
	s := DefaultSettings()
	assert.Equal(t, "/hideout", s.Chat.Commands[0].Text)
	assert.Equal(t, chord("F5"), s.Chat.Commands[0].Chord)
	assert.Empty(t, s.Stash.Searches)
	assert.False(t, s.Stash.CtrlScrollTabs)
	require.NoError(t, Validate(&s))
	s.Stash.Searches = append(s.Stash.Searches, StashSearch{Chord: chord("F5"), Text: `"Rarity: Rare"`})
	require.ErrorContains(t, Validate(&s), "already bound", "F5 is used twice")
	s.Stash.Searches[0].Chord = chord("Digit1", platform.Alt)
	require.NoError(t, Validate(&s))
	s.Chat.Commands[1].Text = "a\nb"
	require.Error(t, Validate(&s), "one line only")
	s.Chat.Commands[1].Text = "/kingsmarch"
	for i := range 20 {
		s.Chat.Commands = append(s.Chat.Commands, ChatCommand{Chord: platform.UnboundChord(), Text: fmt.Sprintf("/c%d", i), Send: true})
	}
	require.Error(t, Validate(&s), "too many")
}

func TestOneMigrationStepPerVersion(t *testing.T) {
	assert.Len(t, migrations, int(SettingsVersion))
}
