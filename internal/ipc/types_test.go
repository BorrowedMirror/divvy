package ipc

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/BorrowedMirror/rust-king-ledger/internal/config"
	"github.com/BorrowedMirror/rust-king-ledger/internal/platform"
	"github.com/BorrowedMirror/rust-king-ledger/internal/trade"
)

type kinded struct{ msg string }

func (k kinded) Error() string { return k.msg }
func (kinded) IPCKind() string { return "parse" }

func TestFromMapsEveryErrorKind(t *testing.T) {
	assert.Nil(t, From(nil))

	own := NewError("busy", "a price check is running")
	assert.Same(t, own, From(fmt.Errorf("wrapped: %w", own)))

	assert.Equal(t, &IpcError{Kind: "parse", Message: "not an item"}, From(kinded{"not an item"}))
	assert.Equal(t, "platform", From(platform.Timeout(time.Second)).Kind)
	assert.Equal(t, "config", From(&config.TooNewError{Found: 9}).Kind)
	assert.Equal(t, "config", From(config.ErrNoConfigDir).Kind)
	assert.Equal(t, &IpcError{Kind: "internal", Message: "boom"}, From(errors.New("boom")))

	limited := From(&trade.Error{Kind: trade.KindRateLimited, RetryAfter: 1500 * time.Millisecond})
	assert.Equal(t, "rate_limited", limited.Kind)
	require.NotNil(t, limited.RetryAfterMs)
	assert.EqualValues(t, 1500, *limited.RetryAfterMs)
}

func TestErrorJSONIsSerdeShaped(t *testing.T) {
	b, err := json.Marshal(NewError("no_item", "no item has been copied yet"))
	require.NoError(t, err)
	assert.JSONEq(t, `{"kind":"no_item","message":"no item has been copied yet","retry_after_ms":null}`, string(b))

	var back IpcError
	require.NoError(t, json.Unmarshal([]byte(`{"kind":"trade","message":"x","retry_after_ms":250}`), &back))
	assert.Equal(t, "trade", back.Kind)
	assert.EqualValues(t, 250, *back.RetryAfterMs)
}

func TestDragPhaseAcceptsOnlyItsVariants(t *testing.T) {
	var p DragPhase
	require.NoError(t, json.Unmarshal([]byte(`"move"`), &p))
	assert.Equal(t, DragMove, p)
	assert.Error(t, json.Unmarshal([]byte(`"Move"`), &p))
}

func TestPayloadShapes(t *testing.T) {
	b, err := json.Marshal(OverlayLayout{Cover: true, X: 12, Y: 3.5})
	require.NoError(t, err)
	assert.Equal(t, `{"cover":true,"x":12.0,"y":3.5}`, string(b))

	b, err = json.Marshal(Diagnostics{Backend: platform.BackendX11})
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))
	assert.Equal(t, []any{}, m["recent_errors"])
	assert.Nil(t, m["league"])
}
