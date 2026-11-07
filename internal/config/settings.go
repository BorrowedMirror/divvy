// Package config is the settings schema (`settings.toml`), its migrations and validation,
// and the per-OS paths RKL keeps its files under. It is a port of the Rust crate
// `rkl-config`: the TOML keys and the JSON sent to the frontend match the Rust serde
// definitions, so files written by the Rust app load unchanged.
package config

import (
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"slices"

	"github.com/BorrowedMirror/rust-king-ledger/internal/core"
	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
)

// SettingsVersion is the current schema version. Increment it and add a step to
// [migrations] on any breaking change.
const SettingsVersion uint32 = 1

// MaxMacros is the most chat commands / stash searches that can be configured.
const MaxMacros = 20

// Every field carries the same `json` and `koanf` name: the JSON goes to the frontend, the
// koanf name is the TOML key. Both are the Rust field names.

// Settings is the whole settings document. Missing sections and fields take their
// defaults (Rust `#[serde(default)]`).
type Settings struct {
	Version  uint32           `json:"version" koanf:"version"`
	League   LeagueSettings   `json:"league" koanf:"league"`
	Hotkeys  HotkeySettings   `json:"hotkeys" koanf:"hotkeys"`
	Trade    TradeSettings    `json:"trade" koanf:"trade"`
	Overlay  OverlaySettings  `json:"overlay" koanf:"overlay"`
	Waystone WaystoneSettings `json:"waystone" koanf:"waystone"`
	Chat     ChatSettings     `json:"chat" koanf:"chat"`
	Stash    StashSettings    `json:"stash" koanf:"stash"`
	UI       UISettings       `json:"ui" koanf:"ui"`
	Platform PlatformSettings `json:"platform" koanf:"platform"`
}

// LeagueSettings picks the trade league.
type LeagueSettings struct {
	// AutoDetect picks the current softcore trade league automatically.
	AutoDetect bool `json:"auto_detect" koanf:"auto_detect"`
	// League is the league id used when AutoDetect is false (or as last-known value).
	League *string `json:"league" koanf:"league,omitempty"`
}

// HotkeySettings are the global hotkeys.
type HotkeySettings struct {
	PriceCheck       platform.Chord `json:"price_check" koanf:"price_check"`
	PriceCheckPinned platform.Chord `json:"price_check_pinned" koanf:"price_check_pinned"`
	OpenSettings     platform.Chord `json:"open_settings" koanf:"open_settings"`
	// OpenTrade opens the last price check's search on the trade site (unset by default).
	OpenTrade platform.Chord `json:"open_trade" koanf:"open_trade"`
	// AdvancedCopy uses Ctrl+Alt+C (advanced item text with tiers) instead of Ctrl+C.
	AdvancedCopy bool `json:"advanced_copy" koanf:"advanced_copy"`
}

// TradeSettings configure trade-site searches.
type TradeSettings struct {
	Source core.PriceSourceKind `json:"source" koanf:"source"`
	// Sellers is the default seller filter for new searches: instant buyout, online, or any
	// (default).
	Sellers        core.OnlineStatus `json:"sellers" koanf:"sellers"`
	WidenPct       float64           `json:"widen_pct" koanf:"widen_pct"`
	EnableAllStats bool              `json:"enable_all_stats" koanf:"enable_all_stats"`
	// ListingCount is the listings fetched per trade-site search (10 to 100, in steps of 10).
	ListingCount uint32 `json:"listing_count" koanf:"listing_count"`
	// ConvertPrices fetches currency rates (from poe2scout) to compare listings in other
	// currencies and show each price's divine or exalted equivalent.
	ConvertPrices bool `json:"convert_prices" koanf:"convert_prices"`
	// RememberFilters starts each item with the stats the user last had on or off for its
	// category.
	RememberFilters bool `json:"remember_filters" koanf:"remember_filters"`
	// Contact is an optional e-mail appended to the User-Agent, as GGG requests.
	Contact *string `json:"contact" koanf:"contact,omitempty"`
	// Poesessid is an optional POESESSID; stored in plain text, so off by default.
	Poesessid *string `json:"poesessid" koanf:"poesessid,omitempty"`
}

