package app

import (
	"bytes"
	"log/slog"
	"os"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogLevelFromDivvyLog(t *testing.T) {
	testCases := []struct {
		name     string
		have     string
		expected slog.Level
	}{
		{
			"EmptyIsInfo",
			"",
			slog.LevelInfo,
		},
		{
			"Info",
			"info",
			slog.LevelInfo,
		},
		{
			"Debug",
			"debug",
			slog.LevelDebug,
		},
		{
			"TraceWithTargetIsDebug",
			"trace,enigo=debug",
			slog.LevelDebug,
		},
		{
			"PaddedUppercaseWarn",
			" WARN ",
			slog.LevelWarn,
		},
		{
			"Error",
			"error",
			slog.LevelError,
		},
		{
			"DebugWithTarget",
			"debug,reqwest=info",
			slog.LevelDebug,
		},
		{
			"UnknownIsInfo",
			"bogus",
			slog.LevelInfo,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, LogLevel(tc.have))
		})
	}
}

func TestLoggerWritesToStderrAndTheFileOnceAttached(t *testing.T) {
	var stderr bytes.Buffer
	var file LogFile
	logger := NewLogger(slog.LevelInfo, &stderr, &file)
	logger.Info("before attach")
	path, err := file.Attach(t.TempDir())
	require.NoError(t, err)
	logger.Info("after attach")
	logger.Debug("hidden at info")
	require.NoError(t, file.Close())
	logger.Info("after close")

	b, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.NotContains(t, string(b), "before attach")
	assert.Contains(t, string(b), "after attach")
	assert.NotContains(t, string(b), "after close")
	assert.Contains(t, stderr.String(), "before attach")
	assert.NotContains(t, stderr.String(), "hidden at info")
	if runtime.GOOS == "windows" {
		return // no Unix permission bits: Go reports any writable file as 0666
	}
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}
