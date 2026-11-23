package app

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
)

// Files in the cache directory: losing one only loses a preference.
const (
	// positionFile holds the last dragged panel position (`{"x":..,"y":..}`).
	positionFile = "panel-position.json"
	// filterMemoryFile holds the remembered stat choices.
	filterMemoryFile = "filter-memory.json"
)

// writeFileAtomic replaces path with data through a temporary file in the same directory, so
// a reader never sees a partial file and concurrent writers never share a temporary file.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name()) // after a successful rename there is nothing to remove
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

type savedPosition struct {
	X int32 `json:"x"`
	Y int32 `json:"y"`
}

// loadPosition reads the last dragged panel position; ok is false when there is none or the
// file is unreadable.
func loadPosition(path string) (p point, ok bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return point{}, false
	}
	var saved savedPosition
	if json.Unmarshal(b, &saved) != nil {
		return point{}, false
	}
	return point(saved), true
}

// savePosition writes the panel position.
func savePosition(path string, p point) error {
	b, err := json.Marshal(savedPosition(p))
	if err != nil {
		return err
	}
	return writeFileAtomic(path, b)
}
