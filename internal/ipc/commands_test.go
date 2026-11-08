package ipc

import (
	"encoding/json/v2"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDragPhaseAcceptsOnlyItsValues(t *testing.T) {
	var p DragPhase
	require.NoError(t, json.Unmarshal([]byte(`"move"`), &p))
	assert.Equal(t, DragMove, p)
	require.Error(t, json.Unmarshal([]byte(`"Move"`), &p))
}
