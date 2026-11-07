package app

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// LogLevel turns RKL_LOG into a level: its first directive (`debug`, `info`, `warn`, `error`;
// `trace` counts as debug), info by default. Per-target filters from the Rust build
// (`reqwest=info`) are accepted and ignored.
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

// LogFile is the log file sink: nothing until Attach opens it, so the second instance of a
// single-instance launch never truncates the running instance's log.
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

// Attach creates (truncates) dir/rkl.log, owner-only, and starts writing to it. It returns
// the file's path.
func (l *LogFile) Attach(dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "rkl.log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return "", err
	}
	// The mode only applies on creation; tighten a pre-existing file too.
	_ = os.Chmod(path, 0o600)
	l.mu.Lock()
	l.f = f
	l.mu.Unlock()
	return path, nil
}

// NewLogger logs to stderr and to file at the RKL_LOG level, and makes it the default logger.
func NewLogger(stderr io.Writer, file *LogFile) *slog.Logger {
	opts := &slog.HandlerOptions{Level: LogLevel(os.Getenv("RKL_LOG"))}
	logger := slog.New(slog.NewMultiHandler(
		slog.NewTextHandler(stderr, opts),
		slog.NewTextHandler(file, opts),
	))
	slog.SetDefault(logger)
	return logger
}
