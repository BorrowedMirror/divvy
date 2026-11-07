package core

// Readers for the Rust side's insta snapshots (`internal/core/testdata/snapshots/*/*.snap`) and
// the shared item corpus (`internal/testkit/testdata/corpus`), so Go is checked against the exact Rust
// output. The .snap files are only ever read.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
	"github.com/stretchr/testify/require"
)

const (
	corpusDir    = "../../internal/testkit/testdata/corpus"
	snapshotsDir = "../../internal/core/testdata/snapshots"
)

type corpusEntry struct {
	ID       string `toml:"id"`
	Class    string `toml:"class"`
	Rarity   string `toml:"rarity"`
	Advanced bool   `toml:"advanced"`
	Source   string `toml:"source"`
}

type corpusItem struct {
	entry corpusEntry
	text  string
}

// loadCorpus returns every corpus entry with its text, in index order.
func loadCorpus(t testing.TB) []corpusItem {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(corpusDir, "index.toml"))
	require.NoError(t, err)
	var index struct {
		Item []corpusEntry `toml:"item"`
	}
	require.NoError(t, toml.Unmarshal(raw, &index))
	out := make([]corpusItem, 0, len(index.Item))
	for _, e := range index.Item {
		text, err := os.ReadFile(filepath.Join(corpusDir, e.ID+".txt"))
		require.NoError(t, err)
		out = append(out, corpusItem{e, string(text)})
	}
	return out
}

func snapshotName(id string) string { return strings.ReplaceAll(id, "/", "__") }

// readSnapshot returns the body of an insta snapshot (after the `---` front matter).
func readSnapshot(t testing.TB, dir, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(snapshotsDir, dir, name+".snap"))
	require.NoError(t, err)
	text := strings.ReplaceAll(string(raw), "\r\n", "\n")
	require.True(t, strings.HasPrefix(text, "---\n"), "%s: no front matter", name)
	_, body, ok := strings.Cut(text[4:], "\n---\n")
	require.True(t, ok, "%s: unterminated front matter", name)
	return body
}

// parseInstaYAML parses the YAML subset insta writes for serde values (block mappings and
// sequences, `~`, `[]`, `{}`, booleans, numbers, plain and double-quoted strings) into the
// same generic shape encoding/json produces (map[string]any, []any, float64, ...).
func parseInstaYAML(src string) (any, error) {
	var lines []yamlLine
	for l := range strings.SplitSeq(src, "\n") {
		if strings.TrimSpace(l) == "" {
			continue
		}
		trimmed := strings.TrimLeft(l, " ")
		lines = append(lines, yamlLine{indent: len(l) - len(trimmed), text: trimmed})
	}
	if len(lines) == 0 {
		return nil, nil
	}
	p := &yamlParser{lines: lines}
	v, err := p.node(lines[0].indent)
	if err != nil {
		return nil, err
	}
	if p.i != len(lines) {
		return nil, fmt.Errorf("yaml: trailing content at %q", lines[p.i].text)
	}
	return v, nil
}

type yamlLine struct {
	indent int
	text   string
}

type yamlParser struct {
	lines []yamlLine
	i     int
}

var yamlKey = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*):(?: (.*))?$`)

func (p *yamlParser) node(indent int) (any, error) {
	l := p.lines[p.i]
	if l.text == "-" || strings.HasPrefix(l.text, "- ") {
		return p.seq(indent)
	}
	if yamlKey.MatchString(l.text) {
		return p.mapping(indent)
	}
	p.i++
	return yamlScalar(l.text)
}

func (p *yamlParser) seq(indent int) (any, error) {
	out := []any{}
	for p.i < len(p.lines) {
		l := p.lines[p.i]
		if l.indent != indent || !(l.text == "-" || strings.HasPrefix(l.text, "- ")) {
			break
		}
		rest := strings.TrimPrefix(strings.TrimPrefix(l.text, "-"), " ")
		if yamlKey.MatchString(rest) {
			// `- key: value` opens a mapping whose further keys sit two columns in.
			p.lines[p.i] = yamlLine{indent: indent + 2, text: rest}
			v, err := p.mapping(indent + 2)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
			continue
		}
		p.i++
		v, err := yamlScalar(rest)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

func (p *yamlParser) mapping(indent int) (any, error) {
	out := map[string]any{}
	for p.i < len(p.lines) {
		l := p.lines[p.i]
		if l.indent != indent {
			break
		}
		m := yamlKey.FindStringSubmatch(l.text)
		if m == nil {
			break
		}
		p.i++
		if m[2] != "" {
			v, err := yamlScalar(m[2])
			if err != nil {
				return nil, err
			}
			out[m[1]] = v
			continue
		}
		if p.i >= len(p.lines) {
			return nil, fmt.Errorf("yaml: key %q has no value", m[1])
		}
		next := p.lines[p.i]
		if next.indent < indent || (next.indent == indent && !strings.HasPrefix(next.text, "- ")) {
			return nil, fmt.Errorf("yaml: key %q has no value", m[1])
		}
		v, err := p.node(next.indent)
		if err != nil {
			return nil, err
		}
		out[m[1]] = v
	}
	return out, nil
}

var yamlNumber = regexp.MustCompile(`^[-+]?(?:\.[0-9]+|[0-9]+(?:\.[0-9]*)?)(?:[eE][-+]?[0-9]+)?$`)

func yamlScalar(s string) (any, error) {
	switch s {
	case "~", "null":
		return nil, nil
	case "true":
		return true, nil
	case "false":
		return false, nil
	case "[]":
		return []any{}, nil
	case "{}":
		return map[string]any{}, nil
	}
	if strings.HasPrefix(s, `"`) {
		var out string
		if err := json.Unmarshal([]byte(s), &out); err != nil {
			u, err2 := strconv.Unquote(s)
			if err2 != nil {
				return nil, fmt.Errorf("yaml: bad quoted string %s: %w", s, err)
			}
			out = u
		}
		return out, nil
	}
	if strings.HasPrefix(s, "'") && strings.HasSuffix(s, "'") && len(s) >= 2 {
		return strings.ReplaceAll(s[1:len(s)-1], "''", "'"), nil
	}
	if yamlNumber.MatchString(s) {
		return strconv.ParseFloat(s, 64)
	}
	return s, nil
}

func TestInstaYAMLReader(t *testing.T) {
	v, err := parseInstaYAML("a: 1\nb:\n  - x: \"#\"\n    y: ~\n  - z: []\nc:\n  - 1.5\n  - plain: text here\nd: \"1.10\"\n")
	require.NoError(t, err)
	require.Equal(t, map[string]any{
		"a": 1.0,
		"b": []any{map[string]any{"x": "#", "y": nil}, map[string]any{"z": []any{}}},
		"c": []any{1.5, map[string]any{"plain": "text here"}},
		"d": "1.10",
	}, v)
}
