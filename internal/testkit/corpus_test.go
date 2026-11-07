package testkit

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCorpusLoads(t *testing.T) {
	items, err := LoadCorpus()
	require.NoError(t, err)
	require.NotEmpty(t, items)
	for _, it := range items {
		assert.True(t, strings.HasPrefix(it.Text, "Item Class:"), "%s must start with Item Class:", it.Entry.ID)
	}
}
