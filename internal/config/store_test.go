package config

import (
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/BorrowedMirror/rust-king-ledger/internal/core"
	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
)

func TestLoadMissingFileYieldsDefaults(t *testing.T) {
	s, err := Load(Under(t.TempDir()))
	require.NoError(t, err)
	assertSettings(t, s, DefaultSettings())
}

func TestSaveThenLoadRoundtripsAndCreatesDirs(t *testing.T) {
	paths := Under(filepath.Join(t.TempDir(), "deep", "nested"))
	s := DefaultSettings()
	s.UI.Language = "de"
	s.Trade.Contact = new("me@example.com")
	require.NoError(t, Save(paths, &s))
	got, err := Load(paths)
	require.NoError(t, err)
	assertSettings(t, got, s)

	// Overwrite in place; no temp files left behind.
	s.Overlay.Opacity = 0.5
	require.NoError(t, Save(paths, &s))
	got, err = Load(paths)
	require.NoError(t, err)
	assertSettings(t, got, s)
	entries, err := os.ReadDir(paths.ConfigDir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "settings.toml", entries[0].Name())
}

func TestSaveRejectsInvalidSettingsWithoutWriting(t *testing.T) {
	paths := Under(t.TempDir())
	s := DefaultSettings()
	s.Overlay.Opacity = 0.0
	requireInvalid(t, Save(paths, &s), "overlay.opacity")
	assert.NoFileExists(t, paths.SettingsFile)
}

func TestSaveToBareRelativeFilename(t *testing.T) {
	dir := t.TempDir()
	paths := Under(dir)
	paths.SettingsFile = filepath.Join(dir, "flat.toml")
	s := DefaultSettings()
	require.NoError(t, Save(paths, &s))
	got, err := Load(paths)
	require.NoError(t, err)
	assertSettings(t, got, DefaultSettings())

	// A bare file name resolves against the working directory.
	t.Chdir(dir)
	bare := Paths{SettingsFile: "bare.toml"}
	require.NoError(t, Save(bare, &s))
	assert.FileExists(t, filepath.Join(dir, "bare.toml"))
}

func TestSaveFailsWhenConfigDirIsAFile(t *testing.T) {
	paths := Under(t.TempDir())
	require.NoError(t, os.WriteFile(paths.ConfigDir, []byte("not a dir"), 0o644))
	s := DefaultSettings()
	requireIOError(t, Save(paths, &s), paths.ConfigDir)
}

func TestSaveFailsWhenTargetIsADirectory(t *testing.T) {
	paths := Under(t.TempDir())
	require.NoError(t, os.MkdirAll(filepath.Join(paths.SettingsFile, "occupied"), 0o755))
	s := DefaultSettings()
	requireIOError(t, Save(paths, &s), paths.SettingsFile)
	// The temp file was cleaned up.
	entries, _ := os.ReadDir(paths.ConfigDir)
	assert.Len(t, entries, 1)
}

func TestLoadUnreadablePathIsIOError(t *testing.T) {
	paths := Under(t.TempDir())
	require.NoError(t, os.MkdirAll(paths.SettingsFile, 0o755))
	_, err := Load(paths)
	requireIOError(t, err, paths.SettingsFile)
	assert.ErrorContains(t, err, "settings.toml")
}

func TestLoadBadTOMLIsParseError(t *testing.T) {
	paths := Under(t.TempDir())
	writeSettings(t, paths, "version = = 1")
	_, err := Load(paths)
	require.ErrorAs(t, err, new(*ParseError))
	writeSettings(t, paths, "version = 1\n[overlay]\nopacity = \"high\"\n")
	_, err = Load(paths)
	require.ErrorAs(t, err, new(*ParseError))
}

func TestLoadTooNewIsRejected(t *testing.T) {
	paths := Under(t.TempDir())
	writeSettings(t, paths, "version = 99\n")
	_, err := Load(paths)
	var tooNew *TooNewError
	require.ErrorAs(t, err, &tooNew)
	assert.Equal(t, uint32(99), tooNew.Found)
}

func TestLoadRunsValidation(t *testing.T) {
	paths := Under(t.TempDir())
	writeSettings(t, paths, "version = 1\n[overlay]\nauto_hide_ms = 60000\n")
	_, err := Load(paths)
	require.ErrorAs(t, err, new(*InvalidError))
}

func TestLoadMigratesLegacyFile(t *testing.T) {
	paths := Under(t.TempDir())
	writeSettings(t, paths, fixture(t, "v0_full.toml"))
	s, err := Load(paths)
	require.NoError(t, err)
	assert.Equal(t, SettingsVersion, s.Version)
	assert.Equal(t, "Rise of the Abyssal", *s.League.League)
	assert.Equal(t, core.StatusAny, s.Trade.Sellers)
}

// ---------------------------------------------------------------- property test

