package app

import (
	"context"

	"github.com/BorrowedMirror/rust-king-ledger/internal/config"
	"github.com/BorrowedMirror/rust-king-ledger/internal/core"
	"github.com/BorrowedMirror/rust-king-ledger/internal/ipc"
	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
	"github.com/BorrowedMirror/rust-king-ledger/internal/trade"
)

// Service is the Wails-bound IPC service: one exported method per command the web UI calls
// through the generated web/src/bindings.ts (`go run ./cmd/bindgen` after changing a
// signature). Methods returning an error reject with an [ipc.IpcError], which the frontend
// receives as `{status: "error", error}`. A leading context.Context is supplied by Wails.
type Service struct {
	b Backend
}

// NewService binds the commands to b.
func NewService(b Backend) *Service { return &Service{b: b} }

// Backend is what the commands call into. The running app implements it; tests use a fake.
type Backend interface {
	GetSettings() config.Settings
	FindGameLog(configured string) *string
	SaveSettings(ctx context.Context, settings config.Settings) (config.Settings, error)
	GetDiagnostics() ipc.Diagnostics
	ListLeagues(ctx context.Context) ([]trade.League, error)
	PriceCheck(ctx context.Context, request ipc.PriceCheckRequest) (ipc.PriceCheckResponse, error)
	QueryPrice(ctx context.Context, filters core.FilterSet) (core.PriceSummary, error)
	OpenTrade(url string) error
	HideOverlay() error
	SetOverlayPinned(pinned bool) error
	GetOverlaySnapshot() *ipc.ItemCopied
	OpenSettings() error
	CloseSettings() error
	ResizeOverlay(width, height float64) error
	CurrencyIcons(ctx context.Context) ([]trade.CurrencyIcon, error)
	CopyText(text string) error
	DragCard(phase ipc.DragPhase) error
	OpenLookup(site LookupSite, name string) error
	OpenSettingsTab(tab string) error
	WaystoneMods(ctx context.Context) ([]WaystoneMod, error)
	ClearFilterMemory() error
	GetOverlayLayout() ipc.OverlayLayout
	RebindHotkey(ctx context.Context, action platform.HotkeyAction, chord platform.Chord) error
	GetPermissions() PermissionReport
	RequestPermission(kind string) error
	RecheckPermissions() PermissionReport
	InstallUpdate(ctx context.Context) error
}

// wrap turns any backend error into the IpcError the frontend expects.
func wrap(err error) error {
	if err == nil {
		return nil
	}
	return ipc.From(err)
}

func wrap1[T any](v T, err error) (T, error) { return v, wrap(err) }

// GetSettings returns the current settings.
func (s *Service) GetSettings() config.Settings { return s.b.GetSettings() }

// FindGameLog returns the game log the chat placeholders would read for this `chat.game_log`
// value (empty: found automatically), or null when there is none.
func (s *Service) FindGameLog(configured string) *string { return s.b.FindGameLog(configured) }

// SaveSettings validates, applies (hotkeys included) and persists settings, then broadcasts
// `settings-changed`.
func (s *Service) SaveSettings(ctx context.Context, settings config.Settings) (config.Settings, error) {
	return wrap1(s.b.SaveSettings(ctx, settings))
}

// GetDiagnostics is what the diagnostics page shows.
func (s *Service) GetDiagnostics() ipc.Diagnostics { return s.b.GetDiagnostics() }

// ListLeagues lists the trade site's leagues.
func (s *Service) ListLeagues(ctx context.Context) ([]trade.League, error) {
	return wrap1(s.b.ListLeagues(ctx))
}

// PriceCheck parses `request.text`, or runs the copy flow against the game when it is null.
func (s *Service) PriceCheck(ctx context.Context, request ipc.PriceCheckRequest) (ipc.PriceCheckResponse, error) {
	return wrap1(s.b.PriceCheck(ctx, request))
}

// QueryPrice prices the last copied item with these filters.
func (s *Service) QueryPrice(ctx context.Context, filters core.FilterSet) (core.PriceSummary, error) {
	return wrap1(s.b.QueryPrice(ctx, filters))
}

