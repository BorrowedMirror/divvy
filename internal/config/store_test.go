package config

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/BorrowedMirror/rust-king-ledger/internal/core"
	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
)

func TestLoadMissingFileYieldsDefaults(t *testing.T) {
	s, err := Load(Under(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	assertSettings(t, s, DefaultSettings())
}

func TestSaveThenLoadRoundtripsAndCreatesDirs(t *testing.T) {
	paths := Under(filepath.Join(t.TempDir(), "deep", "nested"))
	s := DefaultSettings()
	s.UI.Language = "de"
	s.Trade.Contact = ptr("me@example.com")
	if err := Save(paths, &s); err != nil {
		t.Fatal(err)
	}
	got, err := Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	assertSettings(t, got, s)

	// Overwrite in place; no temp files left behind.
	s.Overlay.Opacity = 0.5
	if err := Save(paths, &s); err != nil {
		t.Fatal(err)
	}
	got, err = Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	assertSettings(t, got, s)
	entries, err := os.ReadDir(paths.ConfigDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "settings.toml" {
		t.Fatalf("entries: %v", entries)
	}
}

func TestSaveRejectsInvalidSettingsWithoutWriting(t *testing.T) {
	paths := Under(t.TempDir())
	s := DefaultSettings()
	s.Overlay.Opacity = 0.0
	assertInvalid(t, Save(paths, &s), "overlay.opacity")
	if _, err := os.Stat(paths.SettingsFile); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("settings file written")
	}
}

func TestSaveToBareRelativeFilename(t *testing.T) {
	dir := t.TempDir()
	paths := Under(dir)
	paths.SettingsFile = filepath.Join(dir, "flat.toml")
	s := DefaultSettings()
	if err := Save(paths, &s); err != nil {
		t.Fatal(err)
	}
	got, err := Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	assertSettings(t, got, DefaultSettings())

	// A bare file name resolves against the working directory.
	t.Chdir(dir)
	bare := Paths{SettingsFile: "bare.toml"}
	if err := Save(bare, &s); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "bare.toml")); err != nil {
		t.Fatal(err)
	}
}

