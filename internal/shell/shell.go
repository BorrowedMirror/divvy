// Package shell builds the Wails application: the two web views (price panel overlay and
// settings), the tray and single-instance handling. Business logic lives in the other internal
// packages; this package only wires them to Wails.
package shell

import (
	"io/fs"
	"log/slog"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// Window names, matching the routes the frontend reads from the URL hash.
const (
	Overlay  = "overlay"
	Settings = "settings"
)

// AppID is the identifier the Tauri build used ("app.rkl"); single-instance locking and the
// GTK application id use it so both builds behave as the same app.
const AppID = "app.rkl"

// Icons the shell needs.
type Icons struct {
	App          []byte
	Tray         []byte
	TrayTemplate []byte // macOS menu-bar template image
}

// Options configures [New].
type Options struct {
	Assets fs.FS
	Icons  Icons
	Logger *slog.Logger
	// SingleInstance makes a second launch hand its arguments to the running instance and exit.
	// Off for test-hook runs, so a test instance can start next to an installed RKL.
	SingleInstance bool
	// OnPanic is called with a panic Wails recovered (a bound method or event handler).
	OnPanic func(details *application.PanicDetails)
	// Services are the bound IPC services.
	Services []application.Service
	// OnSecondInstance runs in the first instance when RKL is launched again, with the second
	// launch's arguments (for example `--trigger`).
	OnSecondInstance func(args []string)
}

// Shell holds the Wails application and its windows.
type Shell struct {
	App      *application.App
	Overlay  *application.WebviewWindow
	Settings *application.WebviewWindow
	Tray     *application.SystemTray

	trayActions TrayActions
}

// New creates the application and its windows. Both windows start hidden, as in the Tauri
// build; the flow shows them when needed.
func New(opts Options) *Shell {
	s := &Shell{}
	var single *application.SingleInstanceOptions
	if opts.SingleInstance {
		single = &application.SingleInstanceOptions{
			UniqueID: AppID,
			OnSecondInstanceLaunch: func(d application.SecondInstanceData) {
				if opts.OnSecondInstance != nil {
					opts.OnSecondInstance(d.Args)
				}
			},
		}
	}
	s.App = application.New(application.Options{
		Name:        "Rust King Ledger",
		Description: "Path of Exile 2 price-check overlay",
		Icon:        opts.Icons.App,
		Logger:      opts.Logger,
		Services:    opts.Services,
		PanicHandler: func(d *application.PanicDetails) {
			if opts.OnPanic != nil {
				opts.OnPanic(d)
			}
		},
		Assets:         application.AssetOptions{Handler: application.AssetFileServerFS(opts.Assets)},
		SingleInstance: single,
		// RKL lives in the tray: closing the settings window must not quit it.
		Linux:   application.LinuxOptions{DisableQuitOnLastWindowClosed: true, ApplicationID: AppID, ProgramName: "rkl"},
		Mac:     application.MacOptions{ApplicationShouldTerminateAfterLastWindowClosed: false, ActivationPolicy: application.ActivationPolicyAccessory},
		Windows: application.WindowsOptions{DisableQuitOnLastWindowClosed: true},
	})

	s.Overlay = s.App.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             Overlay,
		Title:            "RKL overlay",
		URL:              "/#/overlay",
		Width:            420,
		Height:           560,
		Hidden:           true,
		Frameless:        true,
		AlwaysOnTop:      true,
		BackgroundType:   application.BackgroundTypeTransparent,
		BackgroundColour: application.NewRGBA(0, 0, 0, 0),
		Windows:          application.WindowsWindow{HiddenOnTaskbar: true},
		Mac: application.MacWindow{
			WindowClass:      application.MacWindowClassPanel,
			PanelPreferences: application.MacPanelPreferences{NonActivating: true},
		},
	})

	s.Settings = s.App.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             Settings,
		Title:            "RKL settings",
		URL:              "/#/settings",
		Width:            900,
		Height:           680,
		Hidden:           true,
		BackgroundType:   application.BackgroundTypeTransparent,
		BackgroundColour: application.NewRGBA(0, 0, 0, 0),
	})
	// The close button hides the settings window; quitting is the tray's job.
	s.Settings.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		e.Cancel()
		s.Settings.Hide()
	})
	return s
}

// TrayLabels are the tray menu's texts, in the user's language.
type TrayLabels struct {
	Tooltip, PriceCheck, Settings, Diagnostics, Quit string
}

// TrayActions are what the tray menu items do.
type TrayActions struct {
	PriceCheck, Settings, Diagnostics func()
}

// BuildTray adds the tray icon and its menu: price check, settings, diagnostics, quit.
func (s *Shell) BuildTray(icons Icons, labels TrayLabels, actions TrayActions) {
	menu := s.App.NewMenu()
	menu.Add(labels.PriceCheck).OnClick(func(*application.Context) { call(actions.PriceCheck) })
	menu.Add(labels.Settings).OnClick(func(*application.Context) { call(actions.Settings) })
	menu.Add(labels.Diagnostics).OnClick(func(*application.Context) { call(actions.Diagnostics) })
	menu.AddSeparator()
	menu.Add(labels.Quit).OnClick(func(*application.Context) { s.App.Quit() })

	s.Tray = s.App.SystemTray.New()
	s.Tray.SetIcon(icons.Tray)
	s.Tray.SetTemplateIcon(icons.TrayTemplate)
	s.Tray.SetTooltip(labels.Tooltip)
	s.Tray.SetMenu(menu)
	s.trayActions = actions
}

// SetTrayLabels relabels the tray menu (the UI language changed).
func (s *Shell) SetTrayLabels(labels TrayLabels) {
	if s.Tray == nil {
		return
	}
	a := s.trayActions
	menu := s.App.NewMenu()
	menu.Add(labels.PriceCheck).OnClick(func(*application.Context) { call(a.PriceCheck) })
	menu.Add(labels.Settings).OnClick(func(*application.Context) { call(a.Settings) })
	menu.Add(labels.Diagnostics).OnClick(func(*application.Context) { call(a.Diagnostics) })
	menu.AddSeparator()
	menu.Add(labels.Quit).OnClick(func(*application.Context) { s.App.Quit() })
	s.Tray.SetMenu(menu)
	s.Tray.SetTooltip(labels.Tooltip)
}

func call(f func()) {
	if f != nil {
		f()
	}
}