// OverlaySettings configure the price panel.
type OverlaySettings struct {
	// AutoHideMs auto-hides after the mouse leaves (ms); pinned panels ignore this.
	AutoHideMs uint32      `json:"auto_hide_ms" koanf:"auto_hide_ms"`
	Opacity    float64     `json:"opacity" koanf:"opacity"`
	Side       OverlaySide `json:"side" koanf:"side"`
	// GroupBySeller shows one row per seller (their cheapest listing, with a count).
	GroupBySeller bool `json:"group_by_seller" koanf:"group_by_seller"`
	// ListingColumns are the listing table columns after the price, in order.
	ListingColumns []ListingColumn `json:"listing_columns" koanf:"listing_columns"`
	// Scale is the size of the price panel (0.75 to 1.5; 1 = normal).
	Scale float64 `json:"scale" koanf:"scale"`
	// BackdropDim is how dark the backdrop behind the unpinned panel is (0 = none, 0.9 =
	// nearly black).
	BackdropDim float64 `json:"backdrop_dim" koanf:"backdrop_dim"`
	// ClosePinnedOnGameFocus closes a pinned panel as soon as the game window gets focus
	// again.
	ClosePinnedOnGameFocus bool `json:"close_pinned_on_game_focus" koanf:"close_pinned_on_game_focus"`
	// Links says which lookup links the panel shows under the item name.
	Links LinkSettings `json:"links" koanf:"links"`
	// Results says where prices and listings go: below the filters, or in a column beside
	// them.
	Results ResultsLayout `json:"results" koanf:"results"`
}

// ResultsLayout is the placement of the results (prices and listings) in the price panel.
type ResultsLayout string

const (
	// ResultsBelow is under the Search button (one narrow column).
	ResultsBelow ResultsLayout = "below"
	// ResultsLeft is a column to the left of the filters.
	ResultsLeft ResultsLayout = "left"
	// ResultsRight is a column to the right of the filters.
	ResultsRight ResultsLayout = "right"
	// ResultsAuto is beside the filters (right) for items with many stats, below otherwise.
	ResultsAuto ResultsLayout = "auto"
)

// LinkSettings are the lookup links shown in the price panel.
type LinkSettings struct {
	Poe2db       bool `json:"poe2db" koanf:"poe2db"`
	Wiki         bool `json:"wiki" koanf:"wiki"`
	CraftOfExile bool `json:"craft_of_exile" koanf:"craft_of_exile"`
}

// ListingColumn is an optional column of the listing table (the price column is always
// shown).
type ListingColumn string

const (
	ColumnSeller    ListingColumn = "seller"
	ColumnItemLevel ListingColumn = "item_level"
	ColumnStock     ListingColumn = "stock"
	ColumnListed    ListingColumn = "listed"
)

// OverlaySide is where the price panel opens.
type OverlaySide string

const (
	// SideCursor is beside the cursor, flipping when near the screen edge.
	SideCursor OverlaySide = "cursor"
	SideLeft   OverlaySide = "left"
	SideRight  OverlaySide = "right"
	// SideLast is where the user last dragged the panel (beside the cursor until it has been
	// moved).
	SideLast OverlaySide = "last"
)

// ChatCommand is a chat command sent with one hotkey: Enter, the text, and (with Send)
// Enter again. `&&` chains several lines in one key press; `@last` and `@area` are filled
// from the game log. All fields are required in the TOML (no serde default).
type ChatCommand struct {
	Chord platform.Chord `json:"chord" koanf:"chord"`
	Text  string         `json:"text" koanf:"text"`
	// Send presses Enter after the text (off: leave it in the chat box, e.g. `@last `).
	Send bool `json:"send" koanf:"send"`
}

// ChatSettings are the chat command hotkeys.
type ChatSettings struct {
	Commands []ChatCommand `json:"commands" koanf:"commands"`
	// GameLog is the game's `Client.txt`, read for `@last` and `@area`. Empty: found
	// automatically.
	GameLog string `json:"game_log" koanf:"game_log"`
}

// StashSearch is a saved stash search: the hotkey fills the stash (or vendor) search box
// with Text. All fields are required in the TOML (no serde default).
type StashSearch struct {
	Chord platform.Chord `json:"chord" koanf:"chord"`
	Text  string         `json:"text" koanf:"text"`
}

// StashSettings are the stash search hotkeys.
type StashSettings struct {
	Searches []StashSearch `json:"searches" koanf:"searches"`
	// CtrlScrollTabs makes Ctrl + mouse wheel over the game switch stash tabs (one arrow key
	// per wheel step).
	CtrlScrollTabs bool `json:"ctrl_scroll_tabs" koanf:"ctrl_scroll_tabs"`
}

// ModFlag is how a waystone mod is flagged in the panel.
type ModFlag string

const (
	FlagDanger ModFlag = "danger"
	FlagWarn   ModFlag = "warn"
	FlagGood   ModFlag = "good"
)

// WaystoneSettings is the waystone mod check: flags per mod, keyed by the mod's template as
// the game copies it (rolled numbers as `#`, e.g. `Monsters fire # additional Projectiles`).
// Serialised with sorted keys, like Rust's BTreeMap.
type WaystoneSettings struct {
	Flags map[string]ModFlag `json:"flags" koanf:"flags"`
}

