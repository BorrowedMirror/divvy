// Command rkl is the Rust King Ledger desktop app: a Path of Exile 2 price-check overlay.
package main

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"slices"
	"sync/atomic"
	"syscall"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"github.com/BorrowedMirror/rust-king-ledger/internal/app"
	"github.com/BorrowedMirror/rust-king-ledger/internal/autostart"
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
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx, os.Args[1:])
	stop()
	if err != nil {
		slog.Error("rkl exited", "err", err)
		os.Exit(1)
	}
}

// run starts the app and returns when it quits. Everything started in the background runs
// until ctx is done or the app shuts down.
func run(ctx context.Context, argv []string) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	// SIGUSR1/SIGUSR2 are taken first: unhandled, either would end the process, so a trigger
	// during startup must not reach the default action. They run once the app has started.
	triggers := platform.SignalTriggers(ctx)

	var logFile app.LogFile
	defer logFile.Close()
	logger := app.NewLogger(app.LogLevel(os.Getenv("RKL_LOG")), os.Stderr, &logFile)
	slog.SetDefault(logger)
	args := parseArgs(argv)

	// Problems before the state exists are recorded once it does (diagnostics).
	var startErrs []error
	var paths *config.Paths
	if p, err := config.Resolve(); err == nil {
		paths = &p
	} else {
		startErrs = append(startErrs, fmt.Errorf("no settings directory: %w", err))
	}
	settings, err := loadSettings(paths)
	if err != nil {
		startErrs = append(startErrs, err)
	}

	var override platform.BackendKind
	if o := settings.Platform.BackendOverride; o != nil {
		override = *o
	}
	kind := platform.Detect(platform.EnvProbeFromEnv(), override)
	// Linux: GTK must use the X11 backend for X11/XWayland before Wails initialises it.
	if err := preInit(kind); err != nil {
		startErrs = append(startErrs, err)
	}
	parts, partsErr := newParts(kind, settings.Stash.CtrlScrollTabs)
	if partsErr != nil {
		startErrs = append(startErrs, fmt.Errorf("platform backend %s unavailable: %w", kind, partsErr))
	}

	// A second launch can arrive (on a Wails goroutine) before the flow exists.
	var flow atomic.Pointer[app.Flow]
	icons := shell.Icons{App: appIcon, Tray: trayIcon, TrayTemplate: trayTemplateIcon}
	s := shell.New(shell.Options{
		Assets: web.Dist(),
		Icons:  icons,
		Logger: logger,
		// A test-hook run is never single-instance, so it can start next to an installed RKL.
		SingleInstance: os.Getenv("RKL_TEST_HOOK") == "",
		OnSecondInstance: func(argv []string) {
			f := flow.Load()
			if f == nil {
				return
			}
			if second := parseArgs(argv); second.trigger {
				f.Trigger(second.pinned)
			} else {
				f.ShowSettings("settings")
			}
		},
		OnPanic: func(d *application.PanicDetails) {
			slog.Error("panic", "err", d.Error, "stack", d.StackTrace)
		},
	})
	if args.trigger {
		// Reaching this point means no instance was running to hand --trigger to.
		return errors.New("no running instance to trigger")
	}
	var logPath string
	if paths != nil {
		if logPath, err = logFile.Attach(paths.LogDir); err != nil {
			startErrs = append(startErrs, fmt.Errorf("log file: %w", err))
		}
	}
	slog.Info("rkl starting", "version", app.Version, "log", logPath)

	plat := platformFrom(ctx, kind, parts, partsErr == nil, s)
	st, err := app.NewState(app.StateOptions{
		Paths: paths, Settings: settings, Platform: plat, Backend: kind, GameData: data.Bundled, LogPath: logPath,
	})
	if err != nil {
		return fmt.Errorf("initialise: %w", err)
	}
	for _, e := range startErrs {
		st.RecordError("startup", e)
	}
	st.DecodeData()

	desktop := shell.NewDesktop(s, parts)
	f := app.NewFlow(ctx, st, desktop)
	a := app.NewApp(f, app.PermissionsForBackend(kind, st.Capabilities(), platform.EnvProbeFromEnv().IsGNOME(), permissionProbe(kind)))
	s.App.RegisterService(application.NewService(app.NewService(a)))
	flow.Store(f)
	desktop.Relabel = func(lang string) { s.SetTrayLabels(app.LoadTrayLabels(lang)) }

	s.BuildTray(icons, app.LoadTrayLabels(settings.UI.Language), shell.TrayActions{
		PriceCheck:  func() { f.Trigger(true) },
		Settings:    func() { f.ShowSettings("settings") },
		Diagnostics: func() { f.ShowSettings("diagnostics") },
	})

	// Hotkeys follow focus: grabbed while the game or an RKL window is focused. The game's focus
	// comes from the tracker; RKL's own windows report theirs here. Wails emits the common focus
	// events on Windows and macOS only; on Linux it sends its own FocusIn/FocusOut and never maps
	// them, so both kinds are listened to. Off the UI thread: syncing can wait on the settings lock.
	focusEvents := []events.WindowEventType{
		events.Common.WindowFocus, events.Common.WindowLostFocus,
		events.Linux.WindowFocusIn, events.Linux.WindowFocusOut,
	}
	for _, w := range []*application.WebviewWindow{s.Overlay, s.Settings} {
		for _, ev := range focusEvents {
			w.OnWindowEvent(ev, func(*application.WindowEvent) { go f.SyncHotkeysToFocus() })
		}
	}

	// The borderless settings window's rounded corners follow its size when the window
	// manager resizes it. Wails emits the common event on Windows and macOS, its own on Linux.
	for _, ev := range []events.WindowEventType{events.Common.WindowDidResize, events.Linux.WindowDidResize} {
		s.Settings.OnWindowEvent(ev, func(*application.WindowEvent) { go f.SettingsResized() })
	}

	// Quitting (tray, test hook, signal) stops the background work before the windows go.
	s.App.OnShutdown(cancel)
	s.App.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		start(ctx, s, desktop, a, triggers, plat != nil && plat.Capabilities.GlobalHotkeys)
	})
	return s.App.Run()
}

