// Command rkl is the Rust King Ledger desktop app: a Path of Exile 2 price-check overlay.
package main

import (
	_ "embed"
	"log/slog"
	"os"

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

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	icons := shell.Icons{App: appIcon, Tray: trayIcon, TrayTemplate: trayTemplateIcon}
	s := shell.New(shell.Options{
		Assets: web.Dist(),
		Icons:  icons,
		Logger: logger,
		// Same rule as the Tauri build: a test-hook run is never single-instance.
		SingleInstance: os.Getenv("RKL_TEST_HOOK") == "",
		OnSecondInstance: func(args []string) {
			logger.Info("second launch", "args", args)
		},
	})
	s.BuildTray(icons, shell.TrayLabels{
		Tooltip:     "Rust King Ledger",
		PriceCheck:  "Price check",
		Settings:    "Settings",
		Diagnostics: "Diagnostics",
		Quit:        "Quit",
	}, shell.TrayActions{
		Settings: func() {
			s.Settings.Show()
			s.Settings.Focus()
		},
	})

	if err := s.App.Run(); err != nil {
		logger.Error("rkl exited", "err", err)
		os.Exit(1)
	}
}
