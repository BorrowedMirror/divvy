package main

import (
	"bufio"
	"bytes"
	"cmp"
	"crypto/sha256"
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"html/template"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// allowedLicenses is the licence policy: everything here can ship inside an Apache-2.0 app.
// It is policy, not an inventory, so entries need not be in use today.
var allowedLicenses = []string{
	"0BSD", "Apache-2.0", "BSD-2-Clause", "BSD-3-Clause", "BSL-1.0", "CC0-1.0", "ISC", "MIT",
	"MIT-0", "MPL-2.0", "Unlicense", "Zlib",
}

// noticeTargets are the builds whose dependencies the notices cover: each OS links a few
// modules of its own (D-Bus and X11 on Linux, COM on Windows, purego on macOS).
var noticeTargets = []struct{ goos, tags string }{
	{"linux", "gtk3"},
	{"windows", ""},
	{"darwin", "purego"},
}

// licenseLineTemplate is the go-licenses report template: one tab-separated line per
// (module, licence).
const licenseLineTemplate = "{{range .}}{{.Name}}\t{{.Version}}\t{{.LicenseName}}\t{{.LicensePath}}\n{{end}}"

//go:embed assets/notices.html.tmpl
var noticesHTML string

// dependency is one third-party package and the licence file go-licenses found for it.
type dependency struct {
	Name, Version, LicensePath string
	Licenses                   []string
}

// noticeGroup is one licence text and the packages it covers.
type noticeGroup struct {
	ID       string
	Licenses string
	Text     string
	UsedBy   []dependency
}

// runNotices writes THIRD-PARTY-NOTICES.html for the release (or only checks the licence
// policy with -check), from `go-licenses report` over ./cmd/rkl for every target OS.
func runNotices(root string, args []string) error {
	fs := flag.NewFlagSet("notices", flag.ContinueOnError)
	out := fs.String("o", filepath.Join(root, "target", "go", "THIRD-PARTY-NOTICES.html"), "output file")
	check := fs.Bool("check", false, "only check every licence against the policy")
	if err := fs.Parse(args); err != nil {
		return err
	}
	tool, err := goLicenses(root)
	if err != nil {
		return err
	}
	tmpl := filepath.Join(os.TempDir(), fmt.Sprintf("rkl-licenses-%d.tmpl", os.Getpid()))
	if err := os.WriteFile(tmpl, []byte(licenseLineTemplate), 0o600); err != nil {
		return err
	}
	defer os.Remove(tmpl)

	var reports []string
	for _, t := range noticeTargets {
		env := []string{"GOOS=" + t.goos, "GOARCH=amd64", "GOFLAGS=-tags=" + t.tags}
		if t.goos != "linux" {
			env = append(env, "CGO_ENABLED=0")
		}
		report, err := sh{dir: root}.with(env...).output(tool, "report", "./cmd/rkl",
			"--template", tmpl, "--ignore", modulePath)
		if err != nil {
			return fmt.Errorf("go-licenses (%s): %w", t.goos, err)
		}
		reports = append(reports, report)
	}
	deps, err := parseLicenseReports(reports...)
	if err != nil {
		return err
	}
	if err := checkLicensePolicy(deps); err != nil {
		return err
	}
	if *check {
		fmt.Fprintf(os.Stderr, "%d third-party packages, all licences allowed\n", len(deps))
		return nil
	}
	html, err := renderNotices(deps, os.ReadFile)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(*out), 0o777); err != nil {
		return err
	}
	if err := os.WriteFile(*out, html, 0o666); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "wrote", *out)
	return nil
}

// goLicenses is the host binary of the go-licenses pinned in tools/go.mod (`go tool -n`
// prints its path), so GOOS in its environment picks what it analyses, not what it runs on.
func goLicenses(root string) (string, error) {
	out, err := sh{dir: root}.with("GOOS=", "GOARCH=", "CGO_ENABLED=").output("go", "tool",
		"-modfile="+filepath.Join(root, "tools", "go.mod"), "-n", "go-licenses")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// parseLicenseReports merges go-licenses reports (licenseLineTemplate lines) into one sorted
// list; a package found in several reports, or with several licences, appears once.
func parseLicenseReports(reports ...string) ([]dependency, error) {
	byName := map[string]*dependency{}
	for _, report := range reports {
		sc := bufio.NewScanner(strings.NewReader(report))
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" {
				continue
			}
			f := strings.Split(line, "\t")
			if len(f) != 4 {
				return nil, fmt.Errorf("go-licenses: unexpected line %q", line)
			}
			d, ok := byName[f[0]]
			if !ok {
				d = &dependency{Name: f[0], Version: f[1], LicensePath: f[3]}
				byName[f[0]] = d
			}
			if !slices.Contains(d.Licenses, f[2]) {
				d.Licenses = append(d.Licenses, f[2])
				slices.Sort(d.Licenses)
			}
		}
		if err := sc.Err(); err != nil {
			return nil, err
		}
	}
	deps := make([]dependency, 0, len(byName))
	for _, name := range slices.Sorted(maps.Keys(byName)) {
		deps = append(deps, *byName[name])
	}
	return deps, nil
}

// checkLicensePolicy fails on any licence outside allowedLicenses, including the "Unknown"
// go-licenses reports when it finds no licence at all.
func checkLicensePolicy(deps []dependency) error {
	var bad []string
	for _, d := range deps {
		for _, l := range d.Licenses {
			if !slices.Contains(allowedLicenses, l) {
				bad = append(bad, fmt.Sprintf("  %s %s: %s", d.Name, d.Version, l))
			}
		}
	}
	if len(bad) > 0 {
		return errors.New("licences outside the policy (cmd/release/notices.go):\n" + strings.Join(bad, "\n"))
	}
	return nil
}

// renderNotices groups the packages by licence text and renders the notices page.
func renderNotices(deps []dependency, readFile func(string) ([]byte, error)) ([]byte, error) {
	groups := map[[32]byte]*noticeGroup{}
	for _, d := range deps {
		text, err := readFile(d.LicensePath)
		if err != nil {
			return nil, fmt.Errorf("%s: licence text: %w", d.Name, err)
		}
		text = bytes.TrimSpace(text)
		key := sha256.Sum256(text)
		g, ok := groups[key]
		if !ok {
			g = &noticeGroup{Licenses: strings.Join(d.Licenses, " AND "), Text: string(text)}
			groups[key] = g
		}
		g.UsedBy = append(g.UsedBy, d)
	}
	list := slices.Collect(maps.Values(groups))
	slices.SortFunc(list, func(a, b *noticeGroup) int {
		return cmp.Or(cmp.Compare(a.Licenses, b.Licenses), cmp.Compare(a.UsedBy[0].Name, b.UsedBy[0].Name))
	})
	overview := map[string]int{}
	for i, g := range list {
		g.ID = fmt.Sprintf("licence-%d", i+1)
		overview[g.Licenses] += len(g.UsedBy)
	}
	type count struct {
		Name  string
		Count int
	}
	var counts []count
	for _, name := range slices.Sorted(maps.Keys(overview)) {
		counts = append(counts, count{name, overview[name]})
	}
	t, err := template.New("notices").Parse(noticesHTML)
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	if err := t.Execute(&b, map[string]any{"Overview": counts, "Groups": list}); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}