// DefaultDangerousWaystoneMods are the mods flagged dangerous out of the box (and by the
// "Common dangers" preset).
var DefaultDangerousWaystoneMods = []string{
	"Players have -#% to all maximum Resistances",
	"Players have #% less Recovery Rate of Life and Energy Shield",
	"Monsters fire # additional Projectiles",
}

// UISettings are general app settings.
type UISettings struct {
	// Language is a BCP-47 tag, e.g. `en`.
	Language       string `json:"language" koanf:"language"`
	Theme          Theme  `json:"theme" koanf:"theme"`
	StartMinimised bool   `json:"start_minimised" koanf:"start_minimised"`
	// CheckUpdates checks for and offers app updates in the background (release builds with
	// an updater key only).
	CheckUpdates bool `json:"check_updates" koanf:"check_updates"`
	// OnboardingDone says first-run onboarding (permissions / platform notes) was completed
	// or dismissed.
	OnboardingDone bool `json:"onboarding_done" koanf:"onboarding_done"`
	// Autostart starts RKL when the user logs in.
	Autostart bool `json:"autostart" koanf:"autostart"`
}

// Theme is the UI colour scheme.
type Theme string

const (
	ThemeSystem Theme = "system"
	ThemeDark   Theme = "dark"
	ThemeLight  Theme = "light"
)

// PlatformSettings override OS integration.
type PlatformSettings struct {
	BackendOverride *platform.BackendKind `json:"backend_override" koanf:"backend_override,omitempty"`
}

// DefaultSettings returns the out-of-the-box settings (Rust `Settings::default()`).
func DefaultSettings() Settings {
	cmd := func(chord platform.Chord, text string) ChatCommand {
		return ChatCommand{Chord: chord, Text: text, Send: true}
	}
	flags := make(map[string]ModFlag, len(DefaultDangerousWaystoneMods))
	for _, m := range DefaultDangerousWaystoneMods {
		flags[m] = FlagDanger
	}
	return Settings{
		Version: SettingsVersion,
		League:  LeagueSettings{AutoDetect: true},
		Hotkeys: HotkeySettings{
			PriceCheck:       platform.NewChord("KeyD", platform.Ctrl),
			PriceCheckPinned: platform.NewChord("KeyD", platform.Ctrl, platform.Alt),
			OpenSettings:     platform.NewChord("Space", platform.Ctrl),
			OpenTrade:        platform.UnboundChord(),
			AdvancedCopy:     true,
		},
		Trade: TradeSettings{
			Source:          core.SourceTrade,
			Sellers:         core.StatusAny,
			WidenPct:        10.0,
			ListingCount:    20,
			ConvertPrices:   true,
			RememberFilters: true,
		},
		Overlay: OverlaySettings{
			AutoHideMs:     400,
			Opacity:        0.95,
			Side:           SideCursor,
			GroupBySeller:  true,
			ListingColumns: []ListingColumn{ColumnSeller, ColumnListed},
			Scale:          1.0,
			BackdropDim:    0.6,
			Links:          LinkSettings{Poe2db: true, Wiki: true, CraftOfExile: true},
			Results:        ResultsBelow,
		},
		Waystone: WaystoneSettings{Flags: flags},
		Chat: ChatSettings{
			Commands: []ChatCommand{
				cmd(platform.NewChord("F5"), "/hideout"),
				cmd(platform.UnboundChord(), "/kingsmarch"),
				cmd(platform.UnboundChord(), "/exit"),
				cmd(platform.UnboundChord(), "@last ty&&/kick @last"),
				cmd(platform.UnboundChord(), "/invite @last"),
				cmd(platform.UnboundChord(), "/tradewith @last"),
				cmd(platform.UnboundChord(), "/hideout @last"),
			},
		},
		Stash: StashSettings{Searches: []StashSearch{}},
		UI: UISettings{
			Language:       "en",
			Theme:          ThemeSystem,
			StartMinimised: true,
			CheckUpdates:   true,
		},
	}
}

