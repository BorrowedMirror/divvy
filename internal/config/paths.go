package config

import (
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
)

// Paths are the resolved per-OS locations.
type Paths struct {
	ConfigDir    string
	CacheDir     string
	LogDir       string
	SettingsFile string
}

// ConfigDirEnv relocates every path under one root (portable mode, tests).
const ConfigDirEnv = "RKL_CONFIG_DIR"

// The `directories::ProjectDirs` triple: `~/.config/rkl` (Linux), `%APPDATA%\rkl` (Windows),
// `~/Library/Application Support/app.rkl` (macOS). Matches the bundle identifier `app.rkl`.
const (
	ProjectQualifier = "app"
	ProjectOrg       = ""
	ProjectApp       = "rkl"
)

// Env looks up an environment variable, like [os.LookupEnv].
type Env func(key string) (string, bool)

// Resolve resolves the paths from the process environment (see [ResolveWith]).
func Resolve() (Paths, error) { return ResolveWith(os.LookupEnv) }

// ResolveWith resolves the paths with an injectable environment lookup: RKL_CONFIG_DIR (when
// not blank) roots everything there ([Under]); otherwise the directories are
// `directories::ProjectDirs` for app.rkl on this OS, read from env (HOME, XDG_*, APPDATA,
// LOCALAPPDATA). Logs go to the state dir (Linux) or the local data dir, under `logs`.
func ResolveWith(env Env) (Paths, error) {
	return resolveFor(runtime.GOOS, env, userHome)
}

// Under roots all paths under root (tests, portable mode).
func Under(root string) Paths {
	return Paths{
		ConfigDir:    filepath.Join(root, "config"),
		CacheDir:     filepath.Join(root, "cache"),
		LogDir:       filepath.Join(root, "logs"),
		SettingsFile: filepath.Join(root, "config", "settings.toml"),
	}
}

// LegacySettingsFile is where settings lived before the project was renamed from kalandra
// (`au.nerv.kalandra`); false when no home directory is known.
func LegacySettingsFile() (string, bool) {
	return legacySettingsFileFor(runtime.GOOS, os.LookupEnv, userHome)
}

func legacySettingsFileFor(goos string, env Env, home func() string) (string, bool) {
	d, ok := projectDirsFor(goos, "au", "nerv", "kalandra", env, home)
	if !ok {
		return "", false
	}
	return joinFor(goos, d.config, "settings.toml"), true
}

func resolveFor(goos string, env Env, home func() string) (Paths, error) {
	if root, ok := env(ConfigDirEnv); ok && strings.TrimSpace(root) != "" {
		return Under(root), nil
	}
	d, ok := projectDirsFor(goos, ProjectQualifier, ProjectOrg, ProjectApp, env, home)
	if !ok {
		return Paths{}, ErrNoConfigDir
	}
	logBase := d.state
	if logBase == "" {
		logBase = d.dataLocal
	}
	return Paths{
		ConfigDir:    d.config,
		CacheDir:     d.cache,
		LogDir:       joinFor(goos, logBase, "logs"),
		SettingsFile: joinFor(goos, d.config, "settings.toml"),
	}, nil
}

// userHome is dirs-sys's getpwuid fallback for an unset or empty HOME.
func userHome() string {
	if u, err := user.Current(); err == nil {
		return u.HomeDir
	}
	return ""
}

// projectDirs is the subset of `directories::ProjectDirs` used here; state is "" when the
// OS has none.
type projectDirs struct {
	config, cache, dataLocal, state string
}

// projectDirsFor reproduces `directories::ProjectDirs::from` (directories 6) for goos.
func projectDirsFor(goos, qualifier, org, app string, env Env, homeFallback func() string) (projectDirs, bool) {
	get := func(k string) string {
		v, _ := env(k)
		return v
	}
	// dirs_sys::home_dir: $HOME unless empty, else the passwd entry.
	home := func() string {
		if h := get("HOME"); h != "" {
			return h
		}
		if homeFallback != nil {
			return homeFallback()
		}
		return ""
	}
	switch goos {
	case "windows":
		// Known folders FOLDERID_RoamingAppData / FOLDERID_LocalAppData.
		roaming, local := get("APPDATA"), get("LOCALAPPDATA")
		if roaming == "" || local == "" {
			return projectDirs{}, false
		}
		project := joinFor(goos, org, app) // PathBuf::from_iter([org, app])
		roaming = joinFor(goos, roaming, project)
		local = joinFor(goos, local, project)
		return projectDirs{
			config:    joinFor(goos, roaming, "config"),
			cache:     joinFor(goos, local, "cache"),
			dataLocal: joinFor(goos, local, "data"),
		}, true
	case "darwin", "ios":
		h := home()
		if h == "" {
			return projectDirs{}, false
		}
		parts := []string{}
		for _, p := range []string{qualifier, strings.ReplaceAll(org, " ", "-"), strings.ReplaceAll(app, " ", "-")} {
			if p != "" {
				parts = append(parts, p)
			}
		}
		bundle := strings.Join(parts, ".")
		config := joinFor(goos, joinFor(goos, h, "Library/Application Support"), bundle)
		return projectDirs{
			config:    config,
			cache:     joinFor(goos, joinFor(goos, h, "Library/Caches"), bundle),
			dataLocal: config,
		}, true
	default:
		h := home()
		if h == "" {
			return projectDirs{}, false
		}
		// trim_and_lowercase_then_replace_spaces(application, "")
		project := strings.ToLower(strings.Join(strings.Fields(app), ""))
		xdg := func(envKey, fallback string) string {
			base := get(envKey)
			if !strings.HasPrefix(base, "/") { // dirs_sys::is_absolute_path
				base = joinFor(goos, h, fallback)
			}
			return joinFor(goos, base, project)
		}
		data := xdg("XDG_DATA_HOME", ".local/share")
		return projectDirs{
			config:    xdg("XDG_CONFIG_HOME", ".config"),
			cache:     xdg("XDG_CACHE_HOME", ".cache"),
			dataLocal: data,
			state:     xdg("XDG_STATE_HOME", ".local/state"),
		}, true
	}
}

// joinFor is Rust's `Path::join` for goos's path syntax (no cleaning): an absolute p
// replaces base, an empty base yields p, and a separator is added only when base does not
// already end in one.
func joinFor(goos, base, p string) string {
	isSep := func(c byte) bool { return c == '/' }
	sep := "/"
	if goos == "windows" {
		isSep = func(c byte) bool { return c == '/' || c == '\\' }
		sep = `\`
	}
	switch {
	case p == "":
		if base == "" || isSep(base[len(base)-1]) {
			return base
		}
		return base + sep
	case isSep(p[0]) && goos != "windows",
		goos == "windows" && len(p) >= 3 && p[1] == ':' && isSep(p[2]):
		return p
	case base == "":
		return p
	case isSep(base[len(base)-1]):
		return base + p
	}
	return base + sep + p
}
