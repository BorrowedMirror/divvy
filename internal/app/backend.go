package app

import (
	"context"
	"math"
	"runtime"
	"strings"

	"github.com/BorrowedMirror/rust-king-ledger/internal/config"
	"github.com/BorrowedMirror/rust-king-ledger/internal/core"
	"github.com/BorrowedMirror/rust-king-ledger/internal/ipc"
	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
	"github.com/BorrowedMirror/rust-king-ledger/internal/trade"
)

// Version is the app version, set at build time with
// `-ldflags "-X github.com/BorrowedMirror/rust-king-ledger/internal/app.Version=..."`.
var Version = "0.1.0-rc.1"

// Updater is what the commands need from the auto-updater.
type Updater interface {
	InstallUpdate(ctx context.Context) error
}

// App implements [Backend]: the commands the web UI calls, on top of the flow.
type App struct {
	*Flow
	Permissions *Permissions
	Updater     Updater
}

var _ Backend = (*App)(nil)

// GetSettings returns the current settings.
func (a *App) GetSettings() config.Settings { return a.st.Settings() }

// FindGameLog returns the game log the chat placeholders would read for this `chat.game_log`
// value (empty: found automatically), or nil when there is none.
func (a *App) FindGameLog(configured string) *string {
	if p, ok := ResolveLogPath(configured); ok {
		return new(p)
	}
	return nil
}

// SaveSettings applies and persists settings.
func (a *App) SaveSettings(_ context.Context, settings config.Settings) (config.Settings, error) {
	return a.ApplySettings(settings)
}

// GetDiagnostics is what the diagnostics page shows.
func (a *App) GetDiagnostics() ipc.Diagnostics {
	st := a.st
	d := ipc.Diagnostics{
		Version:      Version,
		OS:           rustOS(),
		Backend:      st.Backend,
		Capabilities: st.CapabilitiesWithNotes(),
		DataVersion:  st.DataVersion(),
		League:       new(st.League()),
		RecentErrors: st.RecentErrors(),
	}
	if st.Paths != nil {
		d.SettingsPath = new(st.Paths.SettingsFile)
	}
	if st.LogPath != "" {
		d.LogPath = new(st.LogPath)
	}
	return d
}

// rustOS is the OS name the Rust build reported (`std::env::consts::OS`).
func rustOS() string {
	if runtime.GOOS == "darwin" {
		return "macos"
	}
	return runtime.GOOS
}

// ListLeagues lists the trade site's leagues and remembers them for league auto-detection.
func (a *App) ListLeagues(ctx context.Context) ([]trade.League, error) {
	leagues, err := a.st.Sources().Trade.Leagues(ctx)
	if err != nil {
		return nil, err
	}
	a.st.SetLeagues(leagues)
	return leagues, nil
}

// PriceCheck parses request.Text, or runs the copy flow against the game when it is nil.
func (a *App) PriceCheck(ctx context.Context, request ipc.PriceCheckRequest) (ipc.PriceCheckResponse, error) {
	var (
		item    *core.ParsedItem
		filters core.FilterSet
		err     error
	)
	if request.Text != nil {
		item, filters, err = a.HandleItemText(*request.Text, request.Pinned)
	} else {
		item, filters, err = a.RunPriceCheck(ctx, request.Pinned)
	}
	if err != nil {
		return ipc.PriceCheckResponse{}, err
	}
	return ipc.PriceCheckResponse{Item: *item, Filters: filters}, nil
}

// QueryPrice prices the last copied item with these filters.
func (a *App) QueryPrice(ctx context.Context, filters core.FilterSet) (core.PriceSummary, error) {
	st := a.st
	last, ok := st.LastItem()
	if !ok {
		return core.PriceSummary{}, ipc.NewError("no_item", "no item has been copied yet")
	}
	settings := st.Settings()
	source := st.Sources().Pick(settings.Trade.Source, &last.Item)
	if settings.Trade.RememberFilters {
		st.RememberFilters(&filters)
	}
	summary, err := source.Price(ctx, &trade.PriceRequest{Item: last.Item, Filters: filters})
	if err != nil {
		st.RecordError(err)
		return core.PriceSummary{}, err
	}
	summary = a.withRatesIfWanted(ctx, &settings, filters.League, summary)
	st.SetLastPrice(summary)
	return summary, nil
}