// Clone returns a deep copy (Rust `Clone`).
func (s Settings) Clone() Settings {
	c := s
	c.League.League = clonePtr(s.League.League)
	c.Hotkeys.PriceCheck = cloneChord(s.Hotkeys.PriceCheck)
	c.Hotkeys.PriceCheckPinned = cloneChord(s.Hotkeys.PriceCheckPinned)
	c.Hotkeys.OpenSettings = cloneChord(s.Hotkeys.OpenSettings)
	c.Hotkeys.OpenTrade = cloneChord(s.Hotkeys.OpenTrade)
	c.Trade.Contact = clonePtr(s.Trade.Contact)
	c.Trade.Poesessid = clonePtr(s.Trade.Poesessid)
	c.Overlay.ListingColumns = slices.Clone(s.Overlay.ListingColumns)
	c.Waystone.Flags = maps.Clone(s.Waystone.Flags)
	c.Chat.Commands = slices.Clone(s.Chat.Commands)
	for i := range c.Chat.Commands {
		c.Chat.Commands[i].Chord = cloneChord(c.Chat.Commands[i].Chord)
	}
	c.Stash.Searches = slices.Clone(s.Stash.Searches)
	for i := range c.Stash.Searches {
		c.Stash.Searches[i].Chord = cloneChord(c.Stash.Searches[i].Chord)
	}
	c.Platform.BackendOverride = clonePtr(s.Platform.BackendOverride)
	return c
}

func clonePtr[T any](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

func cloneChord(c platform.Chord) platform.Chord {
	return platform.Chord{Modifiers: slices.Clone(c.Modifiers), Key: c.Key}
}

// MarshalJSON writes the settings exactly as serde_json writes the Rust struct: fields in
// declaration order, None as null, floats with a fraction (`10.0`), empty lists as `[]`,
// map keys sorted.
func (s Settings) MarshalJSON() ([]byte, error) {
	type plain Settings
	return core.MarshalSerde(plain(s))
}

// UnmarshalJSON reads settings the way serde_json reads the Rust struct: missing fields
// take their defaults, unknown fields are ignored, and wrong types, unknown enum variants
// and missing required fields (chat command / stash search / chord fields) are errors.
// Migrations and validation do not run (callers validate, as in Rust).
func (s *Settings) UnmarshalJSON(b []byte) error {
	doc, err := decodeJSONDocument(b)
	if err != nil {
		return err
	}
	out, err := decodeDocument(doc)
	if err != nil {
		return err
	}
	*s = out
	return nil
}

// enumValues lists the serde variant names of every string enum in the schema; decoding
// rejects anything else, as serde does.
var enumValues = map[reflect.Type][]string{}

func registerEnum[T ~string](variants ...T) {
	names := make([]string, len(variants))
	for i, v := range variants {
		names[i] = string(v)
	}
	enumValues[reflect.TypeFor[T]()] = names
}

func init() {
	registerEnum(ResultsBelow, ResultsLeft, ResultsRight, ResultsAuto)
	registerEnum(ColumnSeller, ColumnItemLevel, ColumnStock, ColumnListed)
	registerEnum(SideCursor, SideLeft, SideRight, SideLast)
	registerEnum(FlagDanger, FlagWarn, FlagGood)
	registerEnum(ThemeSystem, ThemeDark, ThemeLight)
	registerEnum(core.SourceTrade, core.SourcePoeNinja, core.SourcePoe2Scout)
	registerEnum(core.StatusSecurable, core.StatusOnline, core.StatusAny)
	registerEnum(platform.Ctrl, platform.Alt, platform.Shift, platform.Meta)
	registerEnum(platform.BackendWindows, platform.BackendMacos, platform.BackendX11,
		platform.BackendWayland, platform.BackendFake)
}

func unmarshalEnum[T ~string](b []byte, dst *T) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	if err := checkEnum(reflect.TypeFor[T](), s); err != nil {
		return err
	}
	*dst = T(s)
	return nil
}

// UnmarshalJSON accepts only the serde variant names.
func (v *ResultsLayout) UnmarshalJSON(b []byte) error { return unmarshalEnum(b, v) }

// UnmarshalJSON accepts only the serde variant names.
func (v *ListingColumn) UnmarshalJSON(b []byte) error { return unmarshalEnum(b, v) }

// UnmarshalJSON accepts only the serde variant names.
func (v *OverlaySide) UnmarshalJSON(b []byte) error { return unmarshalEnum(b, v) }

// UnmarshalJSON accepts only the serde variant names.
func (v *ModFlag) UnmarshalJSON(b []byte) error { return unmarshalEnum(b, v) }

// UnmarshalJSON accepts only the serde variant names.
func (v *Theme) UnmarshalJSON(b []byte) error { return unmarshalEnum(b, v) }

func checkEnum(key reflect.Type, s string) error {
	names := enumValues[key]
	if slices.Contains(names, s) {
		return nil
	}
	return fmt.Errorf("unknown variant `%s`, expected one of %s", s, quoteList(names))
}

func quoteList(names []string) string {
	out := ""
	for i, n := range names {
		if i > 0 {
			out += ", "
		}
		out += "`" + n + "`"
	}
	return out
}
