package app

import (
	"errors"
	"io/fs"
	"log/slog"
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

// maxErrors is how many recorded errors the diagnostics page keeps.
const maxErrors = 50

// copiedItem is the last item copied from the game.
type copiedItem struct {
	item    core.ParsedItem
	filters core.FilterSet
	text    string // the item text exactly as the game copied it
}

// dragOrigin is the cursor and card position (physical px) when a header drag started.
type dragOrigin struct {
	cursorX, cursorY float64
	card             point
}

// StateOptions configure [NewState].
type StateOptions struct {
	// Paths are the settings, cache and log locations; nil when there is no settings
	// directory (nothing is saved then).
	Paths    *config.Paths
	Settings config.Settings
	// Platform is the platform backend; nil when it is unavailable.
	Platform *platform.Platform
	Backend  platform.BackendKind
	// GameData decodes the bundled game data (data.Bundled); it is called once.
	GameData func() (*data.GameData, error)
	// LogPath is the open log file, for diagnostics ("" when there is none).
	LogPath string
}

// State is the long-lived app state shared by the commands, the hotkey loop, the tray and the
// test hook. It is safe for concurrent use.
type State struct {
	paths    *config.Paths
	platform *platform.Platform
	backend  platform.BackendKind
	logPath  string

	settingsMu sync.RWMutex
	settings   config.Settings

	gameData func() (*data.GameData, error) // decodes once, then returns the cached result
	decoded  atomic.Bool                    // gameData has returned

	sourcesMu sync.RWMutex
	sources   *priceSources

	filterSaveMu sync.Mutex // orders filter memory saves and clears

	mu            sync.Mutex // guards the fields below
	last          *copiedItem
	lastPrice     *core.PriceSummary
	leagues       []trade.League
	errors        []string
	filters       filterMemory
	lastPosition  *point // where the user last dropped the card, for `Position: Last`
	drag          *dragOrigin
	overlayLayout ipc.OverlayLayout
	card          point   // card position on screen (physical px)
	cardHeight    float64 // last measured card height (logical px)
	cardCSSWidth  float64 // 420, or wider with results beside the filters

	pinned          atomic.Bool
	overlayVisible  atomic.Bool
	settingsOverlay atomic.Bool // settings shown as an overlay covering the game window
	cardMoved       atomic.Bool // the user dragged the card since it was last shown
	showSeq         atomic.Uint64
	followFocus     atomic.Bool // hotkeys are grabbed only while the game has focus

	macroMu sync.Mutex // serialises chat commands and stash searches (they borrow the clipboard)
	applyMu sync.Mutex // held for a whole settings apply or hotkey (re)registration
	gate    *flowGate  // serialises copy flows (hotkey, signal, tray, test hook, IPC)
}

// NewState builds the state and the price sources for its settings.
func NewState(opts StateOptions) (*State, error) {
	sources, err := newPriceSources(&opts.Settings)
	if err != nil {
		return nil, err
	}
	gameData := opts.GameData
	if gameData == nil {
		gameData = func() (*data.GameData, error) { return nil, errors.New("no game data") }
	}
	st := &State{
		paths:        opts.Paths,
		platform:     opts.Platform,
		backend:      opts.Backend,
		logPath:      opts.LogPath,
		settings:     opts.Settings,
		gameData:     sync.OnceValues(gameData),
		sources:      sources,
		cardHeight:   cardHeight,
		cardCSSWidth: cardWidth,
		gate:         &flowGate{debounce: flowGateDebounce},
	}
	if p := opts.Paths; p != nil {
		if pos, ok := loadPosition(filepath.Join(p.CacheDir, positionFile)); ok {
			st.lastPosition = &pos
		}
		st.filters = loadFilterMemory(filepath.Join(p.CacheDir, filterMemoryFile))
	}
	return st, nil
}

// Settings returns a copy of the current settings.
func (s *State) Settings() config.Settings {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	return s.settings.Clone()
}

func (s *State) setSettings(next config.Settings) {
	s.settingsMu.Lock()
	s.settings = next
	s.settingsMu.Unlock()
}

func (s *State) priceSources() *priceSources {
	s.sourcesMu.RLock()
	defer s.sourcesMu.RUnlock()
	return s.sources
}

func (s *State) setPriceSources(next *priceSources) {
	s.sourcesMu.Lock()
	s.sources = next
	s.sourcesMu.Unlock()
}

// Capabilities is what the platform backend can do here.
func (s *State) Capabilities() platform.Capabilities {
	if s.platform == nil {
		return platform.Capabilities{}
	}
	return s.platform.Capabilities
}

// capabilitiesWithNotes adds problems the hotkey host found after registration (diagnostics).
func (s *State) capabilitiesWithNotes() platform.Capabilities {
	caps := s.Capabilities()
	caps.Notes = append([]string{}, caps.Notes...)
	if s.platform != nil && s.platform.Hotkeys != nil {
		caps.Notes = append(caps.Notes, s.platform.Hotkeys.Notes()...)
	}
	return caps
}

// RecordError logs what failed (msg) and why (err, may be nil) and keeps it for the
// diagnostics page (the last 50).
func (s *State) RecordError(msg string, err error) {
	if err != nil {
		slog.Warn(msg, "err", err) //nolint:sloglint // callers pass a constant message
		msg += ": " + err.Error()
	} else {
		slog.Warn(msg) //nolint:sloglint // callers pass a constant message
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.errors) == maxErrors {
		s.errors = s.errors[1:]
	}
	s.errors = append(s.errors, msg)
}

// recentErrors returns the kept errors, oldest first.
func (s *State) recentErrors() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string{}, s.errors...)
}

