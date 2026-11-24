package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// releaseNotesFooter is appended to every release's notes, with the version, tag and repository
// filled in.
const releaseNotesFooter = ".github/release-notes-footer.md"

// highlightCategories are the categories of .github/release.yml that players read for; the
// others (documentation, refactoring, tests, build and CI, dependencies, other changes) fold
// into one collapsed section under them.
var highlightCategories = []string{
	"Breaking changes",
	"Features",
	"Bug fixes",
	"Performance",
	"Translations",
	"Reverts",
}

// runNotes reads GitHub's generated release notes on stdin and writes them to stdout with the
// highlight categories first, the rest collapsed, and the footer after them.
func runNotes(root string, args []string, stdin io.Reader, stdout io.Writer) error {
	flags := flag.NewFlagSet("notes", flag.ContinueOnError)
	version := flags.String("version", "", "release version, without the leading v (required)")
	tag := flags.String("tag", "", "release tag (required)")
	repository := flags.String("repository", "", "owner/name of the repository (required)")
	flags.Usage = func() {
		fmt.Fprint(flags.Output(), "usage: go run ./cmd/release notes -version X.Y.Z -tag vX.Y.Z -repository owner/name < generated.md\n\n"+
			"Reads GitHub's generated release notes (the body of the generate-notes API) on stdin and writes\n"+
			"them with the highlight categories first, the rest collapsed, and "+releaseNotesFooter+" after.\n\n")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *version == "" || *tag == "" || *repository == "" {
		flags.Usage()
		return errors.New("notes: -version, -tag and -repository are required")
	}
	generated, err := io.ReadAll(stdin)
	if err != nil {
		return err
	}
	footer, err := os.ReadFile(filepath.Join(root, releaseNotesFooter))
	if err != nil {
		return err
	}
	filled, err := fillFooter(string(footer), *version, *tag, *repository)
	if err != nil {
		return err
	}
	// One blank line between the notes and the footer, whatever GitHub ends its body with.
	_, err = io.WriteString(stdout, strings.TrimRight(foldNotes(string(generated)), "\n")+"\n"+filled)
	return err
}

// foldNotes reorders GitHub's "## What's Changed" section: the highlight categories stay in
// place, and the rest move into a <details> block after them. Everything outside that section
// (the generated comment, New Contributors, the Full Changelog link) is kept as it is, and notes
// without the section (a release with no pull requests) are returned unchanged.
func foldNotes(notes string) string {
	lines := strings.Split(notes, "\n")
	start := slices.Index(lines, "## What's Changed")
	if start < 0 {
		return notes
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "## ") || strings.HasPrefix(lines[i], "**Full Changelog**") {
			end = i
			break
		}
	}

	var highlights, others []string
	pullRequests := 0
	section := &highlights
	for _, line := range lines[start+1 : end] {
		if title, ok := strings.CutPrefix(line, "### "); ok {
			section = &others
			if slices.Contains(highlightCategories, title) {
				section = &highlights
			}
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		if section == &others && strings.HasPrefix(line, "* ") {
			pullRequests++
		}
		*section = append(*section, line)
	}

	out := append([]string{}, lines[:start+1]...)
	out = append(out, highlights...)
	if len(others) > 0 {
		noun := "pull requests"
		if pullRequests == 1 {
			noun = "pull request"
		}
		if len(highlights) > 0 {
			out = append(out, "")
		}
		out = append(out, "<details>", fmt.Sprintf("<summary>Everything else (%d %s)</summary>", pullRequests, noun), "")
		out = append(out, others...)
		out = append(out, "", "</details>")
	}
	out = append(out, "")
	out = append(out, lines[end:]...)
	return strings.Join(out, "\n")
}

// fillFooter replaces ${VERSION}, ${TAG} and ${REPOSITORY} in the footer, and fails on any other
// placeholder, so a typo can't reach a release.
func fillFooter(footer, version, tag, repository string) (string, error) {
	filled := strings.NewReplacer("${VERSION}", version, "${TAG}", tag, "${REPOSITORY}", repository).Replace(footer)
	if i := strings.Index(filled, "${"); i >= 0 {
		rest, _, _ := strings.Cut(filled[i:], "}")
		return "", fmt.Errorf("%s: unknown placeholder %s}", releaseNotesFooter, rest)
	}
	return filled, nil
}
