package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAutostartTogglesTheXDGEntry(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	t.Setenv("APPIMAGE", "/home/u/Apps/rkl.AppImage")
	entry := filepath.Join(cfg, "autostart", "rkl.desktop")
	if err := SetAutostart(true); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(entry); err != nil || string(b) != DesktopEntry("/home/u/Apps/rkl.AppImage") {
		t.Fatalf("%q %v", b, err)
	}
	for range 2 {
		if err := SetAutostart(false); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(entry); !os.IsNotExist(err) {
		t.Fatalf("entry still there: %v", err)
	}
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")
	if err := SetAutostart(true); err == nil {
		t.Fatal("no config directory should fail")
	}
}
