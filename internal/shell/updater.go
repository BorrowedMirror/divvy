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

	"github.com/BorrowedMirror/divvy/internal/app"
	"github.com/BorrowedMirror/divvy/internal/ipc"
)

// Set by the linker in release builds (-ldflags -X, see cmd/release). Without a public key the
// updater stays off, so dev builds and forks never check for updates.
var (
	// UpdaterPublicKey is the base64 raw ed25519 public key every update must be signed with.
	UpdaterPublicKey = ""
	// UpdateManifestURL is the update manifest (Wails' endpoint protocol). GitHub's "latest"
	// skips drafts and pre-releases, so installs only see a release once it is published.
	UpdateManifestURL = "https://github.com/BorrowedMirror/divvy/releases/latest/download/update.json"
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

// publicKey decodes UpdaterPublicKey; it is nil when this build has none.
func publicKey() ([]byte, error) {
	s := strings.TrimSpace(UpdaterPublicKey)
	if s == "" {
		return nil, nil
	}
	key, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("updater public key: %w", err)
	}
	return key, nil
}

// StartUpdater configures Wails' updater and, until ctx is done, checks for updates in the
// background, if this build enables updates. It returns nil when it does not.
func StartUpdater(ctx context.Context, a *application.App, st *app.State, emit func(string, any)) *Updater {
	key, err := publicKey()
	if err != nil {
		st.RecordError("updater unavailable", err)
		return nil
	}
	if key == nil {
		slog.Debug("updater disabled: no public key in this build")
		return nil
	}
	provider, err := endpoint.New(endpoint.Config{URL: UpdateManifestURL})
	if err != nil {
		st.RecordError("updater unavailable", err)
		return nil
	}
	if err := a.Updater.Init(updater.Config{
		CurrentVersion: app.Version,
		Providers:      []updater.Provider{provider},
		PublicKey:      key,
		// The settings window's update banner is the UI.
		Window: updater.WindowNone,
	}); err != nil {
		st.RecordError("updater unavailable", err)
		return nil
	}
	up := &Updater{u: a.Updater, st: st, emit: emit}
	go up.loop(ctx)
	return up
}

// loop checks after firstCheckDelay and then every checkInterval, while the user has update
// checks on, until ctx is done.
func (up *Updater) loop(ctx context.Context) {
	timer := time.NewTimer(firstCheckDelay)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		if up.st.Settings().UI.CheckUpdates {
			up.checkAndNotify(ctx)
		}
		timer.Reset(checkInterval)
	}
}

func (up *Updater) checkAndNotify(ctx context.Context) {
	rel, err := up.u.Check(ctx)
	if err != nil {
		if ctx.Err() == nil {
			up.st.RecordError("update check failed", err)
		}
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
	up.emit(ipc.EventUpdateAvailable, announcement(rel))
}

// announcement is the `update-available` payload for a release.
func announcement(rel *updater.Release) ipc.UpdateAvailable {
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
		return ipc.NewError(ipc.KindUpdate, "no update is available")
	}
	if err := up.u.DownloadAndInstall(ctx); err != nil {
		if errors.Is(err, context.Canceled) {
			return err
		}
		return ipc.Errorf(ipc.KindUpdate, "update failed: %w", err)
	}
	if err := up.u.Restart(ctx); err != nil {
		return ipc.Errorf(ipc.KindUpdate, "restart into the update: %w", err)
	}
	return nil
}
