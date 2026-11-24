package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/mod/semver"
)

const (
	// webPackageJSON carries the version too; bump stamps both.
	webPackageJSON = "web/package.json"
	// releaseBase is what a new release branch starts from.
	releaseBase = "origin/master"
)

// packageJSONVersion is package.json's top-level "version" field.
var packageJSONVersion = regexp.MustCompile(`(?m)^  "version": "([^"]*)"`)

// runBump commits "release: vX.Y.Z" with the next version, counted from origin/master's,
// stamped into VERSION and web/package.json. On a release/… branch it commits there; anywhere
// else it commits on a new release/vX.Y.Z branch from
// origin/master, without an upstream, so a release never carries a feature branch's commits.
func runBump(ctx context.Context, root string, args []string, stdout io.Writer) error {
	if len(args) != 1 || (args[0] != "patch" && args[0] != "minor" && args[0] != "major" && validateVersion(args[0]) != nil) {
		return errors.New("usage: go run ./cmd/release bump <patch|minor|major|X.Y.Z[-PRE]>, or make release patch|minor|major (make release BUMP=X.Y.Z[-PRE])")
	}
	s := sh{dir: root}
	branch, err := s.output(ctx, "git", "branch", "--show-current")
	if err != nil {
		return err
	}
	branch = strings.TrimSpace(branch)
	onRelease := strings.HasPrefix(branch, "release/")
	if onRelease {
		if s.command(ctx, "git", "diff", "--quiet", "HEAD", "--", versionFile, webPackageJSON).Run() != nil {
			return fmt.Errorf("%s or %s has uncommitted changes", versionFile, webPackageJSON)
		}
	} else if s.command(ctx, "git", "diff", "--quiet", "HEAD").Run() != nil {
		return errors.New("commit or stash your changes first; a new release branch would carry them")
	}
	if err := s.run(ctx, "git", "fetch", "-q", "origin", "master"); err != nil {
		return err
	}

	// The released version is master's, also on a release branch: running bump again there
	// changes its mind (patch, then minor) rather than bumping twice.
	current, err := s.output(ctx, "git", "show", releaseBase+":"+versionFile)
	if err != nil {
		return err
	}
	current = strings.TrimSpace(current)
	basePkg, err := s.output(ctx, "git", "show", releaseBase+":"+webPackageJSON)
	if err != nil {
		return err
	}
	if m := packageJSONVersion.FindStringSubmatch(basePkg); m == nil || m[1] != current {
		return fmt.Errorf("%s does not hold %s's version %s on %s", webPackageJSON, versionFile, current, releaseBase)
	}
	next, err := nextVersion(current, args[0])
	if err != nil {
		return err
	}

	// A release branch keeps its name when bumped again: its pull request is tied to it.
	if !onRelease {
		branch = "release/v" + next
		if err := s.run(ctx, "git", "switch", "-q", "--no-track", "-c", branch, releaseBase); err != nil {
			return err
		}
	}
	held, err := appVersion(root)
	if err != nil {
		return err
	}
	if held == next {
		return fmt.Errorf("%s already holds %s", branch, next)
	}
	if err := os.WriteFile(filepath.Join(root, versionFile), []byte(next+"\n"), 0o644); err != nil {
		return err
	}
	pkg, err := os.ReadFile(filepath.Join(root, webPackageJSON))
	if err != nil {
		return err
	}
	stamped, err := stampPackageJSON(string(pkg), next)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(root, webPackageJSON), []byte(stamped), 0o644); err != nil {
		return err
	}
	title := "release: v" + next
	body := fmt.Sprintf("Stamp %s into %s and %s, up from %s.", next, versionFile, webPackageJSON, current)
	if err := s.run(ctx, "git", "commit", "-q", "-m", title, "-m", body, "--", versionFile, webPackageJSON); err != nil {
		return err
	}
	if onRelease {
		_, err = fmt.Fprintf(stdout, "Committed %q (%s -> %s) on %s. Push it, and retitle its pull request %q:\n\n"+
			"  git push\n  gh pr edit --title %q --body %q\n", title, current, next, branch, title, title, body)
		return err
	}
	_, err = fmt.Fprintf(stdout, "Committed %q (%s -> %s). Push the branch and open a pull request with that title:\n\n"+
		"  git push -u origin %s\n  gh pr create --title %q --body %q\n", title, current, next, branch, title, body)
	return err
}

// nextVersion is current bumped by patch, minor or major, as npm's semver inc does: a
// pre-release is released as its own version when the bumped field and those below it are zero
// (0.2.0-rc.1 patch, minor or major as 0.2.0, 0.2.0, 1.0.0). An exact version is taken as given
// when it is newer.
func nextVersion(current, bump string) (string, error) {
	if err := validateVersion(current); err != nil {
		return "", err
	}
	core, pre, _ := strings.Cut(current, "-")
	parts := strings.Split(core, ".")
	n := make([]int, 3)
	for i, p := range parts {
		n[i], _ = strconv.Atoi(p) // validateVersion checked they are numbers
	}
	isPre := pre != ""

	var next string
	switch bump {
	case "major":
		if !isPre || n[1] != 0 || n[2] != 0 {
			n[0], n[1], n[2] = n[0]+1, 0, 0
		}
	case "minor":
		if !isPre || n[2] != 0 {
			n[1], n[2] = n[1]+1, 0
		}
	case "patch":
		if !isPre {
			n[2]++
		}
	default:
		if err := validateVersion(bump); err != nil {
			return "", fmt.Errorf("%q is not patch, minor, major or a version such as 1.2.3", bump)
		}
		next = bump
	}
	if next == "" {
		next = fmt.Sprintf("%d.%d.%d", n[0], n[1], n[2])
	}
	if semver.Compare("v"+next, "v"+current) <= 0 {
		return "", fmt.Errorf("%s is not newer than %s", next, current)
	}
	return next, nil
}

// stampPackageJSON sets package.json's top-level version, leaving the rest of the file as it is.
func stampPackageJSON(pkg, version string) (string, error) {
	if len(packageJSONVersion.FindAllStringIndex(pkg, -1)) != 1 {
		return "", fmt.Errorf("%s: expected one top-level \"version\" field", webPackageJSON)
	}
	return packageJSONVersion.ReplaceAllLiteralString(pkg, `  "version": "`+version+`"`), nil
}
