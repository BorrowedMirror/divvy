package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func envOf(m map[string]string) Env {
	return func(k string) (string, bool) {
		v, ok := m[k]
		return v, ok
	}
}

func TestResolveHonoursEnvOverride(t *testing.T) {
	root := filepath.FromSlash("/portable/rkl")
	paths, err := ResolveWith(envOf(map[string]string{ConfigDirEnv: root}))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, paths, Under(root))
	assertEqual(t, paths.SettingsFile, filepath.FromSlash("/portable/rkl/config/settings.toml"))
}

func TestResolveIgnoresBlankOverrideAndUsesProjectDirs(t *testing.T) {
	paths, err := resolveFor("linux", envOf(map[string]string{ConfigDirEnv: "  ", "HOME": "/home/exile"}), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := Paths{
		ConfigDir:    "/home/exile/.config/rkl",
		CacheDir:     "/home/exile/.cache/rkl",
		LogDir:       "/home/exile/.local/state/rkl/logs",
		SettingsFile: "/home/exile/.config/rkl/settings.toml",
	}
	assertEqual(t, paths, want)
	none, err := resolveFor("linux", envOf(map[string]string{"HOME": "/home/exile"}), nil)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, none, paths)
}

func TestResolveUsesProcessEnv(t *testing.T) {
	// Whatever the environment holds, Resolve must agree with ResolveWith(os.LookupEnv).
	expected, errW := ResolveWith(os.LookupEnv)
	got, err := Resolve()
	if (err == nil) != (errW == nil) {
		t.Fatal(err, errW)
	}
	assertEqual(t, got, expected)
}

// The layouts of `directories::ProjectDirs::from("app", "", "rkl")` (directories 6).
func TestResolvePerOSLayouts(t *testing.T) {
	cases := []struct {
		name string
		goos string
		env  map[string]string
		want Paths
	}{
		{
			name: "linux defaults",
			goos: "linux",
			env:  map[string]string{"HOME": "/home/exile"},
			want: Paths{
				ConfigDir:    "/home/exile/.config/rkl",
				CacheDir:     "/home/exile/.cache/rkl",
				LogDir:       "/home/exile/.local/state/rkl/logs",
				SettingsFile: "/home/exile/.config/rkl/settings.toml",
			},
		},
		{
			name: "linux xdg overrides",
			goos: "linux",
			env: map[string]string{
				"HOME": "/home/exile", "XDG_CONFIG_HOME": "/cfg", "XDG_CACHE_HOME": "/var/cache/",
				"XDG_STATE_HOME": "/st", "XDG_DATA_HOME": "/data",
			},
			want: Paths{
				ConfigDir:    "/cfg/rkl",
				CacheDir:     "/var/cache/rkl",
				LogDir:       "/st/rkl/logs",
				SettingsFile: "/cfg/rkl/settings.toml",
			},
		},
		{
			name: "linux relative xdg ignored",
			goos: "linux",
			env:  map[string]string{"HOME": "/home/exile", "XDG_CONFIG_HOME": "cfg", "XDG_STATE_HOME": ""},
			want: Paths{
				ConfigDir:    "/home/exile/.config/rkl",
				CacheDir:     "/home/exile/.cache/rkl",
				LogDir:       "/home/exile/.local/state/rkl/logs",
				SettingsFile: "/home/exile/.config/rkl/settings.toml",
			},
		},
		{
			name: "windows",
			goos: "windows",
			env: map[string]string{
				"APPDATA":      `C:\Users\Exile\AppData\Roaming`,
				"LOCALAPPDATA": `C:\Users\Exile\AppData\Local`,
			},
			want: Paths{
				ConfigDir:    `C:\Users\Exile\AppData\Roaming\rkl\config`,
				CacheDir:     `C:\Users\Exile\AppData\Local\rkl\cache`,
				LogDir:       `C:\Users\Exile\AppData\Local\rkl\data\logs`,
				SettingsFile: `C:\Users\Exile\AppData\Roaming\rkl\config\settings.toml`,
			},
		},
		{
			name: "macos",
			goos: "darwin",
			env:  map[string]string{"HOME": "/Users/exile", "XDG_CONFIG_HOME": "/ignored"},
			want: Paths{
				ConfigDir:    "/Users/exile/Library/Application Support/app.rkl",
				CacheDir:     "/Users/exile/Library/Caches/app.rkl",
				LogDir:       "/Users/exile/Library/Application Support/app.rkl/logs",
				SettingsFile: "/Users/exile/Library/Application Support/app.rkl/settings.toml",
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := resolveFor(c.goos, envOf(c.env), nil)
			if err != nil {
				t.Fatal(err)
			}
			assertEqual(t, got, c.want)
		})
	}
}

func TestResolveWithoutHomeIsNoConfigDir(t *testing.T) {
	for _, goos := range []string{"linux", "darwin", "windows"} {
		env := envOf(map[string]string{"HOME": "", "APPDATA": `C:\x`}) // LOCALAPPDATA missing
		if _, err := resolveFor(goos, env, nil); !errors.Is(err, ErrNoConfigDir) {
			t.Errorf("%s: %v", goos, err)
		}
	}
	// HOME empty falls back to the passwd entry (dirs-sys).
	got, err := resolveFor("linux", envOf(map[string]string{"HOME": ""}), func() string { return "/pw/home" })
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, got.ConfigDir, "/pw/home/.config/rkl")
}

func TestLegacySettingsFilePerOS(t *testing.T) {
	cases := []struct {
		goos string
		env  map[string]string
		want string
	}{
		{"linux", map[string]string{"HOME": "/home/exile"}, "/home/exile/.config/kalandra/settings.toml"},
		{"linux", map[string]string{"HOME": "/home/exile", "XDG_CONFIG_HOME": "/cfg"}, "/cfg/kalandra/settings.toml"},
		{"darwin", map[string]string{"HOME": "/Users/exile"}, "/Users/exile/Library/Application Support/au.nerv.kalandra/settings.toml"},
		{"windows", map[string]string{"APPDATA": `C:\R`, "LOCALAPPDATA": `C:\L`}, `C:\R\nerv\kalandra\config\settings.toml`},
	}
	for _, c := range cases {
		got, ok := legacySettingsFileFor(c.goos, envOf(c.env), nil)
		if !ok || got != c.want {
			t.Errorf("%s: %q, %v; want %q", c.goos, got, ok, c.want)
		}
	}
	if _, ok := legacySettingsFileFor("linux", envOf(nil), nil); ok {
		t.Error("no home, yet a legacy file")
	}
}

func TestJoinForMatchesRustPathJoin(t *testing.T) {
	cases := []struct{ goos, base, p, want string }{
		{"linux", "/a", "b", "/a/b"},
		{"linux", "/a/", "b", "/a/b"},
		{"linux", "/a", "/b", "/b"},
		{"linux", "", "b", "b"},
		{"linux", "/home/x", ".local/share", "/home/x/.local/share"},
		{"windows", `C:\a`, "b", `C:\a\b`},
		{"windows", `C:\a\`, "b", `C:\a\b`},
		{"windows", "", "rkl", "rkl"},
		{"windows", "nerv", "kalandra", `nerv\kalandra`},
	}
	for _, c := range cases {
		if got := joinFor(c.goos, c.base, c.p); got != c.want {
			t.Errorf("join(%q, %q) on %s = %q, want %q", c.base, c.p, c.goos, got, c.want)
		}
	}
}
