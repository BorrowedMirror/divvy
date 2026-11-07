package app

// What the chat placeholders need from the game's `Client.txt`: the last whisper partner
// (`@last`) and the current area (`@area`). Read on demand, backwards from the end of the log,
// when a command uses one; nothing watches the file.

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

// LogVars are the values found in the log, newest wins. Empty means not found.
type LogVars struct {
	LastWhisper string
	Area        string
}

// logMessage is the game's message text: everything after the
// `YYYY/MM/DD HH:MM:SS ticks id [INFO Client n] ` prefix.
func logMessage(line string) string {
	if _, m, ok := strings.Cut(line, "] "); ok {
		return m
	}
	return line
}

// WhisperPartner maps `@From <GUILD> Name: hi` or `@To Name: hi` (either direction) to `Name`.
func WhisperPartner(line string) (string, bool) {
	msg := logMessage(line)
	rest, ok := strings.CutPrefix(msg, "@From ")
	if !ok {
		if rest, ok = strings.CutPrefix(msg, "@To "); !ok {
			return "", false
		}
	}
	if tagged, ok := strings.CutPrefix(rest, "<"); ok {
		if _, rest, ok = strings.Cut(tagged, "> "); !ok {
			return "", false
		}
	}
	name, _, ok := strings.Cut(rest, ": ")
	if !ok || name == "" || strings.ContainsFunc(name, unicode.IsSpace) {
		return "", false
	}
	return name, true
}

// SceneArea maps `[SCENE] Set Source [Felled Hideout]` to `Felled Hideout`. The game also logs
// `(null)` and `(unknown)` while loading; those are skipped.
func SceneArea(line string) (string, bool) {
	area, ok := strings.CutPrefix(logMessage(line), "[SCENE] Set Source [")
	if !ok {
		return "", false
	}
	if area, ok = strings.CutSuffix(area, "]"); !ok || area == "" || strings.HasPrefix(area, "(") {
		return "", false
	}
	return area, true
}

// ScanLog scans lines (newest first) until every wanted value is found.
func ScanLog(lines []string, wantLast, wantArea bool) LogVars {
	var vars LogVars
	for _, line := range lines {
		if wantLast && vars.LastWhisper == "" {
			vars.LastWhisper, _ = WhisperPartner(line)
		}
		if wantArea && vars.Area == "" {
			vars.Area, _ = SceneArea(line)
		}
		if (!wantLast || vars.LastWhisper != "") && (!wantArea || vars.Area != "") {
			break
		}
	}
	return vars
}

// textLines splits like Rust's str::lines: on `\n`, dropping a trailing `\r` from each line and
// no empty line after a final newline.
func textLines(text string) []string {
	var lines []string
	for l := range strings.Lines(text) {
		if line, ok := strings.CutSuffix(l, "\n"); ok {
			l = strings.TrimSuffix(line, "\r")
		}
		lines = append(lines, l)
	}
	return lines
}

// ReadLogVars reads the log from the end in growing windows (256 KiB, 4 MiB, 32 MiB) until the
// wanted values turn up. A long session writes megabytes of log, so the last whisper can be far
// back.
func ReadLogVars(path string, wantLast, wantArea bool) (LogVars, error) {
	f, err := os.Open(path)
	if err != nil {
		return LogVars{}, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return LogVars{}, err
	}
	size := info.Size()
	var vars LogVars
	for _, window := range []int64{1 << 18, 1 << 22, 1 << 25} {
		start := max(size-window, 0)
		buf, err := io.ReadAll(io.NewSectionReader(f, start, size-start))
		if err != nil {
			return LogVars{}, err
		}
		text := string(bytes.ToValidUTF8(buf, []byte("\uFFFD")))
		// A window that starts mid-file starts mid-line: drop that partial line.
		if start > 0 {
			_, text, _ = strings.Cut(text, "\n")
		}
		lines := textLines(text)
		slices.Reverse(lines)
		vars = ScanLog(lines, wantLast, wantArea)
		done := (!wantLast || vars.LastWhisper != "") && (!wantArea || vars.Area != "")
		if done || start == 0 {
			break
		}
	}
	return vars, nil
}

// SteamLibraryPaths lists the library folders in Steam's `libraryfolders.vdf`
// (`"path"  "D:\\SteamLibrary"`).
func SteamLibraryPaths(vdf string) []string {
	var out []string
	for l := range strings.Lines(vdf) {
		rest, ok := strings.CutPrefix(strings.TrimSpace(l), `"path"`)
		if !ok {
			continue
		}
		rest = strings.TrimSpace(rest)
		quoted, ok := strings.CutPrefix(rest, `"`)
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

// LogCandidates lists every place the log usually is: each Steam library (from the Steam roots'
// library lists) and the standalone client's default install. home is the user's home
// directory ("" when unknown); it is not used on Windows.
func LogCandidates(home string) []string {
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
			libraries = append(libraries, SteamLibraryPaths(string(vdf))...)
		}
		for _, lib := range libraries {
			if log := steamLog(lib); !slices.Contains(out, log) {
				out = append(out, log)
			}
		}
	}
	return out
}

// ResolveLogPath picks the log to read: the configured path when set, otherwise the most
// recently written candidate that exists.
func ResolveLogPath(configured string) (string, bool) {
	if configured = strings.TrimSpace(configured); configured != "" {
		if info, err := os.Stat(configured); err == nil && info.Mode().IsRegular() {
			return configured, true
		}
		return "", false
	}
	home, ok := os.LookupEnv("HOME")
	if !ok {
		home = os.Getenv("USERPROFILE")
	}
	var best string
	var bestTime time.Time
	found := false
	for _, p := range LogCandidates(home) {
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		// Ties go to the later candidate, like Iterator::max_by_key.
		if t := info.ModTime(); !found || !t.Before(bestTime) {
			best, bestTime, found = p, t, true
		}
	}
	return best, found
}
