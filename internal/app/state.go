package app

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/BorrowedMirror/rust-king-ledger/internal/config"
	"github.com/BorrowedMirror/rust-king-ledger/internal/core"
	"github.com/BorrowedMirror/rust-king-ledger/internal/data"
	"github.com/BorrowedMirror/rust-king-ledger/internal/ipc"
	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
	"github.com/BorrowedMirror/rust-king-ledger/internal/trade"
)

const maxErrors = 50

// Sources are the network price sources built from settings.
type Sources struct {
	Trade *trade.TradeClient
	Ninja *trade.PoeNinjaSource
	Scout *trade.Poe2ScoutSource
}

// EnvOverride is an endpoint override from the environment variable key (e2e tests, mock
// servers). Only test-hook builds honour it, and only for https or loopback http URLs, so a
// release build can't be pointed at another host.
func EnvOverride(key string) (*url.URL, bool) {
	if !testHookBuild {
		return nil, false
	}
	raw := os.Getenv(key)
	if raw == "" {
		return nil, false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, false
	}
	if !IsSafeOverride(u) {
		slog.Warn("ignoring endpoint override: only https or loopback http URLs are allowed", "var", key)
		return nil, false
	}
	return u, true
}

func envURL(key, fallback string) *url.URL {
	if u, ok := EnvOverride(key); ok {
		return u
	}
	u, err := url.Parse(fallback)
	if err != nil {
		panic(err) // fallback is a literal
	}
	return u
}

func tradeConfig(s *config.Settings) trade.TradeConfig {
	cfg := trade.DefaultTradeConfig()
	cfg.BaseURL = envURL("RKL_TRADE_BASE_URL", "https://www.pathofexile.com")
	cfg.UserAgent = UserAgent(s)
	if s.Trade.Poesessid != nil {
		cfg.POESESSID = *s.Trade.Poesessid
	}
	cfg.FetchCount = int(min(max(s.Trade.ListingCount, 10), 100))
	return cfg
}

func ninjaSource(ua string) *trade.PoeNinjaSource {
	return trade.NewPoeNinjaSource(envURL("RKL_NINJA_BASE_URL", "https://poe.ninja"), ua)
}

func scoutSource(ua string) *trade.Poe2ScoutSource {
	return trade.NewPoe2ScoutSource(envURL("RKL_SCOUT_BASE_URL", "https://poe2scout.com"), ua)
}

// SourcesFromSettings builds every price source for s.
func SourcesFromSettings(s *config.Settings) (*Sources, error) {
	cfg := tradeConfig(s)
	client, err := trade.NewTradeClient(cfg)
	if err != nil {
		return nil, err
	}
	return &Sources{Trade: client, Ninja: ninjaSource(cfg.UserAgent), Scout: scoutSource(cfg.UserAgent)}, nil
}

// Updated returns sources for new settings, reusing every client whose inputs are unchanged so
// a save keeps the trade rate-limiter state and the price caches. nil means nothing to rebuild.
func (src *Sources) Updated(s *config.Settings) (*Sources, error) {
	cfg := tradeConfig(s)
	old := src.Trade.Config()
	uaChanged := old.UserAgent != cfg.UserAgent
	if !uaChanged && old.BaseURL.String() == cfg.BaseURL.String() && old.POESESSID == cfg.POESESSID {
		return nil, nil
	}
	client, err := trade.NewTradeClient(cfg)
	if err != nil {
		return nil, err
	}
	next := &Sources{Trade: client, Ninja: src.Ninja, Scout: src.Scout}
	if uaChanged {
		next.Ninja, next.Scout = ninjaSource(cfg.UserAgent), scoutSource(cfg.UserAgent)
	}
	return next, nil
}

// Pick returns the configured source if it supports the item, else the official trade API.
func (src *Sources) Pick(kind core.PriceSourceKind, item *core.ParsedItem) trade.PriceSource {
	var preferred trade.PriceSource = src.Trade
	switch kind {
	case core.SourcePoeNinja:
		preferred = src.Ninja
	case core.SourcePoe2Scout:
		preferred = src.Scout
	}
	if preferred.Supports(item) {
		return preferred
	}
	return src.Trade
}

