package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"

	"github.com/knadh/koanf/parsers/toml/v2"
	"github.com/knadh/koanf/providers/file"
)

var tomlParser = toml.Parser()

// Load reads the settings file; a missing file yields the defaults. The document is parsed,
// migrated ([Migrate]), laid over the defaults, decoded, then validated ([Validate]).
func Load(paths Paths) (Settings, error) {
	path := paths.SettingsFile
	doc := newKoanf()
	if err := doc.Load(file.Provider(path), tomlParser); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return DefaultSettings(), nil
		}
		if _, ok := errors.AsType[*fs.PathError](err); ok {
			return Settings{}, ioErr(path, err)
		}
		return Settings{}, &ParseError{Err: err}
	}
	if err := Migrate(doc); err != nil {
		return Settings{}, err
	}
	s, err := decodeDocument(doc.Raw())
	if err != nil {
		return Settings{}, &ParseError{Err: err}
	}
	if err := Validate(&s); err != nil {
		return Settings{}, err
	}
	return s, nil
}

// MarshalTOML encodes settings as the settings file's TOML.
func MarshalTOML(s Settings) ([]byte, error) {
	k := newKoanf()
	if err := k.Load(settingsProvider{s}, nil); err != nil {
		return nil, &SerialiseError{Err: err}
	}
	b, err := k.Marshal(tomlParser)
	if err != nil {
		return nil, &SerialiseError{Err: err}
	}
	return b, nil
}

// saveSeq makes each save's temp file name unique within the process.
var saveSeq atomic.Uint64

// Save validates and atomically writes settings: a temp file in the same directory, fsync,
// rename. New directories and the file are private to the user (0o700 / 0o600), as the
// file may hold a POESESSID.
func Save(paths Paths, settings *Settings) error {
	if err := Validate(settings); err != nil {
		return err
	}
	// Always persist the current schema version so a stale `version` sent over IPC does not
	// make every later Load re-run old migrations.
	current := settings.Clone()
	current.Version = SettingsVersion
	text, err := MarshalTOML(current)
	if err != nil {
		return err
	}
	target := paths.SettingsFile
	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return ioErr(dir, err)
	}
	name := filepath.Base(target)
	if name == "." || name == string(filepath.Separator) {
		name = "settings.toml"
	}
	// Unique per call so concurrent saves in one process never share a temp file.
	seq := saveSeq.Add(1) - 1
	tmp := filepath.Join(dir, fmt.Sprintf(".%s.%d.%d.tmp", name, os.Getpid(), seq))
	write := func() error {
		f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return err
		}
		if _, err := f.Write(text); err != nil {
			_ = f.Close()
			return err
		}
		if err := f.Sync(); err != nil {
			_ = f.Close()
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
		return os.Rename(tmp, target)
	}
	if err := write(); err != nil {
		_ = os.Remove(tmp)
		return ioErr(target, err)
	}
	// Persist the rename itself; best effort, not every platform/filesystem supports it.
	if runtime.GOOS != "windows" {
		if d, err := os.Open(dir); err == nil {
			_ = d.Sync()
			_ = d.Close()
		}
	}
	return nil
}

// AdoptLegacySettings is the one-time move to the new location: if paths has no settings
// file yet but legacy does, copy it across (the old file is left in place). An empty legacy
// path means there is none. Reports whether a copy happened.
func AdoptLegacySettings(paths Paths, legacy string) (bool, error) {
	if legacy == "" {
		return false, nil
	}
	// A legacy file that is missing, unreadable or not a regular file means there is
	// nothing to adopt, not an error.
	if !isRegularFile(legacy) {
		return false, nil
	}
	if _, err := os.Stat(paths.SettingsFile); err == nil {
		return false, nil
	}
	if err := os.MkdirAll(paths.ConfigDir, 0o700); err != nil {
		return false, ioErr(paths.ConfigDir, err)
	}
	if err := copyFile(legacy, paths.SettingsFile); err != nil {
		return false, ioErr(paths.SettingsFile, err)
	}
	return true, nil
}

func isRegularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// copyFile copies src to the new file dst, with src's permission bits (not masked by the
// umask).
func copyFile(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.WriteFile(dst, data, info.Mode().Perm()); err != nil {
		return err
	}
	return os.Chmod(dst, info.Mode().Perm())
}
