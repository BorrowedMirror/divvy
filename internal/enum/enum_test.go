package enum

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type phase string

func TestParseAcceptsOnlyTheValues(t *testing.T) {
	var p phase
	require.NoError(t, Parse(&p, []byte("end"), "start", "end"))
	assert.Equal(t, phase("end"), p)

	require.EqualError(t, Parse(&p, []byte("End"), "start", "end"), `unknown enum.phase "End"`)
	assert.Equal(t, phase("end"), p, "unchanged on error")
}