// CardWidth is the price card's width (logical px), matching the overlay window's size.
const CardWidth = 420.0

// CardHeight is the card height before the web view has measured it (logical px).
const CardHeight = 560.0

// PositionFile is the cache file holding the last dragged panel position (`{"x":..,"y":..}`).
const PositionFile = "panel-position.json"

type savedPosition struct {
	X int32 `json:"x"`
	Y int32 `json:"y"`
}

// LoadPosition reads the last dragged panel position; ok is false when there is none or the
// file is unreadable.
func LoadPosition(path string) (x, y int32, ok bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, 0, false
	}
	var p savedPosition
	if json.Unmarshal(b, &p) != nil {
		return 0, 0, false
	}
	return p.X, p.Y, true
}

// SavePosition writes the panel position.
func SavePosition(path string, x, y int32) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.Marshal(savedPosition{X: x, Y: y})
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// Point is a screen position in physical pixels.
type Point struct{ X, Y int32 }

// dragOrigin is the cursor and card position (physical px) when a header drag started.
type dragOrigin struct {
	CursorX, CursorY float64
	Card             Point
}

// State is the long-lived app state shared by commands, the hotkey loop, the tray and the
// test hook.
type State struct {
	Paths    *config.Paths
	Platform *platform.Platform
	Backend  platform.BackendKind

	settingsMu sync.RWMutex
	settings   config.Settings

	data     func() (*data.GameData, error) // decodes once, then returns the cached result
	dataDone atomic.Bool

	sourcesMu sync.RWMutex
	sources   *Sources

	mu            sync.Mutex // guards the fields below
	lastItem      *ipc.PriceCheckResponse
	lastItemText  *string // the last item's text exactly as the game copied it
	lastPrice     *core.PriceSummary
	leagues       []trade.League
	leaguesSet    bool
	errors        []string
	filterMemory  FilterMemory
	lastPosition  *Point // where the user last dropped the card, for `Position: Last`
	drag          *dragOrigin
	overlayLayout ipc.OverlayLayout
	card          Point   // card position on screen (physical px)
	cardHeight    float64 // last measured card height (logical px)
	cardCSSWidth  float64 // 420, or wider with results beside the filters

	Pinned          atomic.Bool
	OverlayVisible  atomic.Bool
	SettingsOverlay atomic.Bool // settings shown as an overlay covering the game window
	CardMoved       atomic.Bool // the user dragged the card since it was last shown
	ShowSeq         atomic.Uint64
	Quitting        atomic.Bool
	CloseQuits      atomic.Bool // closing settings quits (no tray to quit from)
	FollowFocus     atomic.Bool // hotkeys are grabbed only while the game has focus

	MacroLock sync.Mutex // serialises chat commands and stash searches (they borrow the clipboard)
	ApplyLock sync.Mutex // held for a whole settings apply
	FlowGate  *FlowGate  // serialises copy flows (hotkey, signal, tray, test hook, IPC)

	LogPath string // set once the log file is open
}

// NewState builds the state. gameData decodes the bundled data on first call (and caches it);
// pass data.Bundled.
func NewState(paths *config.Paths, settings config.Settings, plat *platform.Platform, backend platform.BackendKind, gameData func() (*data.GameData, error)) (*State, error) {
	sources, err := SourcesFromSettings(&settings)
	if err != nil {
		return nil, err
	}
	st := &State{
		Paths:         paths,
		Platform:      plat,
		Backend:       backend,
		settings:      settings,
		data:          gameData,
		sources:       sources,
		cardHeight:    CardHeight,
		cardCSSWidth:  CardWidth,
		overlayLayout: ipc.OverlayLayout{},
		FlowGate:      NewFlowGate(FlowGateDebounce),
	}
	if paths != nil {
		if x, y, ok := LoadPosition(filepath.Join(paths.CacheDir, PositionFile)); ok {
			st.lastPosition = &Point{x, y}
		}
		st.filterMemory = LoadFilterMemory(filepath.Join(paths.CacheDir, FilterMemoryFileName))
	}
	return st, nil
}

