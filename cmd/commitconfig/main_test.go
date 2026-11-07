package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const sample = `header_max_length = 50

[types]
fix = "A bug fix"
feat = "A new feature"

[scopes]
web = "web/"
core = "internal/core"
`

func TestGeneratesSortedConfigAndDocs(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, ".commit-config.toml"), []byte(sample), 0o666))
	require.Error(t, run(root, true), "missing outputs are stale")
	require.NoError(t, run(root, false))
	require.NoError(t, run(root, true))

	rc, err := os.ReadFile(filepath.Join(root, ".commitlintrc.json"))
	require.NoError(t, err)
	assert.Contains(t, string(rc), "\"header-max-length\": [\n      2,\n      \"always\",\n      50\n    ]")
	assert.Contains(t, string(rc), "\"feat\",\n        \"fix\"")
	assert.Contains(t, string(rc), "\"core\",\n        \"web\"")

	md, err := os.ReadFile(filepath.Join(root, "docs", "contributing", "commit-message.md"))
	require.NoError(t, err)
	assert.Contains(t, string(md), "at most 50 characters")
	assert.Contains(t, string(md), "| `core` | internal/core |\n| `web` | web/ |\n")
}

func TestCheckFailsWhenStale(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".commit-config.toml")
	require.NoError(t, os.WriteFile(path, []byte(sample), 0o666))
	require.NoError(t, run(root, false))
	require.NoError(t, os.WriteFile(path, []byte(sample+"perf = \"faster\"\n"), 0o666))
	assert.ErrorContains(t, run(root, true), "out of date")
}

func TestRejectsIncompleteConfig(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, ".commit-config.toml"), []byte("[types]\nfix = \"x\"\n"), 0o666))
	assert.ErrorContains(t, run(root, false), "required")
}
