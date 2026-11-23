package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// sh runs commands in dir with env added to the process environment.
type sh struct {
	dir string
	env []string
}

func (s sh) command(ctx context.Context, name string, args ...string) *exec.Cmd {
	c := exec.CommandContext(ctx, name, args...)
	c.Dir = s.dir
	c.Env = append(os.Environ(), s.env...)
	return c
}

// run runs a command with its output on the terminal.
func (s sh) run(ctx context.Context, name string, args ...string) error {
	fmt.Fprintf(os.Stderr, "$ %s %s\n", name, strings.Join(args, " "))
	c := s.command(ctx, name, args...)
	c.Stdout = os.Stderr
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

// output runs a command and returns its standard output.
func (s sh) output(ctx context.Context, name string, args ...string) (string, error) {
	c := s.command(ctx, name, args...)
	c.Stderr = os.Stderr
	out, err := c.Output()
	if err != nil {
		return "", fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return string(out), nil
}

// with returns s with env added.
func (s sh) with(env ...string) sh {
	s.env = append(append([]string(nil), s.env...), env...)
	return s
}

// tool is a command a packaging step needs and where it comes from.
type tool struct{ name, source string }

// requireTools fails with one message listing every missing tool and where it comes from.
func requireTools(tools ...tool) error {
	var missing []string
	for _, t := range tools {
		if _, err := exec.LookPath(t.name); err != nil {
			missing = append(missing, fmt.Sprintf("  %s: %s", t.name, t.source))
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

// wails3 runs the Wails CLI pinned in tools/go.mod (`go tool -modfile=tools/go.mod wails3`), built
// for the host without cgo: its generators need no GUI, and a cgo build on Linux would need GTK4
// and WebKitGTK 6.0 headers.
func wails3(ctx context.Context, s sh, root string, args ...string) error {
	tool := []string{"tool", "-modfile=" + filepath.Join(root, "tools", "go.mod"), "wails3"}
	return s.with("GOOS=", "GOARCH=", "CGO_ENABLED=0").run(ctx, "go", append(tool, args...)...)
}
