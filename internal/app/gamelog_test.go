package app

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

const testLog = `2026/11/06 11:15:34 172089589 2caa229f [DEBUG Client 348] Generating level 65 area "HideoutFelled" with seed 1
2026/11/06 11:15:34 172089622 7fbd1225 [INFO Client 348] [SCENE] Set Source [Felled Hideout]
2026/11/06 11:16:00 172089700 3ef232c2 [INFO Client 348] @From <GUILD> Seller_One: Hi, I would like to buy your ring
2026/11/06 11:17:00 172089800 3ef232c2 [INFO Client 348] @To Buyer: ty
2026/11/06 11:18:00 172089900 3ef232c2 [INFO Client 348] #Chatter: @To Fake: not a whisper
2026/11/06 13:30:10 180165376 7fbd1225 [INFO Client 480] [SCENE] Set Source [(null)]
2026/11/06 13:30:10 180165393 7fbd1225 [INFO Client 480] [SCENE] Set Source [(unknown)]
`

func reversedLines(s string) []string {
	l := textLines(s)
	slices.Reverse(l)
	return l
}

func TestGamelogParsesWhispersAndScenes(t *testing.T) {
	lines := textLines(testLog)
	for i, want := range map[int]string{2: "Seller_One", 3: "Buyer", 4: ""} {
		if got, _ := WhisperPartner(lines[i]); got != want {
			t.Errorf("whisper line %d: got %q want %q", i, got, want)
		}
	}
	if got, ok := SceneArea(lines[1]); !ok || got != "Felled Hideout" {
		t.Errorf("scene: got %q", got)
	}
	for _, i := range []int{0, 5, 6} {
		if got, ok := SceneArea(lines[i]); ok {
			t.Errorf("scene line %d: got %q", i, got)
		}
	}
	if vars := ScanLog(reversedLines(testLog), true, true); vars != (LogVars{LastWhisper: "Buyer", Area: "Felled Hideout"}) {
		t.Errorf("scan: %+v", vars)
	}
	if vars := ScanLog(reversedLines(testLog), false, true); vars.LastWhisper != "" {
		t.Errorf("unwanted whisper: %+v", vars)
	}
}

func TestGamelogReadsBackwardsPastTheFirstWindow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Client.txt")
	// The whisper sits before 300 KiB of other lines, outside the first 256 KiB window.
	filler := strings.Repeat("2026/11/06 12:00:00 1 2 [INFO Client 1] something else\n", 6000)
	if err := os.WriteFile(path, []byte(testLog+filler), 0o600); err != nil {
		t.Fatal(err)
	}
	vars, err := ReadLogVars(path, true, true)
	if err != nil {
		t.Fatal(err)
	}
	if vars != (LogVars{LastWhisper: "Buyer", Area: "Felled Hideout"}) {
		t.Fatalf("got %+v", vars)
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if vars, err := ReadLogVars(path, true, true); err != nil || vars != (LogVars{}) {
		t.Fatalf("empty log: %+v %v", vars, err)
	}
}

func TestGamelogFindsSteamLibraries(t *testing.T) {
	vdf := "\"libraryfolders\"\n{\n\t\"0\"\n\t{\n\t\t\"path\"\t\t\"/home/u/.local/share/Steam\"\n\t}\n\t\"1\"\n\t{\n\t\t\"path\"\t\t\"D:\\\\SteamLibrary\"\n\t}\n}\n"
	if got, want := SteamLibraryPaths(vdf), []string{"/home/u/.local/share/Steam", `D:\SteamLibrary`}; !slices.Equal(got, want) {
		t.Fatalf("got %q want %q", got, want)
	}
	home := t.TempDir()
	root := filepath.Join(home, ".local/share/Steam/steamapps")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(home, "Games")
	if err := os.WriteFile(filepath.Join(root, "libraryfolders.vdf"), []byte(`"path" "`+other+`"`), 0o600); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && !slices.Contains(LogCandidates(home), steamLog(other)) {
		t.Fatal("library from libraryfolders.vdf is a candidate")
	}
	if p, ok := ResolveLogPath(filepath.Join(home, "missing.txt")); ok {
		t.Fatalf("missing configured log resolved to %q", p)
	}
}

func TestGamelogResolvePrefersConfiguredAndNewestCandidate(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configured := filepath.Join(home, "Client.txt")
	if err := os.WriteFile(configured, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if p, ok := ResolveLogPath("  " + configured + " "); !ok || p != configured {
		t.Fatalf("configured: %q %v", p, ok)
	}
	if runtime.GOOS == "windows" {
		return
	}
	older := steamLog(filepath.Join(home, ".steam/steam"))
	newer := steamLog(filepath.Join(home, ".local/share/Steam"))
	for i, p := range []string{older, newer} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		mtime := testTime.Add(time.Duration(i) * time.Hour)
		if err := os.Chtimes(p, mtime, mtime); err != nil {
			t.Fatal(err)
		}
	}
	if p, ok := ResolveLogPath(""); !ok || p != newer {
		t.Fatalf("newest candidate: %q %v", p, ok)
	}
}

func TestTextLinesMatchesRustLines(t *testing.T) {
	if got := textLines("a\r\nb\n\nc\r"); !slices.Equal(got, []string{"a", "b", "", "c\r"}) {
		t.Fatalf("got %q", got)
	}
	if got := textLines(""); len(got) != 0 {
		t.Fatalf("got %q", got)
	}
}

var testTime = time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