// OpenTrade opens a trade-site URL in the browser.
func (s *Service) OpenTrade(url string) error { return wrap(s.b.OpenTrade(url)) }

// HideOverlay hides the price panel.
func (s *Service) HideOverlay() error { return wrap(s.b.HideOverlay()) }

// SetOverlayPinned pins or unpins the price panel.
func (s *Service) SetOverlayPinned(pinned bool) error { return wrap(s.b.SetOverlayPinned(pinned)) }

// GetOverlaySnapshot is the last copied item as an `item-copied` payload. The overlay calls it
// once its listeners are registered, so an item copied before the web view was listening is
// not lost.
func (s *Service) GetOverlaySnapshot() *ipc.ItemCopied { return s.b.GetOverlaySnapshot() }

// OpenSettings shows the settings window.
func (s *Service) OpenSettings() error { return wrap(s.b.OpenSettings()) }

// CloseSettings hides the settings window (overlay mode: Esc or a click on the backdrop).
func (s *Service) CloseSettings() error { return wrap(s.b.CloseSettings()) }

// ResizeOverlay resizes the price panel window to fit its content (`height` in logical px,
// measured by the web view). Width is unchanged; the height is clamped to the monitor and the
// window is kept on screen.
func (s *Service) ResizeOverlay(width, height float64) error {
	return wrap(s.b.ResizeOverlay(width, height))
}

// CurrencyIcons returns currency icons (exchange tag -> name and GGG CDN image URL) for showing
// prices as icons. Served from the trade site's `data/static`, cached by the trade client.
func (s *Service) CurrencyIcons(ctx context.Context) ([]trade.CurrencyIcon, error) {
	return wrap1(s.b.CurrencyIcons(ctx))
}

// CopyText puts `text` on the system clipboard (e.g. a listing's whisper).
func (s *Service) CopyText(text string) error { return wrap(s.b.CopyText(text)) }

// DragCard moves the price card while the user drags its header. The web view only reports the
// phase; the cursor is read on the Go side.
func (s *Service) DragCard(phase ipc.DragPhase) error { return wrap(s.b.DragCard(phase)) }

// OpenLookup opens the current item on poe2db, the wiki or Craft of Exile in the browser. Craft
// of Exile gets the game's copied item text in its URL and imports it with every affix.
func (s *Service) OpenLookup(site LookupSite, name string) error {
	return wrap(s.b.OpenLookup(site, name))
}

// OpenSettingsTab opens settings on one tab (`waystones`, `hotkeys`, ...).
func (s *Service) OpenSettingsTab(tab string) error { return wrap(s.b.OpenSettingsTab(tab)) }

// WaystoneMods lists every waystone mod RKL knows, grouped, for the Waystones settings tab.
func (s *Service) WaystoneMods(ctx context.Context) ([]WaystoneMod, error) {
	return wrap1(s.b.WaystoneMods(ctx))
}

// ClearFilterMemory forgets every remembered stat choice.
func (s *Service) ClearFilterMemory() error { return wrap(s.b.ClearFilterMemory()) }

// GetOverlayLayout is the current price panel layout, for the web view to catch up on mount.
func (s *Service) GetOverlayLayout() ipc.OverlayLayout { return s.b.GetOverlayLayout() }

// RebindHotkey changes one hotkey and applies the settings.
func (s *Service) RebindHotkey(ctx context.Context, action platform.HotkeyAction, chord platform.Chord) error {
	return wrap(s.b.RebindHotkey(ctx, action, chord))
}

// GetPermissions is the permission report for the onboarding view.
func (s *Service) GetPermissions() PermissionReport { return s.b.GetPermissions() }

// RequestPermission opens the OS prompt or settings page for one permission.
func (s *Service) RequestPermission(kind string) error { return wrap(s.b.RequestPermission(kind)) }

// RecheckPermissions checks the permissions again (after the user changed them).
func (s *Service) RecheckPermissions() PermissionReport { return s.b.RecheckPermissions() }

// InstallUpdate downloads and installs the available update, then restarts.
func (s *Service) InstallUpdate(ctx context.Context) error { return wrap(s.b.InstallUpdate(ctx)) }
