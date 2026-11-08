package bindgen

import (
	"html/template"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/BorrowedMirror/rust-king-ledger/internal/bindgen/fixture"
)

func spec(t *testing.T) Spec {
	t.Helper()
	wd, err := os.Getwd()
	require.NoError(t, err)
	return Spec{
		Service:    reflect.TypeFor[fixture.Service](),
		Error:      reflect.TypeFor[fixture.Fault](),
		Names:      map[reflect.Type]string{reflect.TypeFor[fixture.Fault](): "Problem"},
		ModulePath: "github.com/BorrowedMirror/rust-king-ledger",
		ModuleDir:  filepath.Join(wd, "..", ".."),
		Literal:    map[reflect.Type]string{reflect.TypeFor[fixture.Shape](): `{ round: true }`},
		Events:     []Event{{Name: "outerChanged", Wire: "outer-changed", Payload: reflect.TypeFor[fixture.Outer]()}},
	}
}

func generate(t *testing.T) string {
	t.Helper()
	out, err := Generate(spec(t))
	require.NoError(t, err)
	return string(out)
}

func TestCommandsUseSnakeCaseNamesAndVariants(t *testing.T) {
	out := generate(t)
	// Source order, context parameter dropped, parameters accept _Deserialize, results are
	// _Serialize, fallible methods go through typedError with the renamed error type.
	get := strings.Index(out, "\tget: (which: Outer_Deserialize, mode: Mode) => typedError<Outer_Serialize, Problem_Serialize>(invoke<Outer_Serialize>(\"get\", { which, mode })),")
	count := strings.Index(out, "\tcount: () => invoke<number>(\"count\", {}),")
	clearAt := strings.Index(out, "\tclear: () => typedError<null, Problem_Serialize>(invoke<null>(\"clear\", {})),")
	assert.True(t, get >= 0 && count > get && clearAt > count, out)
	assert.Contains(t, out, "\t/** Get returns the thing. */\n")
	assert.Contains(t, out, "\tget: [\"github.com/BorrowedMirror/rust-king-ledger/internal/bindgen/fixture.Service.Get\", [\"which\", \"mode\"]],")
	assert.Contains(t, out, "\touterChanged: makeEvent<Outer_Serialize>(\"outer-changed\"),")
}

func TestSourceIsWhatADefaultBuildCompiles(t *testing.T) {
	// A binary built with extra tags (the test hook) has more methods; reading only the
	// default build's files makes generating from it fail instead of adding commands.
	g := &gen{spec: spec(t), docs: map[string]*pkgDocs{}}
	pd, err := g.pkg(reflect.TypeFor[fixture.Service]().PkgPath())
	require.NoError(t, err)
	var names []string
	for _, m := range pd.methods["Service"] {
		names = append(names, m.name)
	}
	assert.Equal(t, []string{"Get", "Count", "Clear"}, names)
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
	// ts:"type=..." replaces the field's type; a Literal type is used as written.
	assert.Contains(t, out, "\tshape: Shape,\n\tlevel: string,\n")
	assert.Contains(t, out, "export type Shape = { round: true };")
	assert.NotContains(t, out, "Hidden")
	assert.NotContains(t, out, "byHand")
}

func TestStringTypesAreTheUnionOfTheirConstants(t *testing.T) {
	out := generate(t)
	assert.Contains(t, out, "/** Mode is a string enum. */\nexport type Mode =\n\t| \"a\"\n\t/** The second mode. */\n\t| \"b\";\n")
}

func TestRenamedTypeKeepsItsDoc(t *testing.T) {
	out := generate(t)
	assert.Contains(t, out, "/** Problem is a command's error. */\nexport type Problem = {\n")
	assert.NotContains(t, out, "Fault")
}

func TestStringTypeWithoutConstantsIsAnError(t *testing.T) {
	s := spec(t)
	// Outside the module there is no source to read constants from.
	s.Events = append(s.Events, Event{Name: "page", Wire: "page", Payload: reflect.TypeFor[template.HTML]()})
	_, err := Generate(s)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "template.HTML: a string type needs constants")
}

func TestMethodWithoutSourceIsAnError(t *testing.T) {
	// Without module paths the generator cannot read parameter names; it must say so rather
	// than emit no commands.
	_, err := Generate(Spec{Service: reflect.TypeFor[fixture.Service]()})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no source found")
}

func TestFallibleCommandNeedsTheErrorType(t *testing.T) {
	s := spec(t)
	s.Error = nil
	_, err := Generate(s)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Spec.Error is not set")
}

func TestTypeNameClashIsAnError(t *testing.T) {
	s := spec(t)
	s.Names = map[reflect.Type]string{reflect.TypeFor[fixture.Fault](): "Inner"}
	_, err := Generate(s)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "two Go types are both called Inner")
}

func TestCommandName(t *testing.T) {
	for in, want := range map[string]string{
		"GetSettings": "get_settings", "OpenSettingsTab": "open_settings_tab",
		"RecheckPermissions": "recheck_permissions", "CopyText": "copy_text", "Get": "get",
		"GetURL": "get_url",
	} {
		assert.Equal(t, want, commandName(in), in)
	}
}
