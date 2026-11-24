package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	notesComment   = "<!-- Release notes generated using configuration in .github/release.yml at v0.2.0 -->\n\n"
	notesChangelog = "**Full Changelog**: https://github.com/o/r/compare/v0.1.0...v0.2.0"
)

func TestFoldNotes(t *testing.T) {
	testCases := []struct {
		name     string
		have     string
		expected string
	}{
		{
			"HighlightsAndOthers",
			notesComment + "## What's Changed\n" +
				"### Features\n* feat: a by @a in #1\n" +
				"### Tests\n* test: b by @a in #2\n" +
				"### Bug fixes\n* fix: c by @a in #3\n" +
				"### Dependencies\n* build(deps): d by @renovate[bot] in #4\n* build(deps): e by @renovate[bot] in #5\n" +
				"\n## New Contributors\n* @b made their first contribution in #3\n\n" + notesChangelog,
			notesComment + "## What's Changed\n" +
				"### Features\n* feat: a by @a in #1\n" +
				"### Bug fixes\n* fix: c by @a in #3\n" +
				"\n<details>\n<summary>Everything else (3 pull requests)</summary>\n\n" +
				"### Tests\n* test: b by @a in #2\n" +
				"### Dependencies\n* build(deps): d by @renovate[bot] in #4\n* build(deps): e by @renovate[bot] in #5\n" +
				"\n</details>\n" +
				"\n## New Contributors\n* @b made their first contribution in #3\n\n" + notesChangelog,
		},
		{
			"OnlyHighlights",
			notesComment + "## What's Changed\n### Bug fixes\n* fix: c by @a in #3\n\n\n" + notesChangelog,
			notesComment + "## What's Changed\n### Bug fixes\n* fix: c by @a in #3\n\n" + notesChangelog,
		},
		{
			"OnlyOthers",
			notesComment + "## What's Changed\n### Dependencies\n* build(deps): d by @renovate[bot] in #4\n\n\n" + notesChangelog,
			notesComment + "## What's Changed\n<details>\n<summary>Everything else (1 pull request)</summary>\n\n" +
				"### Dependencies\n* build(deps): d by @renovate[bot] in #4\n\n</details>\n\n" + notesChangelog,
		},
		{
			"NoPullRequests",
			notesComment + "\n\n" + notesChangelog,
			notesComment + "\n\n" + notesChangelog,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, foldNotes(tc.have))
		})
	}
}

func TestFillFooter(t *testing.T) {
	testCases := []struct {
		name     string
		have     string
		expected string
		err      string
	}{
		{
			"Placeholders",
			"get divvy-${VERSION} from ${REPOSITORY} at ${TAG}",
			"get divvy-1.2.3 from o/r at v1.2.3",
			"",
		},
		{
			"UnknownPlaceholder",
			"see ${RELEASE_URL} for more",
			"",
			releaseNotesFooter + ": unknown placeholder ${RELEASE_URL}",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual, err := fillFooter(tc.have, "1.2.3", "v1.2.3", "o/r")
			if tc.err != "" {
				assert.EqualError(t, err, tc.err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.expected, actual)
		})
	}
}

// The footer in the repository fills in and links to this release's assets.
func TestRunNotesAppendsTheFooter(t *testing.T) {
	root := filepath.Join("..", "..")
	var out bytes.Buffer
	in := strings.NewReader(notesComment + "## What's Changed\n### Features\n* feat: a by @a in #1\n\n" + notesChangelog + "\n")
	require.NoError(t, runNotes(root, []string{"-version", "0.2.0", "-tag", "v0.2.0", "-repository", "o/r"}, in, &out))
	assert.Contains(t, out.String(), "### Features\n* feat: a by @a in #1\n")
	assert.Contains(t, out.String(), "https://github.com/o/r/releases/download/v0.2.0/divvy-0.2.0-windows-x64-setup.exe")
	assert.NotContains(t, out.String(), "${")
}

// Every highlight is a category of .github/release.yml, so renaming one there can't quietly
// fold it away.
func TestHighlightCategoriesAreReleaseCategories(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", ".github", "release.yml"))
	require.NoError(t, err)
	for _, title := range highlightCategories {
		assert.Contains(t, string(b), "- title: "+title+"\n", title)
	}
}
