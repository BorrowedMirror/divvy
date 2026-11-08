package gamelog

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testLog = `2026/11/06 11:15:34 172089589 2caa229f [DEBUG Client 348] Generating level 65 area "HideoutFelled" with seed 1
2026/11/06 11:15:34 172089622 7fbd1225 [INFO Client 348] [SCENE] Set Source [Felled Hideout]
2026/11/06 11:16:00 172089700 3ef232c2 [INFO Client 348] @From <GUILD> Seller_One: Hi, I would like to buy your ring
2026/11/06 11:17:00 172089800 3ef232c2 [INFO Client 348] @To Buyer: ty
2026/11/06 11:18:00 172089900 3ef232c2 [INFO Client 348] #Chatter: @To Fake: not a whisper
2026/11/06 13:30:10 180165376 7fbd1225 [INFO Client 480] [SCENE] Set Source [(null)]
2026/11/06 13:30:10 180165393 7fbd1225 [INFO Client 480] [SCENE] Set Source [(unknown)]
`

func TestParsesWhispersAndScenes(t *testing.T) {
	lines := strings.Split(strings.TrimSuffix(testLog, "\n"), "\n")
	for i, want := range map[int]string{2: "Seller_One", 3: "Buyer", 4: ""} {
		assert.Equal(t, want, whisperPartner(lines[i]), "whisper line %d", i)
	}
	assert.Equal(t, "Felled Hideout", sceneArea(lines[1]))
	for _, i := range []int{0, 5, 6} {
		assert.Empty(t, sceneArea(lines[i]), "scene line %d", i)
	}
	assert.Equal(t, Vars{LastWhisper: "Buyer", Area: "Felled Hideout"}, scan(newestFirst(testLog), true, true))
	assert.Empty(t, scan(newestFirst(testLog), false, true).LastWhisper, "unwanted whisper")
}

func TestNewestFirstDropsLineEndings(t *testing.T) {
	assert.Equal(t, []string{"c", "", "b", "a"}, newestFirst("a\r\nb\n\nc\r"))
	assert.Empty(t, newestFirst(""))
}

func TestReadsBackwardsPastTheFirstWindow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Client.txt")
	// The whisper sits before 300 KiB of other lines, outside the first 256 KiB window.
	filler := strings.Repeat("2026/11/06 12:00:00 1 2 [INFO Client 1] something else\n", 6000)
	require.NoError(t, os.WriteFile(path, []byte(testLog+filler), 0o600))
	vars, err := Read(path, true, true)
	require.NoError(t, err)
	assert.Equal(t, Vars{LastWhisper: "Buyer", Area: "Felled Hideout"}, vars)

	require.NoError(t, os.WriteFile(path, nil, 0o600))
	vars, err = Read(path, true, true)
	require.NoError(t, err)
	assert.Equal(t, Vars{}, vars, "empty log")

	_, err = Read(filepath.Join(t.TempDir(), "missing.txt"), true, false)
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestFindsSteamLibraries(t *testing.T) {
	vdf := "\"libraryfolders\"\n{\n\t\"0\"\n\t{\n\t\t\"path\"\t\t\"/home/u/.local/share/Steam\"\n\t}\n\t\"1\"\n\t{\n\t\t\"path\"\t\t\"D:\\\\SteamLibrary\"\n\t}\n}\n"
	assert.Equal(t, []string{"/home/u/.local/share/Steam", `D:\SteamLibrary`}, steamLibraryPaths(vdf))

	home := t.TempDir()
	root := filepath.Join(home, ".local/share/Steam/steamapps")
	require.NoError(t, os.MkdirAll(root, 0o755))
	other := filepath.Join(home, "Games")
	require.NoError(t, os.WriteFile(filepath.Join(root, "libraryfolders.vdf"), []byte(`"path" "`+other+`"`), 0o600))
	if runtime.GOOS != "windows" {
		assert.Contains(t, candidates(home), steamLog(other), "library from libraryfolders.vdf is a candidate")
	}
	_, ok := Resolve(filepath.Join(home, "missing.txt"))
	assert.False(t, ok, "a missing configured log does not resolve")
}

func TestResolvePrefersConfiguredAndNewestCandidate(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configured := filepath.Join(home, "Client.txt")
	require.NoError(t, os.WriteFile(configured, nil, 0o600))
	p, ok := Resolve("  " + configured + " ")
	require.True(t, ok)
	assert.Equal(t, configured, p)
	if runtime.GOOS == "windows" {
		return
	}
	older := steamLog(filepath.Join(home, ".steam/steam"))
	newer := steamLog(filepath.Join(home, ".local/share/Steam"))
	base := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	for i, p := range []string{older, newer} {
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, nil, 0o600))
		mtime := base.Add(time.Duration(i) * time.Hour)
		require.NoError(t, os.Chtimes(p, mtime, mtime))
	}
	p, ok = Resolve("")
	require.True(t, ok)
	assert.Equal(t, newer, p, "newest candidate")
}
