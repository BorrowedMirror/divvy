// Package gamelog reads what the chat placeholders need from the game's `Client.txt`: the last
// whisper partner (`@last`) and the current area (`@area`). The log is read on demand, backwards
// from the end, when a command uses one; nothing watches the file.
package gamelog

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"
	"unicode"
)

// Vars are the values found in the log, newest wins. Empty means not found.
type Vars struct {
	LastWhisper string
	Area        string
}

// complete reports whether every wanted value has been found.
func (v Vars) complete(wantLast, wantArea bool) bool {
	return (!wantLast || v.LastWhisper != "") && (!wantArea || v.Area != "")
}

// message is the game's message text: everything after the
// `YYYY/MM/DD HH:MM:SS ticks id [INFO Client n] ` prefix.
func message(line string) string {
	if _, m, ok := strings.Cut(line, "] "); ok {
		return m
	}
	return line
}

// whisperPartner maps `@From <GUILD> Name: hi` or `@To Name: hi` (either direction) to `Name`;
// "" for any other line.
func whisperPartner(line string) string {
	msg := message(line)
	rest, ok := strings.CutPrefix(msg, "@From ")
	if !ok {
		if rest, ok = strings.CutPrefix(msg, "@To "); !ok {
			return ""
		}
	}
	if tagged, ok := strings.CutPrefix(rest, "<"); ok {
		if _, rest, ok = strings.Cut(tagged, "> "); !ok {
			return ""
		}
	}
	name, _, ok := strings.Cut(rest, ": ")
	if !ok || strings.ContainsFunc(name, unicode.IsSpace) {
		return ""
	}
	return name
}

// sceneArea maps `[SCENE] Set Source [Felled Hideout]` to `Felled Hideout`; "" for any other
// line. The game also logs `(null)` and `(unknown)` while loading; those are skipped.
func sceneArea(line string) string {
	area, ok := strings.CutPrefix(message(line), "[SCENE] Set Source [")
	if !ok {
		return ""
	}
	if area, ok = strings.CutSuffix(area, "]"); !ok || strings.HasPrefix(area, "(") {
		return ""
	}
	return area
}

// scan scans lines (newest first) until every wanted value is found.
func scan(lines []string, wantLast, wantArea bool) Vars {
	var vars Vars
	for _, line := range lines {
		if wantLast && vars.LastWhisper == "" {
			vars.LastWhisper = whisperPartner(line)
		}
		if wantArea && vars.Area == "" {
			vars.Area = sceneArea(line)
		}
		if vars.complete(wantLast, wantArea) {
			break
		}
	}
	return vars
}

// newestFirst splits text into lines without their line endings, newest (last) first.
func newestFirst(text string) []string {
	var lines []string
	for l := range strings.Lines(text) {
		lines = append(lines, strings.TrimRight(l, "\r\n"))
	}
	slices.Reverse(lines)
	return lines
}

// Read reads the log at path from the end in growing windows (256 KiB, 4 MiB, 32 MiB) until
// the wanted values turn up. A long session writes megabytes of log, so the last whisper can be
// far back.
func Read(path string, wantLast, wantArea bool) (Vars, error) {
	f, err := os.Open(path)
	if err != nil {
		return Vars{}, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return Vars{}, err
	}
	size := info.Size()
	var vars Vars
	for _, window := range []int64{1 << 18, 1 << 22, 1 << 25} {
		start := max(size-window, 0)
		buf, err := io.ReadAll(io.NewSectionReader(f, start, size-start))
		if err != nil {
			return Vars{}, err
		}
		text := string(bytes.ToValidUTF8(buf, []byte("\uFFFD")))
		// A window that starts mid-file starts mid-line: drop that partial line.
		if start > 0 {
			_, text, _ = strings.Cut(text, "\n")
		}
		vars = scan(newestFirst(text), wantLast, wantArea)
		if vars.complete(wantLast, wantArea) || start == 0 {
			break
		}
	}
	return vars, nil
}

// steamLibraryPaths lists the library folders in Steam's `libraryfolders.vdf`
// (`"path"  "D:\\SteamLibrary"`).
func steamLibraryPaths(vdf string) []string {
	var out []string
	for l := range strings.Lines(vdf) {
		rest, ok := strings.CutPrefix(strings.TrimSpace(l), `"path"`)
		if !ok {
			continue
		}
		quoted, ok := strings.CutPrefix(strings.TrimSpace(rest), `"`)
		if !ok {
			continue
		}
		if quoted, ok = strings.CutSuffix(quoted, `"`); !ok {
			continue
		}
		out = append(out, strings.ReplaceAll(quoted, `\\`, `\`))
	}
	return out
}

const gameDir = "Path of Exile 2"

// steamLog is where the log can be for a Steam root or library folder.
func steamLog(library string) string {
	return filepath.Join(library, "steamapps", "common", gameDir, "logs", "Client.txt")
}

// candidates lists every place the log usually is: each Steam library (from the Steam roots'
// library lists) and the standalone client's default install. home is the user's home
// directory ("" when unknown); it is not used on Windows.
func candidates(home string) []string {
	var roots, out []string
	if runtime.GOOS == "windows" {
		roots = append(roots, `C:\Program Files (x86)\Steam`)
		out = append(out, `C:\Program Files (x86)\Grinding Gear Games\Path of Exile 2\logs\Client.txt`)
	} else if home != "" {
		for _, root := range []string{".local/share/Steam", ".steam/steam", ".var/app/com.valvesoftware.Steam/.local/share/Steam"} {
			roots = append(roots, filepath.Join(home, root))
		}
		out = append(out, filepath.Join(home, ".wine/drive_c/Program Files (x86)/Grinding Gear Games", gameDir, "logs/Client.txt"))
	}
	for _, root := range roots {
		libraries := []string{root}
		if vdf, err := os.ReadFile(filepath.Join(root, "steamapps", "libraryfolders.vdf")); err == nil {
			libraries = append(libraries, steamLibraryPaths(string(vdf))...)
		}
		for _, lib := range libraries {
			if log := steamLog(lib); !slices.Contains(out, log) {
				out = append(out, log)
			}
		}
	}
	return out
}

// Resolve picks the log to read: the configured path when set, otherwise the most recently
// written candidate that exists (ties go to the later candidate). ok is false when there is
// none.
func Resolve(configured string) (path string, ok bool) {
	if configured = strings.TrimSpace(configured); configured != "" {
		if info, err := os.Stat(configured); err == nil && info.Mode().IsRegular() {
			return configured, true
		}
		return "", false
	}
	home, _ := os.UserHomeDir() // "" when unknown: only the fixed Windows paths remain
	var newest time.Time
	for _, p := range candidates(home) {
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		if t := info.ModTime(); !ok || !t.Before(newest) {
			path, newest, ok = p, t, true
		}
	}
	return path, ok
}
