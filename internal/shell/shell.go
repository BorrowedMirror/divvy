package shell

import (
	"io/fs"
	"log/slog"
	"runtime"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"github.com/BorrowedMirror/divvy/internal/app"
	"github.com/BorrowedMirror/divvy/internal/product"
)

// Window names, matching the routes the frontend reads from the URL hash.
const (
	overlayWindow  = "overlay"
	settingsWindow = "settings"
	noticeWindow   = "notice"
)

// Titles of the frameless windows, as window lists and screen readers name them.
const (
	overlayTitle = "Divvy price check"
	noticeTitle  = "Divvy notice"
)

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
	// Off for test-hook runs, so a test instance can start next to an installed Divvy.
	SingleInstance bool
	// OnPanic is called with a panic Wails recovered (a bound method or event handler).
	OnPanic func(details *application.PanicDetails)
	// Services are the bound IPC services.
	Services []application.Service
	// OnSecondInstance runs in the first instance when Divvy is launched again, with the second
	// launch's arguments (for example `--trigger`).
	OnSecondInstance func(args []string)
}

// Shell holds the Wails application and its windows.
type Shell struct {
	App      *application.App
	Overlay  *application.WebviewWindow
	Settings *application.WebviewWindow
	// Notice is the ready notice over the game window; it never takes focus or mouse input
	// (cmd/divvy styles it at startup).
	Notice *application.WebviewWindow
	Tray   *application.SystemTray

	// toolTitles are the titles of the frameless helper windows (overlay, notice): Wails sets
	// no title on frameless windows, and they stay out of taskbars and window lists.
	toolTitles map[*application.WebviewWindow]string

	trayActions TrayActions
}

// New creates the application and its windows. Both windows start hidden; the flow shows
// them when needed.
func New(opts Options) *Shell {
	s := &Shell{}
	var single *application.SingleInstanceOptions
	if opts.SingleInstance {
		single = &application.SingleInstanceOptions{
			UniqueID: product.ID,
			OnSecondInstanceLaunch: func(d application.SecondInstanceData) {
				if opts.OnSecondInstance != nil {
					opts.OnSecondInstance(d.Args)
				}
			},
		}
	}
	s.App = application.New(application.Options{
		Name:        product.Name,
		Description: product.Description,
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
		// Divvy lives in the tray: closing the settings window must not quit it.
		Linux:   application.LinuxOptions{DisableQuitOnLastWindowClosed: true, ApplicationID: product.ID, ProgramName: product.Binary},
		Mac:     application.MacOptions{ApplicationShouldTerminateAfterLastWindowClosed: false, ActivationPolicy: application.ActivationPolicyAccessory},
		Windows: application.WindowsOptions{DisableQuitOnLastWindowClosed: true},
	})

	s.Overlay = s.App.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             overlayWindow,
		Title:            overlayTitle,
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
		Name:             settingsWindow,
		Title:            "Divvy settings",
		URL:              "/#/settings",
		Width:            900,
		Height:           680,
		Hidden:           true,
		BackgroundType:   application.BackgroundTypeTransparent,
		BackgroundColour: application.NewRGBA(0, 0, 0, 0),
	})
	s.Notice = s.App.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             noticeWindow,
		Title:            noticeTitle,
		URL:              "/#/notice",
		Width:            300,
		Height:           64,
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

	// The close button hides the settings window; quitting is the tray's job.
	s.Settings.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		e.Cancel()
		s.Settings.Hide()
	})
	s.toolTitles = map[*application.WebviewWindow]string{s.Overlay: overlayTitle, s.Notice: noticeTitle}
	return s
}

// TrayActions are what the tray menu items do.
type TrayActions struct {
	PriceCheck, Settings, Diagnostics func()
}

// BuildTray adds the tray icon and its menu: price check, settings, diagnostics, quit.
func (s *Shell) BuildTray(icons Icons, labels app.TrayLabels, actions TrayActions) {
	s.trayActions = actions
	s.Tray = s.App.SystemTray.New()
	// Dedicated small glyphs: the full app tile is too detailed at tray size. macOS gets the
	// black template image the menu bar recolours; everywhere else the colour icon. Wails'
	// SetTemplateIcon replaces the icon on every OS, so it is only called on macOS.
	if runtime.GOOS == "darwin" {
		s.Tray.SetTemplateIcon(icons.TrayTemplate)
	} else {
		s.Tray.SetIcon(icons.Tray)
	}
	s.SetTrayLabels(labels)
}

// SetTrayLabels relabels the tray menu (the UI language changed).
func (s *Shell) SetTrayLabels(labels app.TrayLabels) {
	if s.Tray == nil {
		return
	}
	s.Tray.SetMenu(s.trayMenu(labels))
	s.Tray.SetTooltip(labels.Tooltip)
}

// trayMenu is the tray menu in labels' language.
func (s *Shell) trayMenu(labels app.TrayLabels) *application.Menu {
	a := s.trayActions
	menu := s.App.NewMenu()
	menu.Add(labels.PriceCheck).OnClick(func(*application.Context) { call(a.PriceCheck) })
	menu.Add(labels.Settings).OnClick(func(*application.Context) { call(a.Settings) })
	menu.Add(labels.Diagnostics).OnClick(func(*application.Context) { call(a.Diagnostics) })
	menu.AddSeparator()
	menu.Add(labels.Quit).OnClick(func(*application.Context) { s.App.Quit() })
	return menu
}

func call(f func()) {
	if f != nil {
		f()
	}
}