// Settings returns a copy of the current settings.
func (s *State) Settings() config.Settings {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	return s.settings.Clone()
}

// SetSettings swaps in new settings.
func (s *State) SetSettings(next config.Settings) {
	s.settingsMu.Lock()
	s.settings = next
	s.settingsMu.Unlock()
}

// Sources returns the current price sources.
func (s *State) Sources() *Sources {
	s.sourcesMu.RLock()
	defer s.sourcesMu.RUnlock()
	return s.sources
}

// SetSources swaps in new price sources.
func (s *State) SetSources(next *Sources) {
	s.sourcesMu.Lock()
	s.sources = next
	s.sourcesMu.Unlock()
}

// Capabilities is what the platform backend can do here.
func (s *State) Capabilities() platform.Capabilities {
	if s.Platform == nil {
		return platform.Capabilities{}
	}
	return s.Platform.Capabilities
}

// CapabilitiesWithNotes adds problems the hotkey host found after registration (diagnostics).
func (s *State) CapabilitiesWithNotes() platform.Capabilities {
	caps := s.Capabilities()
	caps.Notes = append([]string{}, caps.Notes...)
	if s.Platform != nil && s.Platform.Hotkeys != nil {
		caps.Notes = append(caps.Notes, s.Platform.Hotkeys.Notes()...)
	}
	return caps
}

// RecordError logs an error and keeps it for the diagnostics page (the last 50).
func (s *State) RecordError(err any) {
	msg := fmt.Sprint(err)
	slog.Warn(msg)
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.errors) == maxErrors {
		s.errors = s.errors[1:]
	}
	s.errors = append(s.errors, msg)
}

// RecentErrors returns the kept errors, oldest first.
func (s *State) RecentErrors() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string{}, s.errors...)
}

// League is the league to search in: the configured one, else the first live trade league.
func (s *State) League() string {
	settings := s.Settings()
	s.mu.Lock()
	leagues := s.leagues
	s.mu.Unlock()
	return ResolveLeague(&settings, leagues)
}

// SetLeagues stores the live league list.
func (s *State) SetLeagues(leagues []trade.League) {
	s.mu.Lock()
	s.leagues, s.leaguesSet = leagues, true
	s.mu.Unlock()
}

// SetLeaguesIfUnset stores the auto-detected league list unless a newer list_leagues result
// is already there.
func (s *State) SetLeaguesIfUnset(leagues []trade.League) {
	s.mu.Lock()
	if !s.leaguesSet {
		s.leagues, s.leaguesSet = leagues, true
	}
	s.mu.Unlock()
}

// Data is the game data if decoding has finished (diagnostics; never blocks).
func (s *State) Data() *data.GameData {
	if !s.dataDone.Load() {
		return nil
	}
	d, _ := s.data()
	return d
}

// WaitData is the game data, waiting for the decode to finish (item parsing needs the
// catalog). nil when decoding failed.
func (s *State) WaitData() *data.GameData {
	d, err := s.data()
	s.dataDone.Store(true)
	if err != nil {
		return nil
	}
	return d
}

// DecodeData starts decoding the game data in the background (startup).
func (s *State) DecodeData() {
	go func() {
		if s.WaitData() == nil {
			_, err := s.data()
			s.RecordError(fmt.Sprintf("game data unavailable: %v", err))
		}
	}()
}

// IsPinned reports whether the price panel is pinned.
func (s *State) IsPinned() bool { return s.Pinned.Load() }

// LastItem returns the last copied item and its filters.
func (s *State) LastItem() (*ipc.PriceCheckResponse, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lastItem == nil {
		return nil, false
	}
	c := *s.lastItem
	return &c, true
}

// SetLastItem stores the last copied item, its filters and its text as the game copied it.
func (s *State) SetLastItem(item core.ParsedItem, filters core.FilterSet, text *string) {
	s.mu.Lock()
	s.lastItem = &ipc.PriceCheckResponse{Item: item, Filters: filters}
	s.lastItemText = text
	s.mu.Unlock()
}