// withRatesIfWanted converts trade-site results in several currencies with poe2scout's rates,
// so min/median/max include every listing and each one shows its divine/exalted equivalent.
// Best effort: without rates the summary is returned unchanged.
func (a *App) withRatesIfWanted(ctx context.Context, settings *config.Settings, league string, summary core.PriceSummary) core.PriceSummary {
	if !settings.Trade.ConvertPrices || summary.Source != core.SourceTrade || len(summary.Listings) == 0 {
		return summary
	}
	rates, err := a.st.Sources().Scout.Rates(ctx, league)
	if err != nil {
		return summary
	}
	return core.WithRates(summary, &rates)
}

// CurrencyIcons returns currency icons (exchange tag -> name and GGG CDN image URL), served
// from the trade site's `data/static` and cached by the trade client.
func (a *App) CurrencyIcons(ctx context.Context) ([]trade.CurrencyIcon, error) {
	return a.st.Sources().Trade.CurrencyIcons(ctx)
}

// ClearFilterMemory forgets every remembered stat choice.
func (a *App) ClearFilterMemory() error { return a.st.ClearFilterMemory() }

// CopyText puts text on the system clipboard (a listing's whisper).
func (a *App) CopyText(text string) error {
	p := a.st.Platform
	if p == nil {
		return ipc.NewError("platform", "no clipboard on this platform")
	}
	if err := p.Clipboard.SetText(text); err != nil {
		return ipc.NewError("platform", err.Error())
	}
	return nil
}

// OpenLookup opens the current item on poe2db, the wiki or Craft of Exile in the browser.
// Craft of Exile gets the game's copied item text in its URL and imports it with every affix.
func (a *App) OpenLookup(site LookupSite, name string) error {
	u, ok := LookupURL(site, name, a.st.LastItemText())
	if !ok {
		return ipc.NewError("internal", "nothing to look up")
	}
	if err := a.d.OpenURL(u); err != nil {
		return ipc.NewError("internal", err.Error())
	}
	return nil
}

// GetOverlaySnapshot is the last copied item as an `item-copied` payload.
func (a *App) GetOverlaySnapshot() *ipc.ItemCopied {
	last, ok := a.st.LastItem()
	if !ok {
		return nil
	}
	return &ipc.ItemCopied{Item: last.Item, Filters: last.Filters, Pinned: a.st.IsPinned()}
}

// SetOverlayPinned switches between covering the game (unpinned) and a card-sized window
// (pinned) in place.
func (a *App) SetOverlayPinned(pinned bool) error {
	st := a.st
	a.SetPinned(pinned)
	if st.OverlayVisible.Load() {
		w := a.d.Overlay()
		card, _ := st.Card()
		a.layoutOverlay(w, card)
		if err := a.d.BringToFront(w, a.gameID(), true); err != nil {
			st.RecordError("overlay focus: " + err.Error())
		}
	}
	a.EmitOverlayState()
	return nil
}