func TestSaveFailsWhenConfigDirIsAFile(t *testing.T) {
	paths := Under(t.TempDir())
	if err := os.WriteFile(paths.ConfigDir, []byte("not a dir"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := DefaultSettings()
	err := Save(paths, &s)
	var ioe *IoError
	if !errors.As(err, &ioe) || ioe.Path != paths.ConfigDir {
		t.Fatalf("%v", err)
	}
}

func TestSaveFailsWhenTargetIsADirectory(t *testing.T) {
	paths := Under(t.TempDir())
	if err := os.MkdirAll(filepath.Join(paths.SettingsFile, "occupied"), 0o755); err != nil {
		t.Fatal(err)
	}
	s := DefaultSettings()
	err := Save(paths, &s)
	var ioe *IoError
	if !errors.As(err, &ioe) || ioe.Path != paths.SettingsFile {
		t.Fatalf("%v", err)
	}
	// The temp file was cleaned up.
	entries, _ := os.ReadDir(paths.ConfigDir)
	assertEqual(t, len(entries), 1)
}

func TestLoadUnreadablePathIsIoError(t *testing.T) {
	paths := Under(t.TempDir())
	if err := os.MkdirAll(paths.SettingsFile, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := Load(paths)
	var ioe *IoError
	if !errors.As(err, &ioe) || ioe.Path != paths.SettingsFile {
		t.Fatalf("%v", err)
	}
	if !strings.Contains(err.Error(), "settings.toml") {
		t.Fatal(err)
	}
}

func TestLoadBadTOMLIsParseError(t *testing.T) {
	paths := Under(t.TempDir())
	var pe *ParseError
	writeSettings(t, paths, "version = = 1")
	if _, err := Load(paths); !errors.As(err, &pe) {
		t.Fatalf("%v", err)
	}
	writeSettings(t, paths, "version = 1\n[overlay]\nopacity = \"high\"\n")
	if _, err := Load(paths); !errors.As(err, &pe) {
		t.Fatalf("%v", err)
	}
}

func TestLoadTooNewIsRejected(t *testing.T) {
	paths := Under(t.TempDir())
	writeSettings(t, paths, "version = 99\n")
	_, err := Load(paths)
	var tooNew *TooNewError
	if !errors.As(err, &tooNew) || tooNew.Found != 99 {
		t.Fatalf("%v", err)
	}
}

func TestLoadRunsValidation(t *testing.T) {
	paths := Under(t.TempDir())
	writeSettings(t, paths, "version = 1\n[overlay]\nauto_hide_ms = 60000\n")
	_, err := Load(paths)
	var inv *InvalidError
	if !errors.As(err, &inv) {
		t.Fatalf("%v", err)
	}
}

func TestLoadMigratesLegacyFile(t *testing.T) {
	paths := Under(t.TempDir())
	writeSettings(t, paths, fixture(t, "v0_full.toml"))
	s, err := Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, s.Version, SettingsVersion)
	assertEqual(t, *s.League.League, "Rise of the Abyssal")
	assertEqual(t, s.Trade.Sellers, core.StatusAny)
}

// ---------------------------------------------------------------- property test

// randomSettings mirrors the Rust proptest strategy.
func randomSettings(r *rand.Rand) Settings {
	pick := func(chars string, n int) string {
		b := make([]rune, n)
		rs := []rune(chars)
		for i := range b {
			b[i] = rs[r.IntN(len(rs))]
		}
		return string(b)
	}
	const upper = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	const lower = "abcdefghijklmnopqrstuvwxyz"
	const digits = "0123456789"
	randChord := func() platform.Chord {
		mods := []platform.KeyModifier{}
		for range r.IntN(4) {
			mods = append(mods, []platform.KeyModifier{platform.Ctrl, platform.Alt, platform.Shift, platform.Meta}[r.IntN(4)])
		}
		var key string
		switch r.IntN(3) {
		case 0:
			key = "Key" + pick(upper, 1)
		case 1:
			key = "F" + pick("123456789", 1)
		default:
			key = "Digit" + pick(digits, 1)
		}
		return platform.Chord{Modifiers: mods, Key: key}
	}
	opt := func(f func() string) *string {
		if r.IntN(2) == 0 {
			return nil
		}
		return ptr(f())
	}
	s := DefaultSettings()
	s.League.AutoDetect = r.IntN(2) == 0
	s.League.League = opt(func() string {
		return pick(upper+lower, 1) + pick(upper+lower+digits+` "'\`, r.IntN(21))
	})
	s.Hotkeys.PriceCheck, s.Hotkeys.PriceCheckPinned, s.Hotkeys.OpenSettings = randChord(), randChord(), randChord()
	s.Hotkeys.AdvancedCopy = r.IntN(2) == 0
	s.Trade.Source = []core.PriceSourceKind{core.SourceTrade, core.SourcePoeNinja, core.SourcePoe2Scout}[r.IntN(3)]
	s.Trade.Sellers = []core.OnlineStatus{core.StatusSecurable, core.StatusOnline, core.StatusAny}[r.IntN(3)]
	s.Trade.WidenPct = r.Float64() * 100
	s.Trade.EnableAllStats = r.IntN(2) == 0
	s.Trade.Contact = opt(func() string {
		return pick(lower+digits+".+", 1+r.IntN(8)) + "@" + pick(lower, 1+r.IntN(8)) + "." + pick(lower, 2+r.IntN(3))
	})
	s.Trade.Poesessid = opt(func() string {
		// Any printable characters, as `\PC{0,32}`.
		n := r.IntN(33)
		b := make([]rune, n)
		for i := range b {
			for {
				c := rune(0x20 + r.IntN(0x3000))
				if c < 0x7f || c > 0x9f {
					b[i] = c
					break
				}
			}
		}
		return string(b)
	})
	s.Overlay.AutoHideMs = uint32(r.IntN(10_001))
	s.Overlay.Opacity = 0.1 + r.Float64()*0.9
	s.Overlay.Side = []OverlaySide{SideCursor, SideLeft, SideRight}[r.IntN(3)]
	s.UI.Language = pick(lower, 2)
	if r.IntN(2) == 0 {
		s.UI.Language += "-" + pick(upper, 2)
	}
	s.UI.Theme = []Theme{ThemeSystem, ThemeDark, ThemeLight}[r.IntN(3)]
	s.UI.StartMinimised, s.UI.CheckUpdates, s.UI.OnboardingDone = r.IntN(2) == 0, r.IntN(2) == 0, r.IntN(2) == 0
	if r.IntN(2) == 0 {
		s.Platform.BackendOverride = ptr([]platform.BackendKind{
			platform.BackendWindows, platform.BackendMacos, platform.BackendX11, platform.BackendWayland, platform.BackendFake,
		}[r.IntN(5)])
	}
	return s
}

func TestAnyValidSettingsRoundtripThroughSaveLoad(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	for cases := 0; cases < 64; {
		s := randomSettings(r)
		if Validate(&s) != nil {
			continue
		}
		cases++
		paths := Under(t.TempDir())
		if err := Save(paths, &s); err != nil {
			t.Fatal(err)
		}
		got, err := Load(paths)
		if err != nil {
			t.Fatal(err)
		}
		assertSettings(t, got, s)
	}
}

func TestSaveNormalisesVersionAndRestrictsPermissions(t *testing.T) {
	paths := Under(t.TempDir())
	s := DefaultSettings()
	s.Version = 0
	if err := Save(paths, &s); err != nil {
		t.Fatal(err)
	}
	got, err := Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, got.Version, SettingsVersion)
	text, _ := os.ReadFile(paths.SettingsFile)
	if !strings.Contains(string(text), fmt.Sprintf("version = %d", SettingsVersion)) {
		t.Fatalf("no version in\n%s", text)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(paths.SettingsFile)
		if err != nil {
			t.Fatal(err)
		}
		assertEqual(t, info.Mode().Perm(), os.FileMode(0o600))
	}
}

func TestConcurrentSavesNeverCorrupt(t *testing.T) {
	paths := Under(t.TempDir())
	var wg sync.WaitGroup
	errs := make(chan error, 8*20)
	for i := range 8 {
		wg.Go(func() {
			s := DefaultSettings()
			s.Trade.WidenPct = float64(i)
			for range 20 {
				if err := Save(paths, &s); err != nil {
					errs <- err
				}
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	if _, err := Load(paths); err != nil {
		t.Fatal(err)
	}
}

func TestHotkeysCanBeClearedAndStayCleared(t *testing.T) {
	paths := Under(t.TempDir())
	s := DefaultSettings()
	assertEqual(t, s.Hotkeys.OpenSettings, chord("Space", platform.Ctrl))
	s.Hotkeys.OpenSettings = platform.UnboundChord()
	s.Hotkeys.PriceCheckPinned = platform.UnboundChord() // two cleared hotkeys are not duplicates
	if err := Validate(&s); err != nil {
		t.Fatal(err)
	}
	if err := Save(paths, &s); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.Hotkeys.OpenSettings.IsUnbound() {
		t.Fatal("cleared hotkey must not reset to the default")
	}
	if !loaded.Hotkeys.PriceCheckPinned.IsUnbound() {
		t.Fatal("cleared hotkey must not reset to the default")
	}
	// Modifiers without a key are still rejected.
	s.Hotkeys.OpenSettings = platform.Chord{Modifiers: []platform.KeyModifier{platform.Ctrl}}
	if Validate(&s) == nil {
		t.Fatal("modifiers without a key accepted")
	}
}

func TestLegacyKalandraSettingsAreAdoptedOnce(t *testing.T) {
	dir := t.TempDir()
	paths := Under(filepath.Join(dir, "new"))
	legacy := filepath.Join(dir, "old", "settings.toml")
	adopt := func() bool {
		t.Helper()
		ok, err := AdoptLegacySettings(paths, legacy)
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}
	widen := func() float64 {
		t.Helper()
		s, err := Load(paths)
		if err != nil {
			t.Fatal(err)
		}
		return s.Trade.WidenPct
	}
	if adopt() {
		t.Fatal("no legacy file")
	}
	if err := os.MkdirAll(filepath.Dir(legacy), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte("version = 1\n\n[trade]\nwiden_pct = 17.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !adopt() {
		t.Fatal("legacy file not adopted")
	}
	assertEqual(t, widen(), 17.0)
	if _, err := os.Stat(legacy); err != nil {
		t.Fatal("old file is kept")
	}
	// Never overwrites settings that already exist at the new location.
	if err := os.WriteFile(legacy, []byte("version = 1\n\n[trade]\nwiden_pct = 3.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if adopt() {
		t.Fatal("adopted twice")
	}
	assertEqual(t, widen(), 17.0)
	if ok, err := AdoptLegacySettings(paths, ""); ok || err != nil {
		t.Fatal("adopted from no legacy file")
	}
	if p, ok := LegacySettingsFile(); ok && filepath.Base(p) != "settings.toml" {
		t.Fatal(p)
	}
	// A directory is not a legacy file.
	if ok, _ := AdoptLegacySettings(Under(filepath.Join(dir, "other")), filepath.Dir(legacy)); ok {
		t.Fatal("adopted a directory")
	}
}