// league is the league to search in: the configured one, else the first live trade league.
func (s *State) league() string {
	settings := s.Settings()
	s.mu.Lock()
	leagues := s.leagues
	s.mu.Unlock()
	return resolveLeague(&settings, leagues)
}

// setLeagues stores the live league list.
func (s *State) setLeagues(leagues []trade.League) {
	s.mu.Lock()
	s.leagues = leagues
	s.mu.Unlock()
}

// data is the game data if decoding has finished (diagnostics; never blocks).
func (s *State) data() *data.GameData {
	if !s.decoded.Load() {
		return nil
	}
	return s.waitData()
}

// waitData is the game data, waiting for the decode to finish (item parsing needs the
// catalog). nil when decoding failed.
func (s *State) waitData() *data.GameData {
	d, err := s.gameData()
	s.decoded.Store(true)
	if err != nil {
		return nil
	}
	return d
}

// DecodeData starts decoding the game data in the background (startup).
func (s *State) DecodeData() {
	go func() {
		if _, err := s.gameData(); err != nil {
			s.RecordError("game data unavailable", err)
		}
		s.decoded.Store(true)
	}()
}

// dataVersion is the game data's version for diagnostics: the game version when known, else
// when the data was generated.
func (s *State) dataVersion() *string {
	d := s.data()
	if d == nil {
		return nil
	}
	m := d.Manifest
	if m.GameVersion != nil && strings.TrimSpace(*m.GameVersion) != "" {
		return new(*m.GameVersion)
	}
	return new(m.GeneratedAt)
}

// lastItem returns the last copied item.
func (s *State) lastItem() (copiedItem, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.last == nil {
		return copiedItem{}, false
	}
	return *s.last, true
}

// setLastItem stores the last copied item, its filters and its text as the game copied it.
func (s *State) setLastItem(item copiedItem) {
	s.mu.Lock()
	s.last = &item
	s.mu.Unlock()
}

// lastItemText is the last item's text exactly as the game copied it.
func (s *State) lastItemText() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.last == nil {
		return ""
	}
	return s.last.text
}

// setLastPrice stores the last price summary.
func (s *State) setLastPrice(p core.PriceSummary) {
	s.mu.Lock()
	s.lastPrice = &p
	s.mu.Unlock()
}

