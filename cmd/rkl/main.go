// Command rkl is the Rust King Ledger desktop app: a Path of Exile 2 price-check overlay.
package main

import (
	"context"
	_ "embed"
	"fmt"
	"log/slog"
	"os"
	"slices"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"github.com/BorrowedMirror/rust-king-ledger/internal/app"
	"github.com/BorrowedMirror/rust-king-ledger/internal/config"
	"github.com/BorrowedMirror/rust-king-ledger/internal/data"
	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
	"github.com/BorrowedMirror/rust-king-ledger/internal/shell"
	"github.com/BorrowedMirror/rust-king-ledger/web"
)

var (
	//go:embed icons/icon.png
	appIcon []byte
	//go:embed icons/tray.png
	trayIcon []byte
	//go:embed icons/tray-template.png
	trayTemplateIcon []byte
)

// cli is `rkl [--trigger [--pinned]]`. --trigger asks the running instance to price-check (a
// Wayland compositor key binding when global shortcuts are unavailable); the arguments reach
// it through single-instance handling.
type cli struct{ trigger, pinned bool }

func parseArgs(args []string) cli {
	return cli{trigger: slices.Contains(args, "--trigger"), pinned: slices.Contains(args, "--pinned")}
}

func main() {
	var logFile app.LogFile
	logger := app.NewLogger(os.Stderr, &logFile)
	args := parseArgs(os.Args[1:])

	var errs []string
	paths, pathsErr := config.Resolve()
	var pathsPtr *config.Paths
	if pathsErr == nil {
		pathsPtr = &paths
	} else {
		errs = append(errs, fmt.Sprintf("no settings directory: %v", pathsErr))
	}
	settings := loadSettings(pathsPtr, &errs)

	kind := platform.Detect(platform.EnvProbeFromEnv(), settings.Platform.BackendOverride)
	// Linux: GTK must use the X11 backend for X11/XWayland before Wails initialises it.
	preInit(kind, &errs)
	parts, partsErr := newParts(kind, settings.Stash.CtrlScrollTabs)
	if partsErr != nil {
		errs = append(errs, fmt.Sprintf("platform backend %s unavailable: %v", kind, partsErr))
	}

	// The commands are bound before the app exists; their backend is filled in below.
	backend := &app.App{}
	var flow *app.Flow
	icons := shell.Icons{App: appIcon, Tray: trayIcon, TrayTemplate: trayTemplateIcon}
	s := shell.New(shell.Options{
		Assets:   web.Dist(),
		Icons:    icons,
		Logger:   logger,
		Services: []application.Service{application.NewService(app.NewService(backend))},
		// Same rule as the Tauri build: a test-hook run is never single-instance.
		SingleInstance: os.Getenv("RKL_TEST_HOOK") == "",
		OnSecondInstance: func(argv []string) {
			if flow == nil {
				return
			}
			if second := parseArgs(argv); second.trigger {
				flow.Trigger(second.pinned)
			} else if err := flow.ShowSettings("settings"); err != nil {
				flow.State().RecordError(err)
			}
		},
		OnPanic: func(d *application.PanicDetails) {
			slog.Error("panic", "err", d.Error, "stack", d.StackTrace)
		},
	})
	if args.trigger {
		// Reaching this point means no instance was running to hand --trigger to.
		fmt.Fprintln(os.Stderr, "rkl: no running instance to trigger")
		os.Exit(1)
	}
	if pathsPtr != nil {
		if path, err := logFile.Attach(paths.LogDir); err == nil {
			startupLog(path)
		} else {
			errs = append(errs, fmt.Sprintf("log file: %v", err))
		}
	}

	plat := platformFrom(kind, parts, partsErr == nil, s)
	st, err := app.NewState(pathsPtr, settings, plat, kind, data.Bundled)
	if err != nil {
		slog.Error("rkl failed to initialise", "err", err)
		os.Exit(1)
	}
	if pathsPtr != nil {
		st.LogPath = logPathOf(paths)
	}
	for _, e := range errs {
		st.RecordError(e)
	}
	st.DecodeData()

	desktop := shell.NewDesktop(s, parts)
	flow = app.NewFlow(st, desktop)
	*backend = app.App{
		Flow:        flow,
		Permissions: app.PermissionsForBackend(kind, st.Capabilities(), platform.EnvProbeFromEnv().IsGNOME(), permissionProbe(kind)),
	}
	desktop.Relabel = func(lang string) { s.SetTrayLabels(trayLabels(lang)) }

	s.BuildTray(icons, trayLabels(settings.UI.Language), shell.TrayActions{
		PriceCheck:  func() { flow.Trigger(true) },
		Settings:    func() { _ = flow.ShowSettings("settings") },
		Diagnostics: func() { _ = flow.ShowSettings("diagnostics") },
	})

	s.App.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		start(s, desktop, flow, backend, plat != nil && plat.Capabilities.GlobalHotkeys)
	})

	if err := s.App.Run(); err != nil {
		slog.Error("rkl exited", "err", err)
		os.Exit(1)
	}
}

