package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// runCoverage runs `go test -race` with coverage once, then splits the profile into one file per
// top-level internal package (target/go/coverage/<name>.out, subpackages included), which CI
// uploads to Codecov with the flag go-<name>.
func runCoverage(root string, args []string) error {
	fs := flag.NewFlagSet("coverage", flag.ContinueOnError)
	race := fs.Bool("race", true, "run the tests with the race detector")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := requireWebDist(root); err != nil {
		return err
	}
	dir := filepath.Join(root, outDir, "coverage")
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	all := filepath.Join(dir, "all.out")
	a := append([]string{"test"}, hostTags()...)
	if *race {
		a = append(a, "-race")
	}
	a = append(a, "-covermode=atomic", "-coverprofile="+all, "./...")
	if err := (sh{dir: root}).run("go", a...); err != nil {
		return err
	}

	f, err := os.Open(all)
	if err != nil {
		return err
	}
	defer f.Close()
	groups, err := splitProfile(bufio.NewScanner(f))
	if err != nil {
		return err
	}
	names := make([]string, 0, len(groups))
	for name := range groups {
		names = append(names, name)
	}
	sort.Strings(names)
	fmt.Fprintln(os.Stderr, "\nstatement coverage")
	for _, name := range names {
		g := groups[name]
		if err := os.WriteFile(filepath.Join(dir, name+".out"), []byte(g.profile()), 0o644); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "  %-10s %6.2f%%  (%d/%d)\n", name, g.percent(), g.covered, g.total)
	}
	fmt.Fprintf(os.Stderr, "profiles in %s\n", dir)
	return nil
}

type coverGroup struct {
	mode           string
	lines          []string
	covered, total int
}

func (g *coverGroup) profile() string {
	return "mode: " + g.mode + "\n" + strings.Join(g.lines, "\n") + "\n"
}

func (g *coverGroup) percent() float64 {
	if g.total == 0 {
		return 0
	}
	return float64(g.covered) * 100 / float64(g.total)
}

// coverGroupOf maps a profile file path to its group: the top-level directory under internal/
// (internal/platform/x11/foo.go -> platform). Files outside internal/ are not grouped.
func coverGroupOf(file string) (string, bool) {
	rest, ok := strings.CutPrefix(file, modulePath+"/internal/")
	if !ok {
		return "", false
	}
	name, _, ok := strings.Cut(rest, "/")
	return name, ok
}

// splitProfile reads a Go cover profile and groups its blocks by coverGroupOf.
func splitProfile(sc *bufio.Scanner) (map[string]*coverGroup, error) {
	groups := map[string]*coverGroup{}
	mode := ""
	for sc.Scan() {
		line := sc.Text()
		if m, ok := strings.CutPrefix(line, "mode: "); ok {
			mode = m
			continue
		}
		if line == "" {
			continue
		}
		// <file>:<start>,<end> <statements> <count>
		file, _, ok := strings.Cut(line, ":")
		fields := strings.Fields(line)
		if !ok || len(fields) != 3 {
			return nil, fmt.Errorf("bad cover profile line %q", line)
		}
		stmts, err1 := strconv.Atoi(fields[1])
		count, err2 := strconv.Atoi(fields[2])
		if err1 != nil || err2 != nil {
			return nil, fmt.Errorf("bad cover profile line %q", line)
		}
		name, ok := coverGroupOf(file)
		if !ok {
			continue
		}
		g := groups[name]
		if g == nil {
			g = &coverGroup{mode: mode}
			groups[name] = g
		}
		g.lines = append(g.lines, line)
		g.total += stmts
		if count > 0 {
			g.covered += stmts
		}
	}
	if mode == "" && len(groups) > 0 {
		return nil, fmt.Errorf("cover profile has no mode line")
	}
	return groups, sc.Err()
}
