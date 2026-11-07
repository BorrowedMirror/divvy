package shell

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/updater"
	"github.com/wailsapp/wails/v3/pkg/updater/providers/endpoint"

	"github.com/BorrowedMirror/rust-king-ledger/internal/app"
	"github.com/BorrowedMirror/rust-king-ledger/internal/ipc"
)

// Linked in by release builds with -ldflags -X. Without a public key the updater stays off, so
// dev builds and forks never check for updates (as with the Tauri build's updater pubkey).
var (
	// UpdaterPublicKey is the base64 raw ed25519 public key every update must be signed with.
	UpdaterPublicKey = ""
	// UpdateManifestURL is the update manifest (Wails' endpoint protocol). GitHub's "latest"
	// skips drafts and pre-releases, so installs only see a release once it is published.
	UpdateManifestURL = "https://github.com/BorrowedMirror/rust-king-ledger/releases/latest/download/update.json"
)

const (
	// firstCheckDelay keeps startup (hotkeys, league detection) fast.
	firstCheckDelay = 30 * time.Second
	checkInterval   = 24 * time.Hour
)

// Updater checks for signed updates in the background and installs one on request.
type Updater struct {
	u    *updater.Updater
	st   *app.State
	emit func(event string, payload any)

	mu      sync.Mutex
	pending *updater.Release
}

// PublicKey decodes UpdaterPublicKey; ok is false when this build has none.
func PublicKey() (key []byte, ok bool, err error) {
	s := strings.TrimSpace(UpdaterPublicKey)
	if s == "" {
		return nil, false, nil
	}
	key, err = base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, false, fmt.Errorf("updater public key: %w", err)
	}
	return key, true, nil
}

// StartUpdater configures Wails' updater and starts the periodic check, if this build
// enables updates. It returns nil when it does not.
func StartUpdater(a *application.App, st *app.State, emit func(string, any)) *Updater {
	key, ok, err := PublicKey()
	if err != nil {
		st.RecordError(err)
		return nil
	}
	if !ok {
		slog.Debug("updater disabled: no public key in this build")
		return nil
	}
	provider, err := endpoint.New(endpoint.Config{URL: UpdateManifestURL})
	if err != nil {
		st.RecordError(fmt.Sprintf("updater unavailable: %v", err))
		return nil
	}
	if err := a.Updater.Init(updater.Config{
		CurrentVersion: app.Version,
		Providers:      []updater.Provider{provider},
		PublicKey:      key,
		// The settings window's update banner is the UI, as before.
		Window: updater.WindowNone,
	}); err != nil {
		st.RecordError(fmt.Sprintf("updater unavailable: %v", err))
		return nil
	}
	up := &Updater{u: a.Updater, st: st, emit: emit}
	go up.loop()
	return up
}

func (up *Updater) loop() {
	time.Sleep(firstCheckDelay)
	for {
		if up.st.Settings().UI.CheckUpdates {
			up.checkAndNotify(context.Background())
		}
		time.Sleep(checkInterval)
	}
}

func (up *Updater) checkAndNotify(ctx context.Context) {
	rel, err := up.u.Check(ctx)
	if err != nil {
		slog.Warn("update check failed", "err", err)
		up.st.RecordError(fmt.Sprintf("update check failed: %v", err))
		return
	}
	if rel == nil {
		slog.Debug("no update available")
		return
	}
	slog.Info("update available", "version", rel.Version)
	up.mu.Lock()
	up.pending = rel
	up.mu.Unlock()
	up.emit(ipc.EventUpdateAvailable, Announcement(rel))
}

// Announcement is the `update-available` payload for a release.
func Announcement(rel *updater.Release) ipc.UpdateAvailable {
	a := ipc.UpdateAvailable{Version: rel.Version}
	if notes := strings.TrimSpace(rel.Notes); notes != "" {
		a.Notes = new(rel.Notes)
	}
	if !rel.PublishedAt.IsZero() {
		a.Date = new(rel.PublishedAt.UTC().Format(time.RFC3339))
	}
	return a
}

// InstallUpdate downloads and verifies the announced update, then restarts into it.
func (up *Updater) InstallUpdate(ctx context.Context) error {
	up.mu.Lock()
	pending := up.pending
	up.mu.Unlock()
	if pending == nil {
		return ipc.NewError("update", "no update is available")
	}
	if err := up.u.DownloadAndInstall(ctx); err != nil {
		if errors.Is(err, context.Canceled) {
			return err
		}
		return ipc.Errorf("update", "update failed: %v", err)
	}
	if err := up.u.Restart(ctx); err != nil {
		return ipc.Errorf("update", "restart into the update: %v", err)
	}
	return nil
}
