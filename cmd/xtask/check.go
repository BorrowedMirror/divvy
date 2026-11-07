package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strings"
)

// hostTags are the build tags of a host build (Linux uses the GTK3 WebKit backend).
func hostTags() []string {
	if runtime.GOOS == "linux" {
		return []string{"-tags", "gtk3"}
	}
	return nil
}

// runCheck runs every check, reports each failure and fails if any did.
func runCheck(root string, args []string) error {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	race := fs.Bool("race", false, "run the tests with the race detector")
	noLint := fs.Bool("no-lint", false, "skip golangci-lint")
	noTest := fs.Bool("no-test", false, "skip go test (CI runs the tests in its coverage job)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := requireWebDist(root); err != nil {
		return err
	}
	s := sh{dir: root}
	tags := hostTags()

	type step struct {
		name string
		run  func() error
	}
	steps := []step{
		{"gofmt", func() error { return gofmtCheck(s) }},
		{"go vet (" + runtime.GOOS + ")", func() error { return s.run("go", append(append([]string{"vet"}, tags...), "./...")...) }},
		{"go vet (windows)", func() error {
			return s.with("GOOS=windows", "GOARCH=amd64", "CGO_ENABLED=0").run("go", "vet", "./...")
		}},
	}
	if runtime.GOOS != "darwin" {
		// Without a macOS SDK cgo cannot run, so this vets the CGO-free (purego) variant: files
		// that import "C" are skipped. The macOS CI job vets the real build.
		steps = append(steps, step{"go vet (darwin, purego)", func() error {
			return s.with("GOOS=darwin", "GOARCH=arm64", "CGO_ENABLED=0").run("go", "vet", "-tags", "purego", "./...")
		}})
	}
	if !*noTest {
		steps = append(steps, step{"go test", func() error {
			a := append([]string{"test"}, tags...)
			if *race {
				a = append(a, "-race")
			}
			return s.run("go", append(a, "./...")...)
		}})
	}
	steps = append(steps, step{"bindgen -check", func() error { return s.run("go", "run", "./cmd/bindgen", "-check") }})
	if !*noLint {
		lint := func(s sh, args ...string) error {
			if _, err := exec.LookPath("golangci-lint"); err != nil {
				return errors.New("golangci-lint is not on PATH (install it, or pass -no-lint)")
			}
			return s.run("golangci-lint", append([]string{"run"}, args...)...)
		}
		steps = append(steps,
			step{"golangci-lint (" + runtime.GOOS + ")", func() error { return lint(s) }},
			step{"golangci-lint (windows)", func() error { return lint(s.with("GOOS=windows", "GOARCH=amd64", "CGO_ENABLED=0")) }},
		)
		if runtime.GOOS != "darwin" {
			steps = append(steps, step{"golangci-lint (darwin, purego)", func() error {
				return lint(s.with("GOOS=darwin", "GOARCH=arm64", "CGO_ENABLED=0"), "--build-tags", "purego")
			}})
		}
	}

	var failed []string
	for _, st := range steps {
		fmt.Fprintf(os.Stderr, "\n== %s\n", st.name)
		if err := st.run(); err != nil {
			fmt.Fprintf(os.Stderr, "FAIL %s: %v\n", st.name, err)
			failed = append(failed, st.name)
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("failed: %s", strings.Join(failed, ", "))
	}
	fmt.Fprintln(os.Stderr, "\nall checks passed")
	return nil
}

// gofmtCheck fails when a file in the module's packages is not gofmt-clean.
func gofmtCheck(s sh) error {
	out, err := s.output("go", "list", "-f", "{{.Dir}}", "./...")
	if err != nil {
		return err
	}
	dirs := strings.Fields(out)
	if len(dirs) == 0 {
		return errors.New("go list found no packages")
	}
	bad, err := s.output("gofmt", append([]string{"-l"}, dirs...)...)
	if err != nil {
		return err
	}
	// gofmt -l recurses, so nested package directories list a file more than once.
	files := slices.Compact(slices.Sorted(slices.Values(strings.Fields(bad))))
	if len(files) > 0 {
		return fmt.Errorf("not gofmt-clean (run gofmt -w):\n%s", strings.Join(files, "\n"))
	}
	return nil
}
