package app

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// LogLevel turns an DIVVY_LOG value into a level: its first comma-separated directive (`debug`,
// `info`, `warn`, `error`; `trace` counts as debug), info by default. Later directives
// (`reqwest=info`, kept by older configurations) are ignored.
func LogLevel(env string) slog.Level {
	first, _, _ := strings.Cut(strings.TrimSpace(env), ",")
	switch strings.ToLower(strings.TrimSpace(first)) {
	case "trace", "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	}
	return slog.LevelInfo
}

// logFileName is the log file's name in the log directory.
const logFileName = "divvy.log"

// LogFile is the log file sink: it discards everything until Attach opens the file, so the
// second instance of a single-instance launch never truncates the running instance's log.
type LogFile struct {
	mu sync.Mutex
	f  *os.File
}

func (l *LogFile) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return len(p), nil
	}
	return l.f.Write(p)
}

// Attach creates (truncates) dir/divvy.log, owner-only (it can hold whispers and character
// names), and starts writing to it. It returns the file's path.
func (l *LogFile) Attach(dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, logFileName)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return "", err
	}
	// The mode only applies on creation; tighten a pre-existing file too.
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return "", err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f != nil {
		l.f.Close()
	}
	l.f = f
	return path, nil
}

// Close closes the file; later writes are discarded.
func (l *LogFile) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return nil
	}
	err := l.f.Close()
	l.f = nil
	return err
}

// NewLogger logs text records at level to stderr and to file.
func NewLogger(level slog.Level, stderr io.Writer, file *LogFile) *slog.Logger {
	opts := &slog.HandlerOptions{Level: level}
	return slog.New(slog.NewMultiHandler(
		slog.NewTextHandler(stderr, opts),
		slog.NewTextHandler(file, opts),
	))
}
