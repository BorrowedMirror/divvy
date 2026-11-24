package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNextVersion(t *testing.T) {
	testCases := []struct {
		name     string
		current  string
		bump     string
		expected string
		err      string
	}{
		{"Patch", "0.1.0", "patch", "0.1.1", ""},
		{"Minor", "0.1.3", "minor", "0.2.0", ""},
		{"Major", "0.4.2", "major", "1.0.0", ""},
		{"PatchReleasesThePreRelease", "0.2.0-rc.1", "patch", "0.2.0", ""},
		{"MinorReleasesThePreRelease", "0.2.0-rc.1", "minor", "0.2.0", ""},
		{"MinorPastAPatchPreRelease", "0.2.1-rc.1", "minor", "0.3.0", ""},
		{"MajorReleasesThePreRelease", "1.0.0-rc.2", "major", "1.0.0", ""},
		{"MajorPastAMinorPreRelease", "0.2.0-rc.1", "major", "1.0.0", ""},
		{"Exact", "0.1.0", "0.2.0-rc.1", "0.2.0-rc.1", ""},
		{"ExactPreReleaseAfterAnother", "0.2.0-rc.1", "0.2.0-rc.2", "0.2.0-rc.2", ""},
		{"ExactNotNewer", "0.2.0", "0.2.0-rc.1", "", "0.2.0-rc.1 is not newer than 0.2.0"},
		{"ExactSame", "0.2.0", "0.2.0", "", "0.2.0 is not newer than 0.2.0"},
		{"UnknownBump", "0.2.0", "next", "", `"next" is not patch, minor, major or a version such as 1.2.3`},
		{"LeadingV", "0.2.0", "v0.3.0", "", `"v0.3.0" is not patch, minor, major or a version such as 1.2.3`},
		{"BadCurrent", "0.2", "patch", "", `version "0.2" must be MAJOR.MINOR.PATCH[-PRE]`},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual, err := nextVersion(tc.current, tc.bump)
			if tc.err != "" {
				assert.EqualError(t, err, tc.err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.expected, actual)
		})
	}
}

func TestStampPackageJSON(t *testing.T) {
	testCases := []struct {
		name     string
		have     string
		expected string
		err      string
	}{
		{
			"TopLevelOnly",
			"{\n  \"name\": \"web\",\n  \"version\": \"0.1.0\",\n  \"x\": {\n    \"version\": \"9.9.9\"\n  }\n}\n",
			"{\n  \"name\": \"web\",\n  \"version\": \"0.2.0\",\n  \"x\": {\n    \"version\": \"9.9.9\"\n  }\n}\n",
			"",
		},
		{
			"Missing",
			"{\n  \"name\": \"web\"\n}\n",
			"",
			webPackageJSON + `: expected one top-level "version" field`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual, err := stampPackageJSON(tc.have, "0.2.0")
			if tc.err != "" {
				assert.EqualError(t, err, tc.err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.expected, actual)
		})
	}
}
