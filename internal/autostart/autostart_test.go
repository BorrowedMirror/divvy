package autostart

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDesktopEntryQuotesThePath(t *testing.T) {
	e := desktopEntry("/home/u/Apps/Divvy $1.AppImage")
	assert.Contains(t, e, "Exec=\"/home/u/Apps/Divvy \\$1.AppImage\"\n")
	assert.Equal(t, `"/a\"b\\c\`+"`"+`d"`, desktopExec("/a\"b\\c`d"))
}

func TestDesktopEntryIsExact(t *testing.T) {
	want := "[Desktop Entry]\nType=Application\nName=Divvy\nComment=Path of Exile 2 price-check overlay\nExec=\"/opt/divvy\"\nTerminal=false\nX-GNOME-Autostart-enabled=true\n"
	assert.Equal(t, want, desktopEntry("/opt/divvy"))
}

func TestLaunchAgentEscapesXML(t *testing.T) {
	p := launchAgent("/Applications/R&K <L>.app/Contents/MacOS/divvy")
	assert.Contains(t, p, "<string>/Applications/R&amp;K &lt;L&gt;.app/Contents/MacOS/divvy</string>")
	assert.Contains(t, p, "<key>RunAtLoad</key><true/>")
	assert.Contains(t, p, "<key>Label</key><string>app.divvy</string>")
}

func TestEntryPaths(t *testing.T) {
	assert.Equal(t, filepath.FromSlash("/x/autostart/divvy.desktop"), desktopEntryPath("/x"))
	assert.Equal(t, filepath.FromSlash("/Users/u/Library/LaunchAgents/app.divvy.plist"), launchAgentPath("/Users/u"))
}

func TestWriteAndRemoveAreIdempotent(t *testing.T) {
	f := filepath.Join(t.TempDir(), "a", "b.desktop")
	require.NoError(t, writeFile(f, "x"))
	b, err := os.ReadFile(f)
	require.NoError(t, err)
	assert.Equal(t, "x", string(b))
	for range 2 {
		require.NoError(t, removeFile(f))
	}
}

func TestLaunchPathPrefersAppImage(t *testing.T) {
	t.Setenv("APPIMAGE", "/home/u/divvy.AppImage")
	p, err := launchPath()
	require.NoError(t, err)
	assert.Equal(t, "/home/u/divvy.AppImage", p)
	t.Setenv("APPIMAGE", "")
	p, err = launchPath()
	require.NoError(t, err)
	assert.NotEmpty(t, p)
	assert.NotEqual(t, "/home/u/divvy.AppImage", p)
}