// randomSettings returns random settings, valid or not.
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
		return new(f())
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
	s.Trade.POESESSID = opt(func() string {
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
	s.Overlay.AutoHideMS = uint32(r.IntN(10_001))
	s.Overlay.Opacity = 0.1 + r.Float64()*0.9
	s.Overlay.Side = []OverlaySide{SideCursor, SideLeft, SideRight}[r.IntN(3)]
	s.UI.Language = pick(lower, 2)
	if r.IntN(2) == 0 {
		s.UI.Language += "-" + pick(upper, 2)
	}
	s.UI.Theme = []Theme{ThemeSystem, ThemeDark, ThemeLight}[r.IntN(3)]
	s.UI.StartMinimised, s.UI.CheckUpdates, s.UI.OnboardingDone = r.IntN(2) == 0, r.IntN(2) == 0, r.IntN(2) == 0
	if r.IntN(2) == 0 {
		s.Platform.BackendOverride = new([]platform.BackendKind{
			platform.BackendWindows, platform.BackendMacOS, platform.BackendX11, platform.BackendWayland, platform.BackendFake,
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
		require.NoError(t, Save(paths, &s))
		got, err := Load(paths)
		require.NoError(t, err)
		assertSettings(t, got, s)
	}
}

func TestSaveNormalisesVersionAndRestrictsPermissions(t *testing.T) {
	paths := Under(t.TempDir())
	s := DefaultSettings()
	s.Version = 0
	require.NoError(t, Save(paths, &s))
	got, err := Load(paths)
	require.NoError(t, err)
	assert.Equal(t, SettingsVersion, got.Version)
	text, err := os.ReadFile(paths.SettingsFile)
	require.NoError(t, err)
	assert.Contains(t, string(text), fmt.Sprintf("version = %d", SettingsVersion))
	if runtime.GOOS != "windows" {
		info, err := os.Stat(paths.SettingsFile)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
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
		require.NoError(t, err)
	}
	_, err := Load(paths)
	require.NoError(t, err)
}

func TestHotkeysCanBeClearedAndStayCleared(t *testing.T) {
	paths := Under(t.TempDir())
	s := DefaultSettings()
	assert.Equal(t, chord("Space", platform.Ctrl), s.Hotkeys.OpenSettings)
	s.Hotkeys.OpenSettings = platform.UnboundChord()
	s.Hotkeys.PriceCheckPinned = platform.UnboundChord() // two cleared hotkeys are not duplicates
	require.NoError(t, Validate(&s))
	require.NoError(t, Save(paths, &s))
	loaded, err := Load(paths)
	require.NoError(t, err)
	require.True(t, loaded.Hotkeys.OpenSettings.IsUnbound(), "cleared hotkey must not reset to the default")
	require.True(t, loaded.Hotkeys.PriceCheckPinned.IsUnbound(), "cleared hotkey must not reset to the default")
	// Modifiers without a key are still rejected.
	s.Hotkeys.OpenSettings = platform.Chord{Modifiers: []platform.KeyModifier{platform.Ctrl}}
	require.Error(t, Validate(&s), "modifiers without a key accepted")
}

func TestLegacyKalandraSettingsAreAdoptedOnce(t *testing.T) {
	dir := t.TempDir()
	paths := Under(filepath.Join(dir, "new"))
	legacy := filepath.Join(dir, "old", "settings.toml")
	adopt := func() bool {
		t.Helper()
		ok, err := AdoptLegacySettings(paths, legacy)
		require.NoError(t, err)
		return ok
	}
	widen := func() float64 {
		t.Helper()
		s, err := Load(paths)
		require.NoError(t, err)
		return s.Trade.WidenPct
	}
	require.False(t, adopt(), "no legacy file")
	require.NoError(t, os.MkdirAll(filepath.Dir(legacy), 0o755))
	require.NoError(t, os.WriteFile(legacy, []byte("version = 1\n\n[trade]\nwiden_pct = 17.0\n"), 0o644))
	require.True(t, adopt(), "legacy file not adopted")
	assert.InDelta(t, 17.0, widen(), 0)
	assert.FileExists(t, legacy, "the old file is kept")
	// Never overwrites settings that already exist at the new location.
	require.NoError(t, os.WriteFile(legacy, []byte("version = 1\n\n[trade]\nwiden_pct = 3.0\n"), 0o644))
	require.False(t, adopt(), "adopted twice")
	assert.InDelta(t, 17.0, widen(), 0)
	ok, err := AdoptLegacySettings(paths, "")
	require.NoError(t, err)
	assert.False(t, ok, "adopted from no legacy file")
	if p, ok := LegacySettingsFile(); ok {
		assert.Equal(t, "settings.toml", filepath.Base(p))
	}
	// A directory is not a legacy file.
	ok, err = AdoptLegacySettings(Under(filepath.Join(dir, "other")), filepath.Dir(legacy))
	require.NoError(t, err)
	assert.False(t, ok, "adopted a directory")
}