// ResizeOverlay fits the panel window to its content (height in logical px, measured by the
// web view). The height is clamped to the monitor and the window kept on screen.
func (a *App) ResizeOverlay(width, height float64) error {
	st := a.st
	w := a.d.Overlay()
	scale := w.ScaleFactor()
	x, y, _, _ := w.Bounds()
	monitor, haveMonitor := a.d.MonitorAt(float64(x), float64(y))
	screenW, screenH := math.Inf(1), math.Inf(1)
	if haveMonitor {
		screenW, screenH = float64(monitor.Width)/scale, float64(monitor.Height)/scale
	}
	zoom := st.Settings().Overlay.Scale
	h := FitOverlayHeight(height*zoom, screenH)
	// The card is 420 CSS px wide, or wider with results beside the filters; never wider than
	// the screen.
	maxCSS := 4000.0
	if haveMonitor {
		maxCSS = screenW / zoom
	}
	cssWidth := math.Min(math.Max(width, CardWidth), math.Max(maxCSS, CardWidth))
	logicalWidth := cssWidth * zoom
	st.SetCardHeight(h)
	st.SetCardCSSWidth(cssWidth)

	// Covering the game: the window stays game-sized. A card the user has not moved is pulled
	// up when its content grows past the bottom of the game.
	if st.OverlayLayout().Cover {
		if g, ok := a.GameWindow(); ok && !st.CardMoved.Load() {
			card, _ := st.Card()
			b := CardBounds(g.Rect)
			ny := KeepOnScreenY(card.Y, physical(h, scale), b.Y, b.Height)
			if ny != card.Y {
				card.Y = ny
				st.SetCardPosition(card)
				cx, cy := CardInWindow(card.X, card.Y, g.Rect, scale, macPoints)
				layout := ipc.OverlayLayout{Cover: true, X: cx / zoom, Y: cy / zoom}
				st.SetOverlayLayout(layout)
				a.emit(ipc.EventOverlayLayout, layout)
			}
		}
		return nil
	}
	w.SetLogicalSize(logicalWidth, h)
	a.roundCard(w, logicalWidth, h, zoom)
	if haveMonitor {
		hPhys, wPhys := physical(h, scale), physical(logicalWidth, scale)
		// The same clamp works on either axis: keep the card on the monitor.
		ny := KeepOnScreenY(y, hPhys, monitor.Y, monitor.Height)
		nx := KeepOnScreenY(x, wPhys, monitor.X, monitor.Width)
		if nx != x || ny != y {
			w.SetPosition(nx, ny)
			st.SetCardPosition(Point{nx, ny})
		}
	}
	return nil
}

// DragCard moves the price card while the user drags its header. The web view only reports
// the phase: the cursor is read here (web view screen coordinates go stale while the window
// moves under the pointer). A card-sized window moves itself; over the game the card moves
// inside the covering window. DragEnd remembers the spot for `Position: Last`.
func (a *App) DragCard(phase ipc.DragPhase) error {
	st := a.st
	w := a.d.Overlay()
	cx, cy, ok := a.d.CursorPosition()
	if !ok {
		return ipc.NewError("internal", "cursor position unavailable")
	}
	cardNow, _ := st.Card()
	st.mu.Lock()
	if phase == ipc.DragStart {
		st.drag = &dragOrigin{CursorX: cx, CursorY: cy, Card: cardNow}
		st.mu.Unlock()
		st.CardMoved.Store(true)
		return nil
	}
	origin := st.drag
	st.mu.Unlock()
	if origin == nil {
		return nil
	}
	card := Point{
		X: origin.Card.X + int32(math.Round(cx-origin.CursorX)),
		Y: origin.Card.Y + int32(math.Round(cy-origin.CursorY)),
	}
	cover := st.OverlayLayout().Cover
	g, haveGame := a.GameWindow()
	// Over the game, keep the card's left edge on the game and at least 120 CSS px of it
	// visible (the web view applies the same rule).
	if cover && haveGame {
		px := w.ScaleFactor() * st.Settings().Overlay.Scale
		width := int32(math.Round(st.CardCSSWidth() * px))
		visible := int32(math.Round(120 * px))
		b := CardBounds(g.Rect)
		card.X = min(max(card.X, b.X), max(b.X+b.Width-width, b.X))
		card.Y = min(max(card.Y, b.Y), max(b.Y+b.Height-visible, b.Y))
	}
	st.SetCardPosition(card)
	if cover {
		if haveGame {
			zoom := st.Settings().Overlay.Scale
			x, y := CardInWindow(card.X, card.Y, g.Rect, w.ScaleFactor(), macPoints)
			layout := ipc.OverlayLayout{Cover: true, X: x / zoom, Y: y / zoom}
			st.SetOverlayLayout(layout)
			a.emit(ipc.EventOverlayLayout, layout)
		}
	} else {
		w.SetPosition(card.X, card.Y)
	}
	if phase == ipc.DragEnd {
		st.mu.Lock()
		st.drag = nil
		st.mu.Unlock()
		st.SetLastPosition(card)
	}
	return nil
}

