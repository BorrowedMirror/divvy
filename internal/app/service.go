package app

import (
	"context"

	"github.com/BorrowedMirror/rust-king-ledger/internal/config"
	"github.com/BorrowedMirror/rust-king-ledger/internal/core"
	"github.com/BorrowedMirror/rust-king-ledger/internal/gamelog"
	"github.com/BorrowedMirror/rust-king-ledger/internal/ipc"
	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
	"github.com/BorrowedMirror/rust-king-ledger/internal/trade"
)

//go:generate go run ../../cmd/bindgen -root ../..

// Service is the Wails-bound IPC service: one exported method per command the web UI calls
// through the generated web/src/bindings.ts (`go generate ./...` after changing a signature).
// Methods returning an error reject with an [ipc.Error], which the frontend receives as
// `{status: "error", error}`. A leading context.Context is supplied by Wails.
type Service struct {
	a *App
}

// NewService binds the commands to a.
func NewService(a *App) *Service { return &Service{a: a} }

// wrap turns any error into the [ipc.Error] the frontend expects.
func wrap(err error) error {
	if err == nil {
		return nil
	}
	return ipc.From(err)
}

func wrap1[T any](v T, err error) (T, error) { return v, wrap(err) }

// GetSettings returns the current settings.
func (s *Service) GetSettings() config.Settings { return s.a.st.Settings() }

// FindGameLog returns the game log the chat placeholders would read for this `chat.game_log`
// value (empty: found automatically), or null when there is none.
func (s *Service) FindGameLog(configured string) *string {
	if p, ok := gamelog.Resolve(configured); ok {
		return &p
	}
	return nil
}

// SaveSettings validates, applies (hotkeys included) and persists settings, then broadcasts
// `settings-changed`.
func (s *Service) SaveSettings(_ context.Context, settings config.Settings) (config.Settings, error) {
	return wrap1(s.a.applySettings(settings))
}

// GetDiagnostics is what the diagnostics page shows.
func (s *Service) GetDiagnostics() ipc.Diagnostics { return s.a.diagnostics() }

// ListLeagues lists the trade site's leagues.
func (s *Service) ListLeagues(ctx context.Context) ([]trade.League, error) {
	return wrap1(s.a.ListLeagues(ctx))
}

// PriceCheck parses `request.text`, or runs the copy flow against the game when it is null.
func (s *Service) PriceCheck(ctx context.Context, request ipc.PriceCheckRequest) (ipc.PriceCheckResponse, error) {
	return wrap1(s.a.priceCheck(ctx, request))
}

// QueryPrice prices the last copied item with these filters.
func (s *Service) QueryPrice(ctx context.Context, filters core.FilterSet) (core.PriceSummary, error) {
	return wrap1(s.a.queryPrice(ctx, filters))
}

// OpenTrade opens a trade-site URL in the browser.
func (s *Service) OpenTrade(url string) error { return wrap(s.a.openTrade(url)) }

// HideOverlay hides the price panel.
func (s *Service) HideOverlay() error {
	s.a.hideOverlay()
	return nil
}

// SetOverlayPinned pins or unpins the price panel.
func (s *Service) SetOverlayPinned(pinned bool) error {
	s.a.setOverlayPinned(pinned)
	return nil
}

// GetOverlaySnapshot is the last copied item as an `item-copied` payload. The overlay calls it
// once its listeners are registered, so an item copied before the web view was listening is
// not lost.
func (s *Service) GetOverlaySnapshot() *ipc.ItemCopied { return s.a.overlaySnapshot() }

// OpenSettings shows the settings window.
func (s *Service) OpenSettings() error {
	s.a.ShowSettings("settings")
	return nil
}

// CloseSettings hides the settings window (overlay mode: Esc or a click on the backdrop).
func (s *Service) CloseSettings() error {
	s.a.hideSettings()
	return nil
}

// ResizeOverlay resizes the price panel window to fit its content (`height` in logical px,
// measured by the web view). Width is unchanged; the height is clamped to the monitor and the
// window is kept on screen.
func (s *Service) ResizeOverlay(width, height float64) error {
	s.a.resizeOverlay(width, height)
	return nil
}

// CurrencyIcons returns currency icons (exchange tag -> name and GGG CDN image URL) for showing
// prices as icons. Served from the trade site's `data/static`, cached by the trade client.
func (s *Service) CurrencyIcons(ctx context.Context) ([]trade.CurrencyIcon, error) {
	return wrap1(s.a.st.priceSources().trade.CurrencyIcons(ctx))
}

// CopyText puts `text` on the system clipboard (e.g. a listing's whisper).
func (s *Service) CopyText(text string) error { return wrap(s.a.copyText(text)) }

// DragCard moves the price card while the user drags its header. The web view only reports the
// phase; the cursor is read on the Go side.
func (s *Service) DragCard(phase ipc.DragPhase) error { return wrap(s.a.dragCard(phase)) }

// OpenLookup opens the current item on poe2db, the wiki or Craft of Exile in the browser. Craft
// of Exile gets the game's copied item text in its URL and imports it with every affix.
func (s *Service) OpenLookup(site LookupSite, name string) error {
	return wrap(s.a.openLookup(site, name))
}

// OpenSettingsTab opens settings on one tab (`waystones`, `hotkeys`, ...).
func (s *Service) OpenSettingsTab(tab string) error { return wrap(s.a.showSettingsTab(tab)) }

// WaystoneMods lists every waystone mod RKL knows, grouped, for the Waystones settings tab.
func (s *Service) WaystoneMods(_ context.Context) ([]WaystoneMod, error) {
	return s.a.waystoneMods(), nil
}

// ClearFilterMemory forgets every remembered stat choice.
func (s *Service) ClearFilterMemory() error { return wrap(s.a.st.clearFilterMemory()) }

// GetOverlayLayout is the current price panel layout, for the web view to catch up on mount.
func (s *Service) GetOverlayLayout() ipc.OverlayLayout { return s.a.st.layout() }

// RebindHotkey changes one hotkey and applies the settings.
func (s *Service) RebindHotkey(_ context.Context, action platform.HotkeyAction, chord platform.Chord) error {
	return wrap(s.a.rebindHotkey(action, chord))
}

// GetPermissions is the permission report for the onboarding view.
func (s *Service) GetPermissions() ipc.PermissionReport { return s.a.permissions.Current() }

// RequestPermission opens the OS prompt or settings page for one permission.
func (s *Service) RequestPermission(kind string) error { return wrap(s.a.permissions.request(kind)) }

// RecheckPermissions checks the permissions again (after the user changed them).
func (s *Service) RecheckPermissions() ipc.PermissionReport { return s.a.recheckPermissions() }

// InstallUpdate downloads and installs the available update, then restarts.
func (s *Service) InstallUpdate(ctx context.Context) error { return wrap(s.a.installUpdate(ctx)) }
