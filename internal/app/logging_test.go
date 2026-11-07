package app

import (
	"bytes"
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogLevelFromRKLLog(t *testing.T) {
	for env, want := range map[string]slog.Level{
		"": slog.LevelInfo, "info": slog.LevelInfo, "debug": slog.LevelDebug, "trace,enigo=debug": slog.LevelDebug,
		" WARN ": slog.LevelWarn, "error": slog.LevelError, "debug,reqwest=info": slog.LevelDebug, "bogus": slog.LevelInfo,
	} {
		assert.Equal(t, want, LogLevel(env), env)
	}
}

func TestLoggerWritesToStderrAndTheFileOnceAttached(t *testing.T) {
	var stderr bytes.Buffer
	var file LogFile
	t.Setenv("RKL_LOG", "info")
	prev := slog.Default()
	defer slog.SetDefault(prev)
	logger := NewLogger(&stderr, &file)
	logger.Info("before attach")
	path, err := file.Attach(t.TempDir())
	require.NoError(t, err)
	logger.Info("after attach")
	logger.Debug("hidden at info")
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.NotContains(t, string(b), "before attach")
	assert.Contains(t, string(b), "after attach")
	assert.Contains(t, stderr.String(), "before attach")
	assert.NotContains(t, stderr.String(), "hidden at info")
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}