// start runs once the event loop is up: hotkeys, the updater, the platform event loops, the
// test hook and the first window.
func start(ctx context.Context, s *shell.Shell, desktop *shell.Desktop, a *app.App, triggers <-chan platform.HotkeyEvent, hotkeysOK bool) {
	st := a.State()
	if styler, err := desktop.Styler(s.Overlay); err == nil {
		if err := styler.Prepare(); err != nil {
			st.RecordError("overlay styling failed", err)
		}
	}
	if err := desktop.PrepareNotice(); err != nil && !errors.Is(err, platform.ErrUnsupported) {
		st.RecordError("notice styling failed", err)
	}
	// Portal registration can block for seconds; keep the UI thread free.
	go func() {
		err := a.RegisterSettingsHotkeys()
		if err != nil {
			st.RecordError("hotkeys", err)
		}
		// No tray or no hotkey: the settings window is the only way in.
		if app.MustShowSettings(s.Tray != nil, hotkeysOK && err == nil) {
			a.ShowSettings("settings")
		}
	}()
	if up := shell.StartUpdater(ctx, s.App, st, desktop.Emit); up != nil {
		a.SetUpdater(up)
	}
	a.RunHotkeyLoop(ctx)
	a.RunInputLoop(ctx)
	a.RunGameFollower(ctx)
	settings := st.Settings()
	a.ApplyPanelScale(settings.Overlay.Scale)
	// Keep the login entry pointing at this executable (an AppImage can be moved).
	if settings.UI.Autostart {
		if err := autostart.Set(true); err != nil {
			st.RecordError("start at login", err)
		}
	}
	startSignalTriggers(a.Flow, triggers)
	startTestHook(ctx, s, a.Flow)
	if !settings.UI.StartMinimised {
		a.ShowSettings("settings")
	}
	// First run or required OS permissions missing: open `#/onboarding` instead.
	if app.NeedsOnboarding(settings.UI.OnboardingDone, a.Permissions().Current()) {
		a.ShowSettings("onboarding")
	}
	go func() {
		if _, err := a.ListLeagues(ctx); err != nil && ctx.Err() == nil {
			st.RecordError("league auto-detect failed", err)
		}
	}()
}

// loadSettings loads the settings file, copying settings saved before the rename (kalandra)
// over once. Without a settings directory, or when the file cannot be loaded, it returns the
// defaults; err then says what went wrong.
func loadSettings(paths *config.Paths) (config.Settings, error) {
	if paths == nil {
		return config.DefaultSettings(), nil
	}
	var adoptErr error
	if _, set := os.LookupEnv(config.ConfigDirEnv); !set {
		if legacy, ok := config.LegacySettingsFile(); ok {
			copied, err := config.AdoptLegacySettings(*paths, legacy)
			switch {
			case err != nil:
				adoptErr = fmt.Errorf("could not copy previous kalandra settings: %w", err)
			case copied:
				slog.Info("copied settings from the previous kalandra install")
			}
		}
	}
	s, err := config.Load(*paths)
	if err != nil {
		return config.DefaultSettings(), errors.Join(adoptErr, fmt.Errorf("settings could not be loaded, using defaults: %w", err))
	}
	return s, adoptErr
}

// platformFrom completes the backend's parts with what Wails provides on every OS: global
// hotkeys and the clipboard.
func platformFrom(ctx context.Context, kind platform.BackendKind, parts platform.Parts, ok bool, s *shell.Shell) *platform.Platform {
	if !ok {
		return nil
	}
	hotkeys, notes := globalHotkeys(ctx, kind)
	parts.Capabilities.GlobalHotkeys = hotkeys
	parts.Capabilities.Notes = append(parts.Capabilities.Notes, notes...)
	parts.Capabilities.Clipboard = true
	// The backend's own clipboard when it has one (Wayland-native), else Wails'.
	if parts.Clipboard == nil {
		parts.Clipboard = shell.NewClipboard(s.App)
	}
	return &platform.Platform{Parts: parts, Hotkeys: shell.NewHotkeys(s.App, kind == platform.BackendX11)}
}
