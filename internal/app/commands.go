package app

import (
	"context"
	"runtime"
	"strings"
	"sync"

	"github.com/BorrowedMirror/divvy/internal/config"
	"github.com/BorrowedMirror/divvy/internal/core"
	"github.com/BorrowedMirror/divvy/internal/ipc"
	"github.com/BorrowedMirror/divvy/internal/platform"
	"github.com/BorrowedMirror/divvy/internal/trade"
)

// Updater is what the commands need from the auto-updater.
type Updater interface {
	InstallUpdate(ctx context.Context) error
}

// App is the running app behind the bound [Service]: the flow, the permission state and the
// auto-updater. The commands' behaviour lives in its methods.
type App struct {
	*Flow
	permissions *Permissions

	mu      sync.Mutex
	updater Updater // nil until the updater has started (or in builds without updates)
}

// NewApp builds the app on flow.
func NewApp(flow *Flow, permissions *Permissions) *App {
	return &App{Flow: flow, permissions: permissions}
}

// Permissions is the permission state.
func (a *App) Permissions() *Permissions { return a.permissions }

// SetUpdater makes the updater available to the install command.
func (a *App) SetUpdater(u Updater) {
	a.mu.Lock()
	a.updater = u
	a.mu.Unlock()
}

// ListLeagues lists the trade site's leagues and remembers them for league auto-detection.
func (a *App) ListLeagues(ctx context.Context) ([]trade.League, error) {
	leagues, err := a.st.priceSources().trade.Leagues(ctx)
	if err != nil {
		return nil, err
	}
	a.st.setLeagues(leagues)
	return leagues, nil
}

// osName is the OS name the diagnostics page shows (the frontend expects `macos`).
func osName() string {
	if runtime.GOOS == "darwin" {
		return "macos"
	}
	return runtime.GOOS
}

// diagnostics is what the diagnostics page shows.
func (a *App) diagnostics() ipc.Diagnostics {
	st := a.st
	d := ipc.Diagnostics{
		Version:      Version,
		OS:           osName(),
		Backend:      st.backend,
		Capabilities: st.capabilitiesWithNotes(),
		DataVersion:  st.dataVersion(),
		League:       new(st.league()),
		RecentErrors: st.recentErrors(),
	}
	if st.paths != nil {
		d.SettingsPath = new(st.paths.SettingsFile)
	}
	if st.logPath != "" {
		d.LogPath = new(st.logPath)
	}
	return d
}

// priceCheck parses request.Text, or runs the copy flow against the game when it is nil.
func (a *App) priceCheck(ctx context.Context, request ipc.PriceCheckRequest) (ipc.PriceCheckResponse, error) {
	var (
		item    *core.ParsedItem
		filters core.FilterSet
		err     error
	)
	if request.Text != nil {
		item, filters, err = a.handleItemText(*request.Text, request.Pinned)
	} else {
		item, filters, err = a.runPriceCheck(ctx, request.Pinned)
	}
	if err != nil {
		return ipc.PriceCheckResponse{}, err
	}
	return ipc.PriceCheckResponse{Item: *item, Filters: filters}, nil
}

// queryPrice prices the last copied item with these filters.
func (a *App) queryPrice(ctx context.Context, filters core.FilterSet) (core.PriceSummary, error) {
	st := a.st
	last, ok := st.lastItem()
	if !ok {
		return core.PriceSummary{}, ipc.NewError(ipc.KindNoItem, "no item has been copied yet")
	}
	settings := st.Settings()
	sources := st.priceSources()
	if settings.Trade.RememberFilters {
		st.rememberFilters(&filters)
	}
	summary, err := sources.pick(settings.Trade.Source, &last.item).Price(ctx, &trade.PriceRequest{Item: last.item, Filters: filters})
	if err != nil {
		st.RecordError("price query", err)
		return core.PriceSummary{}, err
	}
	summary = withRatesIfWanted(ctx, sources, &settings, filters.League, summary)
	st.setLastPrice(summary)
	return summary, nil
}

// withRatesIfWanted converts trade-site results in several currencies with poe2scout's rates,
// so min/median/max include every listing and each one shows its divine/exalted equivalent.
// Best effort: without rates the summary is returned unchanged.
func withRatesIfWanted(ctx context.Context, sources *priceSources, settings *config.Settings, league string, summary core.PriceSummary) core.PriceSummary {
	if !settings.Trade.ConvertPrices || summary.Source != core.SourceTrade || len(summary.Listings) == 0 {
		return summary
	}
	rates, err := sources.scout.Rates(ctx, league)
	if err != nil {
		return summary
	}
	return core.WithRates(summary, &rates)
}

// copyText puts text on the system clipboard (a listing's whisper).
func (a *App) copyText(text string) error {
	p := a.st.platform
	if p == nil {
		return ipc.NewError(ipc.KindPlatform, "no clipboard on this platform")
	}
	if err := p.Clipboard.SetText(text); err != nil {
		return ipc.Errorf(ipc.KindPlatform, "%w", err)
	}
	return nil
}

// openLookup opens the current item on poe2db, the wiki or Craft of Exile in the browser.
func (a *App) openLookup(site LookupSite, name string) error {
	u, ok := lookupURL(site, name, a.st.lastItemText())
	if !ok {
		return ipc.NewError(ipc.KindInternal, "nothing to look up")
	}
	if err := a.d.OpenURL(u); err != nil {
		return ipc.Errorf(ipc.KindInternal, "%w", err)
	}
	return nil
}

// overlaySnapshot is the last copied item as an `item-copied` payload.
func (a *App) overlaySnapshot() *ipc.ItemCopied {
	last, ok := a.st.lastItem()
	if !ok {
		return nil
	}
	return &ipc.ItemCopied{Item: last.item, Filters: last.filters, Pinned: a.st.pinned.Load()}
}

// waystoneMods is the waystone mod catalogue from the bundled stats.
func (a *App) waystoneMods() []WaystoneMod {
	gameData := a.st.waitData()
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
	return waystoneCatalog(texts)
}

// rebindHotkey changes one hotkey and applies the settings.
func (a *App) rebindHotkey(action platform.HotkeyAction, chord platform.Chord) error {
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
			return ipc.NewError(ipc.KindConfig, "no such chat command")
		}
		settings.Chat.Commands[action.Index].Chord = chord
	case platform.StashSearch:
		if int(action.Index) >= len(settings.Stash.Searches) {
			return ipc.NewError(ipc.KindConfig, "no such stash search")
		}
		settings.Stash.Searches[action.Index].Chord = chord
	}
	_, err := a.applySettings(settings)
	return err
}

// recheckPermissions checks the permissions again and broadcasts a change.
func (a *App) recheckPermissions() ipc.PermissionReport {
	report, changed := a.permissions.recheck()
	if changed {
		a.emit(ipc.EventPermissionsChanged, ipc.PermissionsChanged{Report: report})
	}
	return report
}

// installUpdate downloads and installs the available update, then restarts.
func (a *App) installUpdate(ctx context.Context) error {
	a.mu.Lock()
	u := a.updater
	a.mu.Unlock()
	if u == nil {
		return ipc.NewError(ipc.KindUpdate, "updates are not available in this build")
	}
	return u.InstallUpdate(ctx)
}
