package bindgen

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/BorrowedMirror/rust-king-ledger/internal/bindgen/fixture"
)

func generate(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	require.NoError(t, err)
	out, err := Generate(Spec{
		Service:    reflect.TypeFor[fixture.Service](),
		ModulePath: "github.com/BorrowedMirror/rust-king-ledger",
		ModuleDir:  filepath.Join(wd, "..", ".."),
		Literal:    map[reflect.Type]string{reflect.TypeFor[fixture.Mode](): `"a" | "b"`},
		Events:     []Event{{Name: "outerChanged", Wire: "outer-changed", Payload: reflect.TypeFor[fixture.Outer]()}},
	})
	require.NoError(t, err)
	return string(out)
}

func TestCommandsUseRustCommandNamesAndVariants(t *testing.T) {
	out := generate(t)
	// Source order, context parameter dropped, parameters accept _Deserialize, results are
	// _Serialize, fallible methods go through typedError.
	get := strings.Index(out, "\tget: (which: Outer_Deserialize, mode: Mode) => typedError<Outer_Serialize, IpcError_Serialize>(invoke<Outer_Serialize>(\"get\", { which, mode })),")
	count := strings.Index(out, "\tcount: () => invoke<number>(\"count\", {}),")
	clear := strings.Index(out, "\tclear: () => typedError<null, IpcError_Serialize>(invoke<null>(\"clear\", {})),")
	assert.True(t, get >= 0 && count > get && clear > count, out)
	assert.Contains(t, out, "\t/** Get returns the thing. */\n")
	assert.Contains(t, out, "\tget: [\"github.com/BorrowedMirror/rust-king-ledger/internal/bindgen/fixture.Service.Get\", [\"which\", \"mode\"]],")
	assert.Contains(t, out, "\touterChanged: makeEvent<Outer_Serialize>(\"outer-changed\"),")
}

func TestStructVariantsAndFields(t *testing.T) {
	out := generate(t)
	assert.Contains(t, out, "export type Outer = Outer_Serialize | Outer_Deserialize;")
	assert.Contains(t, out, "/** Outer is a test payload. */\n")
	assert.Contains(t, out, "\t/** Name of the thing. */\n\tname: string,\n")
	// Nested structs follow the variant; floats may be null; maps are plain index types.
	assert.Contains(t, out, "\tinner: Inner_Serialize,\n\tratio: number | null,\n\ttags: { [key in string]: Mode },")
	assert.Contains(t, out, "\tinner: Inner_Deserialize,")
	assert.Contains(t, out, "\tlist: Inner_Serialize[],")
	// Pointers are nullable and optional when sent; ts:"optional" is optional when sent.
	assert.Contains(t, out, "\tmaybe: Inner_Serialize | null,")
	assert.Contains(t, out, "\tmaybe?: Inner_Deserialize | null,")
	assert.Contains(t, out, "\textra?: string,")
	assert.NotContains(t, out, "Hidden")
	assert.NotContains(t, out, "byHand")
	assert.Contains(t, out, "export type Mode = \"a\" | \"b\";")
}

func TestUnknownStringEnumIsAnError(t *testing.T) {
	wd, err := os.Getwd()
	require.NoError(t, err)
	_, err = Generate(Spec{
		Service:    reflect.TypeFor[fixture.Service](),
		ModulePath: "github.com/BorrowedMirror/rust-king-ledger",
		ModuleDir:  filepath.Join(wd, "..", ".."),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "string enum needs a Literal entry")
}

func TestMethodWithoutSourceIsAnError(t *testing.T) {
	// Without module paths the generator cannot read parameter names; it must say so rather
	// than emit no commands.
	_, err := Generate(Spec{Service: reflect.TypeFor[fixture.Service]()})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no source found")
}

func TestSnake(t *testing.T) {
	for in, want := range map[string]string{
		"GetSettings": "get_settings", "OpenSettingsTab": "open_settings_tab",
		"RecheckPermissions": "recheck_permissions", "CopyText": "copy_text", "Get": "get",
	} {
		assert.Equal(t, want, snake(in), in)
	}
}
