package ipc

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"io/fs"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/BorrowedMirror/divvy/internal/config"
	"github.com/BorrowedMirror/divvy/internal/platform"
	"github.com/BorrowedMirror/divvy/internal/trade"
)

type kinded struct{ msg string }

func (k kinded) Error() string { return k.msg }
func (kinded) IPCKind() Kind   { return KindParse }

func TestFromMapsEveryErrorKind(t *testing.T) {
	assert.Nil(t, From(nil))

	own := NewError(KindBusy, "a price check is running")
	assert.Same(t, own, From(fmt.Errorf("wrapped: %w", own)))

	parse := kinded{"not an item"}
	assert.Equal(t, &Error{Kind: KindParse, Message: "not an item", Err: parse}, From(parse))
	assert.Equal(t, KindPlatform, From(platform.Timeout(time.Second)).Kind)
	assert.Equal(t, KindConfig, From(&config.TooNewError{Found: 9}).Kind)
	assert.Equal(t, KindConfig, From(config.ErrNoConfigDir).Kind)
	boom := errors.New("boom")
	assert.Equal(t, &Error{Kind: KindInternal, Message: "boom", Err: boom}, From(boom))

	limited := From(&trade.Error{Kind: trade.KindRateLimited, RetryAfter: 1500 * time.Millisecond})
	assert.Equal(t, KindRateLimited, limited.Kind)
	require.NotNil(t, limited.RetryAfterMS)
	assert.EqualValues(t, 1500, *limited.RetryAfterMS)
	assert.Equal(t, KindTrade, From(&trade.Error{Kind: trade.KindHTTP}).Kind)
}

func TestErrorKeepsItsCause(t *testing.T) {
	err := Errorf(KindUpdate, "update failed: %w", fs.ErrNotExist)
	assert.Equal(t, "update failed: file does not exist", err.Message)
	require.ErrorIs(t, err, fs.ErrNotExist)
	require.ErrorIs(t, From(fmt.Errorf("saving: %w", fs.ErrPermission)), fs.ErrPermission)

	require.NoError(t, Errorf(KindInternal, "no cause %d", 1).Unwrap())
	assert.Equal(t, "update: update failed: file does not exist", err.Error())
	both := Errorf(KindInternal, "%w and %w", fs.ErrExist, fs.ErrClosed)
	require.ErrorIs(t, both, fs.ErrExist)
	require.ErrorIs(t, both, fs.ErrClosed)
}

func TestErrorJSON(t *testing.T) {
	b, err := json.Marshal(Errorf(KindNoItem, "no item: %w", fs.ErrNotExist))
	require.NoError(t, err)
	assert.JSONEq(t, `{"kind":"no_item","message":"no item: file does not exist","retry_after_ms":null}`, string(b))

	var back Error
	require.NoError(t, json.Unmarshal([]byte(`{"kind":"trade","message":"x","retry_after_ms":250}`), &back))
	assert.Equal(t, KindTrade, back.Kind)
	assert.EqualValues(t, 250, *back.RetryAfterMS)
}
