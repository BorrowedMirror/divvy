package app

// Start RKL at login: an XDG autostart entry on Linux, the `HKCU\...\Run` key on Windows and a
// LaunchAgent on macOS. Linux uses `$APPIMAGE` when set, because an AppImage's executable path
// is a temporary mount that changes on every launch.
//
// The OS-specific parts live in autostart_<os>.go; this file keeps the pure helpers so their
// tests run everywhere.

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const (
	// AutostartName is the entry name (desktop file stem / registry value).
	AutostartName = "rkl"
	// AutostartLabel is the macOS LaunchAgent label (and plist file stem).
	AutostartLabel = "app.rkl"
)

// LaunchPath is the program to start at login.
func LaunchPath() (string, error) {
	if appimage := os.Getenv("APPIMAGE"); appimage != "" {
		return appimage, nil
	}
	return os.Executable()
}

// DesktopExec quotes path for a desktop entry `Exec` key (spec: double quotes; escape `"`,
// a backtick, `$`, `\`).
func DesktopExec(path string) string {
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

// DesktopEntry is the XDG autostart desktop entry for path.
func DesktopEntry(path string) string {
	return "[Desktop Entry]\nType=Application\nName=Rust King Ledger\nComment=Path of Exile 2 price-check overlay\nExec=" +
		DesktopExec(path) + "\nTerminal=false\nX-GNOME-Autostart-enabled=true\n"
}

var xmlEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")

// LaunchAgent is the macOS LaunchAgent plist starting path at login.
func LaunchAgent(path string) string {
	return "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<!DOCTYPE plist PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" \"http://www.apple.com/DTDs/PropertyList-1.0.dtd\">\n<plist version=\"1.0\">\n<dict>\n  <key>Label</key><string>" +
		AutostartLabel + "</string>\n  <key>ProgramArguments</key><array><string>" +
		xmlEscaper.Replace(strings.ToValidUTF8(path, "\uFFFD")) +
		"</string></array>\n  <key>RunAtLoad</key><true/>\n</dict>\n</plist>\n"
}

// LinuxAutostartPath is where the Linux autostart entry lives (`$XDG_CONFIG_HOME/autostart`,
// else `~/.config/autostart`). Empty arguments count as unset; ok is false when both are.
func LinuxAutostartPath(xdgConfigHome, home string) (string, bool) {
	var base string
	switch {
	case xdgConfigHome != "":
		base = xdgConfigHome
	case home != "":
		base = filepath.Join(home, ".config")
	default:
		return "", false
	}
	return filepath.Join(base, "autostart", AutostartName+".desktop"), true
}

// MacLaunchAgentPath is where the macOS LaunchAgent plist lives under home.
func MacLaunchAgentPath(home string) string {
	return filepath.Join(home, "Library/LaunchAgents", AutostartLabel+".plist")
}

func writeAutostartFile(path, contents string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o777); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(contents), 0o666)
}

func removeAutostartFile(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// SetAutostart turns start-at-login on (refreshing the path) or off.
func SetAutostart(enabled bool) error {
	path, err := LaunchPath()
	if err != nil {
		return err
	}
	return setAutostart(enabled, path)
}
