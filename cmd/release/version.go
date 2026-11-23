package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	modulePath = "github.com/BorrowedMirror/divvy"
	// appVersionVar is the variable the app version is linked into with -ldflags -X
	// (diagnostics, updater, the trade User-Agent).
	appVersionVar = modulePath + "/internal/app.Version"
	// versionFile holds the app version (one line, MAJOR.MINOR.PATCH[-PRE]).
	versionFile = "VERSION"
	// versionEnv overrides versionFile for a package build (the release workflow sets it; the
	// Task runner inside wails3 drops arguments after --, so tasks can't pass -version).
	versionEnv = "DIVVY_RELEASE_VERSION"
)

// updaterKeyVar receives the updater's public key; updaterKeyEnv holds it in the release
// workflow. Builds without it have the updater off.
const (
	updaterKeyVar = modulePath + "/internal/shell.UpdaterPublicKey"
	updaterKeyEnv = "DIVVY_UPDATER_PUBKEY"
)

// appVersion reads the version from versionFile.
func appVersion(root string) (string, error) {
	b, err := os.ReadFile(filepath.Join(root, versionFile))
	if err != nil {
		return "", err
	}
	v := strings.TrimSpace(string(b))
	if v == "" {
		return "", fmt.Errorf("%s is empty", versionFile)
	}
	return v, validateVersion(v)
}

// validateVersion accepts MAJOR.MINOR.PATCH with an optional -prerelease of dot-separated
// [0-9A-Za-z-] identifiers.
func validateVersion(v string) error {
	core, pre, hasPre := strings.Cut(v, "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return fmt.Errorf("version %q must be MAJOR.MINOR.PATCH[-PRE]", v)
	}
	for _, p := range parts {
		if p == "" || strings.Trim(p, "0123456789") != "" {
			return fmt.Errorf("version %q: %q is not a number", v, p)
		}
		if len(p) > 1 && p[0] == '0' {
			return fmt.Errorf("version %q: leading zero in %q", v, p)
		}
	}
	if hasPre {
		for id := range strings.SplitSeq(pre, ".") {
			if id == "" || strings.Trim(id, "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ-") != "" {
				return fmt.Errorf("version %q: bad pre-release identifier %q", v, id)
			}
		}
	}
	return nil
}

// numericVersion is MAJOR.MINOR.PATCH.0, the form Windows version resources and NSIS need.
func numericVersion(v string) string {
	core, _, _ := strings.Cut(v, "-")
	return core + ".0"
}

// ldflags are the linker flags of a release build.
func ldflags(version string, extra ...string) string {
	flags := []string{"-s", "-w", "-X", appVersionVar + "=" + version}
	if key := strings.TrimSpace(os.Getenv(updaterKeyEnv)); key != "" {
		flags = append(flags, "-X", updaterKeyVar+"="+key)
	}
	return strings.Join(append(flags, extra...), " ")
}
