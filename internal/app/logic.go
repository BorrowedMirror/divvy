package app

// Pure, Wails-independent pieces of the app flow (port of apps/desktop/src-tauri/src/logic.rs).
// Positions, URLs, waystone mods and chat/stash macros live in the other logic_*.go files.

import (
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/BorrowedMirror/rust-king-ledger/internal/config"
	"github.com/BorrowedMirror/rust-king-ledger/internal/core"
	"github.com/BorrowedMirror/rust-king-ledger/internal/trade"
)

// MaxItemText is the largest clipboard text accepted as an item (real items are a few KiB).
const MaxItemText = 64 * 1024

// ItemTextError is returned by [ProcessItemText] when the text is not an item. The IPC layer
// reports it with error kind [ItemTextError.IPCKind] and [ItemTextError.Error] as the message.
type ItemTextError struct {
	Message string
	// Err is the parser's error, nil for oversized text.
	Err error
}

func (e *ItemTextError) Error() string { return e.Message }

// Unwrap returns the parser's error.
func (e *ItemTextError) Unwrap() error { return e.Err }

// IPCKind is the frontend error kind (`IpcError.kind`): `parse`.
func (*ItemTextError) IPCKind() string { return "parse" }

// ProcessItemText parses clipboard text, matches stats and builds default filters. catalog
// may be nil (no stat matching, no unique candidates).
func ProcessItemText(text string, catalog core.Catalog, opts *core.QueryOptions) (*core.ParsedItem, core.FilterSet, error) {
	if len(text) > MaxItemText {
		return nil, core.FilterSet{}, &ItemTextError{Message: "item text too large"}
	}
	item, err := core.ParseItem(text)
	if err != nil {
		return nil, core.FilterSet{}, &ItemTextError{Message: err.Error(), Err: err}
	}
	if catalog != nil {
		core.NewStatMatcher(catalog).MatchItem(item)
	}
	filters := core.BuildFilters(item, opts)
	if item.Rarity == core.RarityUnique && item.Unidentified && catalog != nil {
		uniques := catalog.UniquesByBase(item.BaseType)
		names := make([]string, 0, len(uniques))
		for _, u := range uniques {
			names = append(names, u.Name)
		}
		filters.UniqueCandidates = names
	}
	return item, filters, nil
}

// QueryOptionsFor derives query options from settings and the resolved league.
func QueryOptionsFor(settings *config.Settings, league string) core.QueryOptions {
	return core.QueryOptions{
		League:         league,
		Status:         settings.Trade.Sellers,
		WidenPct:       settings.Trade.WidenPct,
		EnableAllStats: settings.Trade.EnableAllStats,
	}
}

// AutoLeague picks the current softcore trade league: the first league that is not Standard,
// Hardcore, SSF, Solo Self-Found or a private/event ruthless variant; falls back to `Standard`.
// ok is false when neither is listed.
func AutoLeague(leagues []trade.League) (league string, ok bool) {
	isChallengeSC := func(l *trade.League) bool {
		id := asciiLowerString(l.ID)
		return !(id == "standard" ||
			strings.Contains(id, "hardcore") ||
			strings.HasPrefix(id, "hc ") ||
			strings.Contains(id, "ssf") ||
			strings.Contains(id, "solo self-found") ||
			strings.Contains(id, "ruthless"))
	}
	for i := range leagues {
		if isChallengeSC(&leagues[i]) {
			return leagues[i].ID, true
		}
	}
	for i := range leagues {
		if asciiLowerString(leagues[i].ID) == "standard" {
			return leagues[i].ID, true
		}
	}
	return "", false
}

// ResolveLeague is the league to use given settings and the live league list (nil when it
// could not be fetched).
func ResolveLeague(settings *config.Settings, live []trade.League) string {
	if !settings.League.AutoDetect && settings.League.League != nil && *settings.League.League != "" {
		return *settings.League.League
	}
	if l, ok := AutoLeague(live); ok {
		return l
	}
	if settings.League.League != nil {
		return *settings.League.League
	}
	return "Standard"
}