// GetOverlayLayout is the current panel layout, for the web view to catch up on mount.
func (a *App) GetOverlayLayout() ipc.OverlayLayout { return a.st.OverlayLayout() }

// CloseSettings hides the settings window.
func (a *App) CloseSettings() error { return a.HideSettings() }

// OpenSettings shows the settings window.
func (a *App) OpenSettings() error { return a.ShowSettings("settings") }

// OpenSettingsTab opens settings on one tab (`waystones`, `hotkeys`, ...).
func (a *App) OpenSettingsTab(tab string) error {
	if tab == "" || len(tab) > 20 || strings.IndexFunc(tab, func(r rune) bool { return r < 'a' || r > 'z' }) >= 0 {
		return ipc.NewError("internal", "unknown settings tab")
	}
	return a.ShowSettings("settings?tab=" + tab)
}

// WaystoneMods lists every waystone mod RKL knows, grouped, for the Waystones settings tab.
func (a *App) WaystoneMods(context.Context) ([]WaystoneMod, error) {
	gameData := a.st.WaitData()
	texts := func(yield func(string) bool) {
		if gameData == nil {
			return
		}
		for _, s := range gameData.Bundle().Stats {
			if strings.HasPrefix(s.ID, "explicit.") && !yield(s.Text) {
				return
			}
		}
	}
	return WaystoneCatalog(texts), nil
}

// RebindHotkey changes one hotkey and applies the settings.
func (a *App) RebindHotkey(_ context.Context, action platform.HotkeyAction, chord platform.Chord) error {
	settings := a.st.Settings()
	switch action.Kind {
	case platform.PriceCheck:
		settings.Hotkeys.PriceCheck = chord
	case platform.PriceCheckPinned:
		settings.Hotkeys.PriceCheckPinned = chord
	case platform.OpenSettings:
		settings.Hotkeys.OpenSettings = chord
	case platform.OpenTrade:
		settings.Hotkeys.OpenTrade = chord
	case platform.Chat:
		if int(action.Index) >= len(settings.Chat.Commands) {
			return ipc.NewError("config", "no such chat command")
		}
		settings.Chat.Commands[action.Index].Chord = chord
	case platform.StashSearch:
		if int(action.Index) >= len(settings.Stash.Searches) {
			return ipc.NewError("config", "no such stash search")
		}
		settings.Stash.Searches[action.Index].Chord = chord
	}
	_, err := a.ApplySettings(settings)
	return err
}

// GetPermissions is the permission report for the onboarding view.
func (a *App) GetPermissions() PermissionReport { return a.Permissions.Current() }

// RequestPermission opens the OS prompt or settings page for one permission.
func (a *App) RequestPermission(kind string) error { return a.Permissions.Request(kind) }

// RecheckPermissions checks the permissions again and broadcasts a change.
func (a *App) RecheckPermissions() PermissionReport {
	report, changed := a.Permissions.Recheck()
	if changed {
		a.emit(PermissionsChangedEvent, PermissionsChanged{Report: report})
	}
	return report
}

// InstallUpdate downloads and installs the available update, then restarts.
func (a *App) InstallUpdate(ctx context.Context) error {
	if a.Updater == nil {
		return ipc.NewError("update", "updates are not available in this build")
	}
	return a.Updater.InstallUpdate(ctx)
}
