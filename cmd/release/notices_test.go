package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseLicenseReportsMergesTargets(t *testing.T) {
	linux := "example.com/b\tv1.0.0\tMIT\t/mod/b/LICENSE\n" +
		"example.com/multi\tv2.0.0\tMIT\t/mod/multi/LICENSE\n" +
		"example.com/multi\tv2.0.0\tApache-2.0\t/mod/multi/LICENSE\n"
	windows := "example.com/b\tv1.0.0\tMIT\t/mod/b/LICENSE\n\nexample.com/a\tv0.1.0\tBSD-3-Clause\t/mod/a/LICENSE\n"
	deps, err := parseLicenseReports(linux, windows)
	require.NoError(t, err)
	assert.Equal(t, []dependency{
		{Name: "example.com/a", Version: "v0.1.0", LicensePath: "/mod/a/LICENSE", Licenses: []string{"BSD-3-Clause"}},
		{Name: "example.com/b", Version: "v1.0.0", LicensePath: "/mod/b/LICENSE", Licenses: []string{"MIT"}},
		{Name: "example.com/multi", Version: "v2.0.0", LicensePath: "/mod/multi/LICENSE", Licenses: []string{"Apache-2.0", "MIT"}},
	}, deps)

	_, err = parseLicenseReports("garbage line\n")
	assert.ErrorContains(t, err, "unexpected line")
}

func TestLicensePolicy(t *testing.T) {
	require.NoError(t, checkLicensePolicy([]dependency{{Name: "a", Licenses: []string{"MIT", "Apache-2.0"}}}))
	err := checkLicensePolicy([]dependency{
		{Name: "gpl", Version: "v1", Licenses: []string{"GPL-3.0"}},
		{Name: "none", Version: "v2", Licenses: []string{"Unknown"}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "gpl v1: GPL-3.0")
	assert.Contains(t, err.Error(), "none v2: Unknown")
}

func TestRenderNoticesGroupsByLicenceText(t *testing.T) {
	files := map[string]string{
		"/mit-1": "MIT text <copyright holder>\n",
		"/mit-2": "  MIT text <copyright holder>",
		"/bsd":   "BSD text",
	}
	read := func(p string) ([]byte, error) {
		if s, ok := files[p]; ok {
			return []byte(s), nil
		}
		return nil, errors.New("missing")
	}
	html, err := renderNotices([]dependency{
		{Name: "example.com/x", Version: "v1", LicensePath: "/mit-1", Licenses: []string{"MIT"}},
		{Name: "example.com/y", Version: "v2", LicensePath: "/mit-2", Licenses: []string{"MIT"}},
		{Name: "example.com/z", Version: "v3", LicensePath: "/bsd", Licenses: []string{"BSD-3-Clause"}},
	}, read)
	require.NoError(t, err)
	page := string(html)
	assert.Equal(t, 1, strings.Count(page, "MIT text"), "identical texts share one section")
	assert.Contains(t, page, "&lt;copyright holder&gt;", "licence text is escaped")
	assert.Contains(t, page, "<li>MIT (2)</li>")
	assert.Contains(t, page, "<li>BSD-3-Clause (1)</li>")
	assert.Less(t, strings.Index(page, "BSD text"), strings.Index(page, "MIT text"), "sorted by licence")

	_, err = renderNotices([]dependency{{Name: "lost", LicensePath: "/nope"}}, read)
	assert.ErrorContains(t, err, "lost: licence text")
}