// lastPriceSummary is a copy of the last price summary; nil before the first one.
func (s *State) lastPriceSummary() *core.PriceSummary {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lastPrice == nil {
		return nil
	}
	p := *s.lastPrice
	return &p
}

// lastTradeURL is the trade-site search of the last price check; ok is false when there is
// none.
func (s *State) lastTradeURL() (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lastPrice == nil || s.lastPrice.TradeURL == nil {
		return "", false
	}
	return *s.lastPrice.TradeURL, true
}

// rememberFilters records the stat choices of a search the user ran (best effort; a failed
// save only loses them).
func (s *State) rememberFilters(filters *core.FilterSet) {
	if s.paths == nil {
		return
	}
	s.filterSaveMu.Lock()
	defer s.filterSaveMu.Unlock()
	s.mu.Lock()
	changed := s.filters.record(filters)
	var b []byte
	var err error
	if changed {
		b, err = s.filters.marshal()
	}
	s.mu.Unlock()
	if !changed {
		return
	}
	if err == nil {
		err = writeFileAtomic(filepath.Join(s.paths.CacheDir, filterMemoryFile), b)
	}
	if err != nil {
		s.RecordError("save remembered filters", err)
	}
}

// applyFilterMemory turns remembered stats on or off in new filters.
func (s *State) applyFilterMemory(filters *core.FilterSet) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.filters.apply(filters)
}

// clearFilterMemory forgets every remembered stat choice and deletes the file.
func (s *State) clearFilterMemory() error {
	s.filterSaveMu.Lock()
	defer s.filterSaveMu.Unlock()
	s.mu.Lock()
	s.filters = filterMemory{}
	s.mu.Unlock()
	if s.paths == nil {
		return nil
	}
	err := os.Remove(filepath.Join(s.paths.CacheDir, filterMemoryFile))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return ipc.Errorf(ipc.KindConfig, "%w", err)
	}
	return nil
}

// layout is the current price panel layout.
func (s *State) layout() ipc.OverlayLayout {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.overlayLayout
}

func (s *State) setLayout(l ipc.OverlayLayout) {
	s.mu.Lock()
	s.overlayLayout = l
	s.mu.Unlock()
}

// cardPosition returns the card position (physical px) and its last measured height (logical
// px).
func (s *State) cardPosition() (point, float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.card, s.cardHeight
}

func (s *State) setCardPosition(p point) {
	s.mu.Lock()
	s.card = p
	s.mu.Unlock()
}

// setCardSize stores the card's measured height (logical px) and width (CSS px).
func (s *State) setCardSize(height, cssWidth float64) {
	s.mu.Lock()
	s.cardHeight, s.cardCSSWidth = height, cssWidth
	s.mu.Unlock()
}

// cardWidthCSS is the card width in CSS px.
func (s *State) cardWidthCSS() float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cardCSSWidth
}

// droppedPosition is where the user last dropped the card.
func (s *State) droppedPosition() (point, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lastPosition == nil {
		return point{}, false
	}
	return *s.lastPosition, true
}

// setDroppedPosition remembers where the user dropped the card and saves it.
func (s *State) setDroppedPosition(p point) {
	s.mu.Lock()
	s.lastPosition = &p
	s.mu.Unlock()
	if s.paths == nil {
		return
	}
	if err := savePosition(filepath.Join(s.paths.CacheDir, positionFile), p); err != nil {
		s.RecordError("save panel position", err)
	}
}

// beginDrag records where a header drag started.
func (s *State) beginDrag(o dragOrigin) {
	s.mu.Lock()
	s.drag = &o
	s.mu.Unlock()
}

// dragStart is where the running header drag started; ok is false when none is running.
func (s *State) dragStart() (dragOrigin, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.drag == nil {
		return dragOrigin{}, false
	}
	return *s.drag, true
}

// endDrag forgets the running header drag.
func (s *State) endDrag() {
	s.mu.Lock()
	s.drag = nil
	s.mu.Unlock()
}

// dragging reports whether a header drag is running.
func (s *State) dragging() bool {
	_, ok := s.dragStart()
	return ok
}