// start runs once the event loop is up: what the Tauri build did in `setup`.
func start(s *shell.Shell, desktop *shell.Desktop, flow *app.Flow, backend *app.App, hotkeysOK bool) {
	st := flow.State()
	if styler, err := desktop.Styler(s.Overlay); err == nil {
		if err := styler.Prepare(); err != nil {
			st.RecordError(fmt.Sprintf("overlay styling failed: %v", err))
		}
	}
	// Portal registration can block for seconds; keep the UI thread free.
	go func() {
		st.ApplyLock.Lock()
		settings := st.Settings()
		err := flow.RegisterHotkeys(&settings)
		st.ApplyLock.Unlock()
		ok := hotkeysOK && err == nil
		if err != nil {
			st.RecordError(fmt.Sprintf("hotkeys: %v", err))
		}
		// No tray or no hotkey: the settings window is the only way in.
		if app.MustShowSettings(s.Tray != nil, ok) {
			_ = flow.ShowSettings("settings")
		}
	}()
	flow.RunHotkeyLoop()
	flow.RunInputLoop()
	flow.RunGameFollower()
	settings := st.Settings()
	flow.ApplyPanelScale(settings.Overlay.Scale)
	// Keep the login entry pointing at this executable (an AppImage can be moved).
	if settings.UI.Autostart {
		if err := app.SetAutostart(true); err != nil {
			st.RecordError(fmt.Sprintf("start at login: %v", err))
		}
	}
	startSignalTriggers(flow)
	startTestHook(s, flow)
	if !settings.UI.StartMinimised {
		_ = flow.ShowSettings("settings")
	}
	// First run or required OS permissions missing: open `#/onboarding` instead.
	if app.NeedsOnboarding(settings.UI.OnboardingDone, backend.Permissions.Current()) {
		if err := flow.ShowSettings("onboarding"); err != nil {
			st.RecordError(fmt.Sprintf("onboarding: %v", err))
		}
	}
	go func() {
		if _, err := backend.ListLeagues(appContext()); err != nil {
			st.RecordError(fmt.Sprintf("league auto-detect failed: %v", err))
		}
	}()
}

func loadSettings(paths *config.Paths, errs *[]string) config.Settings {
	if paths == nil {
		return config.DefaultSettings()
	}
	// Settings saved before the rename (kalandra) are copied over once.
	if _, set := os.LookupEnv(config.ConfigDirEnv); !set {
		if legacy, ok := config.LegacySettingsFile(); ok {
			if copied, err := config.AdoptLegacySettings(*paths, legacy); err != nil {
				*errs = append(*errs, fmt.Sprintf("could not copy previous kalandra settings: %v", err))
			} else if copied {
				slog.Info("copied settings from the previous kalandra install")
			}
		}
	}
	s, err := config.Load(*paths)
	if err != nil {
		*errs = append(*errs, fmt.Sprintf("settings could not be loaded, using defaults: %v", err))
		return config.DefaultSettings()
	}
	return s
}

// platformFrom completes the backend's parts with what Wails provides on every OS: global
// hotkeys and the clipboard.
func platformFrom(kind platform.BackendKind, parts platform.Parts, ok bool, s *shell.Shell) *platform.Platform {
	if !ok {
		return nil
	}
	caps := parts.Capabilities
	caps.GlobalHotkeys = true
	caps.Clipboard = true
	return &platform.Platform{
		Kind:           kind,
		Capabilities:   caps,
		WindowTracker:  parts.WindowTracker,
		Hotkeys:        shell.NewHotkeys(s.App),
		InputListener:  parts.InputListener,
		InputSimulator: parts.InputSimulator,
		Clipboard:      shell.NewClipboard(s.App),
	}
}

func trayLabels(language string) shell.TrayLabels {
	l := app.LoadTrayLabels(language)
	return shell.TrayLabels{Tooltip: l.Tooltip, PriceCheck: l.PriceCheck, Settings: l.Settings, Diagnostics: l.Diagnostics, Quit: l.Quit}
}

func logPathOf(p config.Paths) string { return p.LogDir + string(os.PathSeparator) + "rkl.log" }

func startupLog(path string) {
	slog.Info("rkl starting", "version", app.Version, "log", path)
}

// appContext is the context for background work started at launch.
func appContext() context.Context { return context.Background() }
