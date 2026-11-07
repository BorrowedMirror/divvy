package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	modulePath = "github.com/BorrowedMirror/rust-king-ledger"
	// versionVars are the variables the app version is linked into with -ldflags -X: the app's
	// own (diagnostics, updater) and the trade client's (User-Agent).
	appVersionVar   = modulePath + "/internal/app.Version"
	tradeVersionVar = modulePath + "/internal/trade.Version"
	// versionFile holds the app version until the Rust build is retired.
	versionFile = "apps/desktop/src-tauri/tauri.conf.json"
	// outDir is where packaging writes, relative to the module root (gitignored via target/).
	outDir = "target/go"
)

// Product metadata shared by the installers.
const (
	productName  = "Rust King Ledger"
	binaryName   = "rkl"
	identifier   = "app.rkl"
	publisher    = "RKL contributors"
	description  = "Path of Exile 2 price-check overlay"
	copyrightMsg = "Copyright (c) 2026 RKL contributors. Apache-2.0."
)

// moduleRoot is the directory holding go.mod.
func moduleRoot() (string, error) {
	out, err := exec.Command("go", "env", "GOMOD").Output()
	if err != nil {
		return "", fmt.Errorf("go env GOMOD: %w", err)
	}
	mod := strings.TrimSpace(string(out))
	if mod == "" || mod == os.DevNull {
		return "", errors.New("not inside the rkl Go module")
	}
	return filepath.Dir(mod), nil
}

// appVersion reads the version from versionFile.
func appVersion(root string) (string, error) {
	b, err := os.ReadFile(filepath.Join(root, versionFile))
	if err != nil {
		return "", err
	}
	var conf struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(b, &conf); err != nil {
		return "", fmt.Errorf("%s: %w", versionFile, err)
	}
	if conf.Version == "" {
		return "", fmt.Errorf("%s has no version", versionFile)
	}
	return conf.Version, validateVersion(conf.Version)
}

// validateVersion accepts MAJOR.MINOR.PATCH with an optional -prerelease of dot-separated
// [0-9A-Za-z-] identifiers (the same rule as `cargo xtask release`).
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

// updaterKeyVar receives the updater's public key; updaterKeyEnv holds it in the release
// workflow. Builds without it have the updater off.
const (
	updaterKeyVar = modulePath + "/internal/shell.UpdaterPublicKey"
	updaterKeyEnv = "RKL_UPDATER_PUBKEY"
)

// ldflags are the linker flags of a release build.
func ldflags(version string, extra ...string) string {
	flags := []string{"-s", "-w", "-X", appVersionVar + "=" + version, "-X", tradeVersionVar + "=" + version}
	if key := strings.TrimSpace(os.Getenv(updaterKeyEnv)); key != "" {
		flags = append(flags, "-X", updaterKeyVar+"="+key)
	}
	return strings.Join(append(flags, extra...), " ")
}

// sh runs a command with its output on the terminal.
type sh struct {
	dir string
	env []string // added to os.Environ()
}

func (s sh) run(name string, args ...string) error {
	fmt.Fprintf(os.Stderr, "$ %s %s\n", name, strings.Join(args, " "))
	c := exec.Command(name, args...)
	c.Dir = s.dir
	c.Env = append(os.Environ(), s.env...)
	c.Stdout = os.Stderr
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

func (s sh) output(name string, args ...string) (string, error) {
	c := exec.Command(name, args...)
	c.Dir = s.dir
	c.Env = append(os.Environ(), s.env...)
	c.Stderr = os.Stderr
	out, err := c.Output()
	if err != nil {
		return "", fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return string(out), nil
}

func (s sh) with(env ...string) sh {
	return sh{dir: s.dir, env: append(append([]string(nil), s.env...), env...)}
}

// requireTools fails with one message listing every missing tool and where it comes from.
func requireTools(tools [][2]string) error {
	var missing []string
	for _, t := range tools {
		if _, err := exec.LookPath(t[0]); err != nil {
			missing = append(missing, fmt.Sprintf("  %s: %s", t[0], t[1]))
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing tools:\n%s", strings.Join(missing, "\n"))
	}
	return nil
}

// requireWebDist fails early when the embedded frontend has not been built.
func requireWebDist(root string) error {
	if _, err := os.Stat(filepath.Join(root, "web/dist/index.html")); err != nil {
		return errors.New("web/dist is missing: run `pnpm --dir web build` first")
	}
	return nil
}

// wails3 runs the Wails CLI pinned in tools/go.mod (`go tool -modfile=tools/go.mod wails3`).
func wails3(s sh, root string, args ...string) error {
	tool := []string{"tool", "-modfile=" + filepath.Join(root, "tools", "go.mod"), "wails3"}
	return s.with("GOOS=", "GOARCH=", "CGO_ENABLED=").run("go", append(tool, args...)...)
}

func copyFile(src, dst string, mode os.FileMode) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, b, mode)
}