// LastItemText is the last item's text exactly as the game copied it.
func (s *State) LastItemText() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lastItemText == nil {
		return ""
	}
	return *s.lastItemText
}

// SetLastPrice stores the last price summary.
func (s *State) SetLastPrice(p core.PriceSummary) {
	s.mu.Lock()
	s.lastPrice = &p
	s.mu.Unlock()
}

// RememberFilters records the stat choices of a search the user ran (best effort; a failed
// save only loses them).
func (s *State) RememberFilters(filters *core.FilterSet) {
	if s.Paths == nil {
		return
	}
	s.mu.Lock()
	changed := s.filterMemory.Record(filters)
	mem := s.filterMemory
	s.mu.Unlock()
	if changed {
		if err := mem.Save(filepath.Join(s.Paths.CacheDir, FilterMemoryFileName)); err != nil {
			s.RecordError(fmt.Sprintf("save remembered filters: %v", err))
		}
	}
}

// ApplyFilterMemory turns remembered stats on or off in new filters.
func (s *State) ApplyFilterMemory(filters *core.FilterSet) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.filterMemory.Apply(filters)
}

// ClearFilterMemory forgets every remembered stat choice and deletes the file.
func (s *State) ClearFilterMemory() error {
	s.mu.Lock()
	s.filterMemory = FilterMemory{}
	s.mu.Unlock()
	if s.Paths == nil {
		return nil
	}
	err := os.Remove(filepath.Join(s.Paths.CacheDir, FilterMemoryFileName))
	if err != nil && !os.IsNotExist(err) {
		return ipc.NewError("config", err.Error())
	}
	return nil
}

// OverlayLayout is the current price panel layout.
func (s *State) OverlayLayout() ipc.OverlayLayout {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.overlayLayout
}

// SetOverlayLayout stores the price panel layout.
func (s *State) SetOverlayLayout(l ipc.OverlayLayout) {
	s.mu.Lock()
	s.overlayLayout = l
	s.mu.Unlock()
}

// Card returns the card position (physical px) and its last measured height (logical px).
func (s *State) Card() (Point, float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.card, s.cardHeight
}

// SetCardPosition stores the card position.
func (s *State) SetCardPosition(p Point) {
	s.mu.Lock()
	s.card = p
	s.mu.Unlock()
}

// SetCardHeight stores the card's measured height.
func (s *State) SetCardHeight(h float64) {
	s.mu.Lock()
	s.cardHeight = h
	s.mu.Unlock()
}

// CardCSSWidth is the card width in CSS px.
func (s *State) CardCSSWidth() float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cardCSSWidth
}

// SetCardCSSWidth stores the card width in CSS px.
func (s *State) SetCardCSSWidth(w float64) {
	s.mu.Lock()
	s.cardCSSWidth = w
	s.mu.Unlock()
}

// LastPosition is where the user last dropped the card.
func (s *State) LastPosition() (Point, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lastPosition == nil {
		return Point{}, false
	}
	return *s.lastPosition, true
}

// SetLastPosition remembers where the user dropped the card and saves it.
func (s *State) SetLastPosition(p Point) {
	s.mu.Lock()
	s.lastPosition = &p
	s.mu.Unlock()
	if s.Paths == nil {
		return
	}
	if err := SavePosition(filepath.Join(s.Paths.CacheDir, PositionFile), p.X, p.Y); err != nil {
		s.RecordError(fmt.Sprintf("save panel position: %v", err))
	}
}

// DataVersion is the game data's version for diagnostics: the game version when known, else
// when the data was generated.
func (s *State) DataVersion() *string {
	d := s.Data()
	if d == nil {
		return nil
	}
	m := d.Manifest
	if m.GameVersion != nil && strings.TrimSpace(*m.GameVersion) != "" {
		return new(*m.GameVersion)
	}
	return new(m.GeneratedAt)
}