// UserAgent is the trade User-Agent including the optional contact from settings.
func UserAgent(settings *config.Settings) string {
	base := trade.DefaultUserAgent()
	if c := settings.Trade.Contact; c != nil && *c != "" {
		return base + " " + *c
	}
	return base
}

// LogFilter is the tracing filter string the Rust app built: env (`RKL_LOG`, "" when unset) or
// `info`, with the HTTP/TLS stack capped at `info` so debug logs never contain request headers
// (POESESSID cookie), and enigo kept to errors unless env names it.
func LogFilter(env string) string {
	base := strings.TrimSpace(env)
	if base == "" {
		base = "info"
	}
	caps := "reqwest=info,hyper=info,hyper_util=info,h2=info,rustls=info"
	if strings.Contains(base, "enigo") {
		return base + "," + caps
	}
	return base + "," + caps + ",enigo=error"
}

// MayCopy reports whether the copy keystroke may be sent: only into the focused game window
// when the backend can track windows (otherwise e.g. a terminal would receive Ctrl+C).
// gameFocused is only called with tracking.
func MayCopy(windowTracking bool, gameFocused func() bool) bool {
	return !windowTracking || gameFocused()
}

// ShellHandlesSIGUSR2 reports whether the shell must handle SIGUSR2 itself: a Wayland platform
// already maps it to a hotkey. SIGUSR1 (pinned) is always the shell's, else its default action
// would kill the app.
func ShellHandlesSIGUSR2(waylandPlatform bool) bool { return !waylandPlatform }

// MustShowSettings reports whether settings must be shown at startup: only when the user would
// otherwise have no way in, neither a tray icon nor a registered hotkey. With either one the
// app is reachable (missing hotkeys are explained by first-run onboarding and the diagnostics
// page, and `rkl --trigger` works).
func MustShowSettings(trayOK, hotkeysOK bool) bool { return !trayOK && !hotkeysOK }

// FlowGateDebounce is the default debounce of a [FlowGate].
const FlowGateDebounce = 150 * time.Millisecond

// FlowGate serialises price-check flows: one copy at a time, and presses within the debounce
// window of the last accepted one are dropped (key repeat / storms). Overlapping copies could
// otherwise "restore" the clipboard to the item text captured by the other run. Create it with
// [NewFlowGate]; it is safe for concurrent use.
type FlowGate struct {
	inFlight atomic.Bool
	mu       sync.Mutex
	last     time.Time
	hasLast  bool
	debounce time.Duration
}

// NewFlowGate returns a gate with the given debounce ([FlowGateDebounce] by default).
func NewFlowGate(debounce time.Duration) *FlowGate { return &FlowGate{debounce: debounce} }

// FlowPermit is held while a flow runs; [FlowPermit.Release] lets the next press through.
type FlowPermit struct {
	gate     *FlowGate
	released atomic.Bool
}

// Release ends the flow. Only the first call has an effect.
func (p *FlowPermit) Release() {
	if p.released.CompareAndSwap(false, true) {
		p.gate.inFlight.Store(false)
	}
}

// TryBegin starts a flow at now, or returns false if one is running or the last start was too
// recent (a now before the last start counts as too recent).
func (g *FlowGate) TryBegin(now time.Time) (*FlowPermit, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.hasLast && max(now.Sub(g.last), 0) < g.debounce {
		return nil, false
	}
	if !g.inFlight.CompareAndSwap(false, true) {
		return nil, false
	}
	g.last, g.hasLast = now, true
	return &FlowPermit{gate: g}, true
}

// asciiLowerString lowercases ASCII letters only (Rust's to_ascii_lowercase).
func asciiLowerString(s string) string {
	b := []byte(s)
	for i, c := range b {
		if 'A' <= c && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}
