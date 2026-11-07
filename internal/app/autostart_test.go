package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAutostartDesktopEntryQuotesThePath(t *testing.T) {
	e := DesktopEntry("/home/u/Apps/Rust King Ledger $1.AppImage")
	if !strings.Contains(e, "Exec=\"/home/u/Apps/Rust King Ledger \\$1.AppImage\"\n") {
		t.Fatal(e)
	}
	if !strings.HasPrefix(e, "[Desktop Entry]\nType=Application\n") {
		t.Fatal(e)
	}
	if got, want := DesktopExec("/a\"b\\c`d"), `"/a\"b\\c\`+"`"+`d"`; got != want {
		t.Fatalf("got %s want %s", got, want)
	}
}

func TestAutostartDesktopEntryIsByteIdentical(t *testing.T) {
	want := "[Desktop Entry]\nType=Application\nName=Rust King Ledger\nComment=Path of Exile 2 price-check overlay\nExec=\"/opt/rkl\"\nTerminal=false\nX-GNOME-Autostart-enabled=true\n"
	if got := DesktopEntry("/opt/rkl"); got != want {
		t.Fatalf("got %q", got)
	}
}

func TestAutostartLaunchAgentEscapesXML(t *testing.T) {
	p := LaunchAgent("/Applications/R&K <L>.app/Contents/MacOS/rkl")
	if !strings.Contains(p, "<string>/Applications/R&amp;K &lt;L&gt;.app/Contents/MacOS/rkl</string>") {
		t.Fatal(p)
	}
	if !strings.Contains(p, "<key>RunAtLoad</key><true/>") || !strings.Contains(p, "<key>Label</key><string>app.rkl</string>") {
		t.Fatal(p)
	}
}

func TestAutostartEntryPathPrefersXDGConfigHome(t *testing.T) {
	cases := []struct {
		xdg, home, want string
		ok              bool
	}{
		{"/x", "/home/u", "/x/autostart/rkl.desktop", true},
		{"", "/home/u", "/home/u/.config/autostart/rkl.desktop", true},
		{"", "", "", false},
	}
	for _, c := range cases {
		if got, ok := LinuxAutostartPath(c.xdg, c.home); got != filepath.FromSlash(c.want) || ok != c.ok {
			t.Errorf("(%q, %q): got %q %v", c.xdg, c.home, got, ok)
		}
	}
	if got := MacLaunchAgentPath("/Users/u"); got != filepath.FromSlash("/Users/u/Library/LaunchAgents/app.rkl.plist") {
		t.Errorf("launch agent path %q", got)
	}
}

func TestAutostartWriteAndRemoveAreIdempotent(t *testing.T) {
	f := filepath.Join(t.TempDir(), "a/b.desktop")
	if err := writeAutostartFile(f, "x"); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(f); err != nil || string(b) != "x" {
		t.Fatalf("%q %v", b, err)
	}
	for range 2 {
		if err := removeAutostartFile(f); err != nil {
			t.Fatal(err)
		}
	}
}

func TestAutostartLaunchPathPrefersAppImage(t *testing.T) {
	t.Setenv("APPIMAGE", "/home/u/rkl.AppImage")
	if p, err := LaunchPath(); err != nil || p != "/home/u/rkl.AppImage" {
		t.Fatalf("%q %v", p, err)
	}
	t.Setenv("APPIMAGE", "")
	if p, err := LaunchPath(); err != nil || p == "" || p == "/home/u/rkl.AppImage" {
		t.Fatalf("%q %v", p, err)
	}
}
