// Package autostart starts Divvy at login: an XDG autostart entry on Linux, the
// `HKCU\...\Run` key on Windows and a LaunchAgent on macOS. Linux uses `$APPIMAGE` when set,
// because an AppImage's executable path is a temporary mount that changes on every launch.
//
// The OS-specific parts live in autostart_<os>.go; this file keeps the pure helpers so their
// tests run everywhere.
package autostart

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/BorrowedMirror/divvy/internal/product"
)

const (
	// name is the entry name (desktop file stem / registry value).
	name = product.Binary
	// label is the macOS LaunchAgent label (and plist file stem).
	label = product.ID
)

// Set turns start-at-login on (refreshing the program path) or off.
func Set(enabled bool) error {
	path, err := launchPath()
	if err != nil {
		return err
	}
	return set(enabled, path)
}

// launchPath is the program to start at login.
func launchPath() (string, error) {
	if appimage := os.Getenv("APPIMAGE"); appimage != "" {
		return appimage, nil
	}
	return os.Executable()
}

// desktopExec quotes path for a desktop entry `Exec` key (spec: double quotes; escape `"`,
// a backtick, `$`, `\`).
func desktopExec(path string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, c := range strings.ToValidUTF8(path, "\uFFFD") {
		switch c {
		case '"', '`', '$', '\\':
			b.WriteByte('\\')
		}
		b.WriteRune(c)
	}
	b.WriteByte('"')
	return b.String()
}

// desktopEntry is the XDG autostart desktop entry for path.
func desktopEntry(path string) string {
	return "[Desktop Entry]\nType=Application\nName=Divvy\nComment=Path of Exile 2 price-check overlay\nExec=" +
		desktopExec(path) + "\nTerminal=false\nX-GNOME-Autostart-enabled=true\n"
}

var xmlEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")

// launchAgent is the macOS LaunchAgent plist starting path at login.
func launchAgent(path string) string {
	return "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<!DOCTYPE plist PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" \"http://www.apple.com/DTDs/PropertyList-1.0.dtd\">\n<plist version=\"1.0\">\n<dict>\n  <key>Label</key><string>" +
		label + "</string>\n  <key>ProgramArguments</key><array><string>" +
		xmlEscaper.Replace(strings.ToValidUTF8(path, "\uFFFD")) +
		"</string></array>\n  <key>RunAtLoad</key><true/>\n</dict>\n</plist>\n"
}

// launchAgentPath is where the macOS LaunchAgent plist lives under home.
func launchAgentPath(home string) string {
	return filepath.Join(home, "Library", "LaunchAgents", label+".plist")
}

// desktopEntryPath is where the XDG autostart entry lives under the config directory.
func desktopEntryPath(configDir string) string {
	return filepath.Join(configDir, "autostart", name+".desktop")
}

// writeFile writes an entry, creating its directory.
func writeFile(path, contents string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(contents), 0o644)
}

// removeFile removes an entry; a missing one is already off.
func removeFile(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
