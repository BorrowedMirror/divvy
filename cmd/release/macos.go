package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// macos builds a universal (arm64 + x86_64) app bundle and a DMG. It needs macOS: the Wails
// darwin backend uses cgo, and the bundle tools (lipo, codesign, hdiutil) ship with Xcode.
//
// The bundle is signed ad hoc only. Developer ID signing and notarisation are not wired up yet.
func (p packager) macos(ctx context.Context) error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("package macos runs on macOS (this is %s)", runtime.GOOS)
	}
	if err := requireTools(
		tool{"lipo", "Xcode command line tools"},
		tool{"codesign", "Xcode command line tools"},
		tool{"hdiutil", "macOS"},
	); err != nil {
		return err
	}
	if err := requireWebDist(p.root); err != nil {
		return err
	}
	work, err := p.fresh("macos")
	if err != nil {
		return err
	}
	s := sh{dir: p.root}

	var parts []string
	for _, arch := range []string{"arm64", "amd64"} {
		out := filepath.Join(work, binaryName+"-"+arch)
		minFlag := "-mmacosx-version-min=" + macOSMin
		env := []string{
			"GOOS=darwin", "GOARCH=" + arch, "CGO_ENABLED=1",
			"CGO_CFLAGS=" + minFlag, "CGO_LDFLAGS=" + minFlag, "MACOSX_DEPLOYMENT_TARGET=" + macOSMin,
		}
		if err := s.with(env...).run(ctx, "go", "build", "-tags", "production", "-trimpath", "-ldflags", ldflags(p.version), "-o", out, "./cmd/divvy"); err != nil {
			return err
		}
		parts = append(parts, out)
	}

	app := filepath.Join(work, "dmg", productName+".app")
	exe := filepath.Join(app, "Contents/MacOS", binaryName)
	if err := os.MkdirAll(filepath.Dir(exe), 0o755); err != nil {
		return err
	}
	if err := s.run(ctx, "lipo", append([]string{"-create", "-output", exe}, parts...)...); err != nil {
		return err
	}
	if err := p.render("Info.plist.tmpl", filepath.Join(app, "Contents/Info.plist")); err != nil {
		return err
	}
	if err := copyFile(filepath.Join(p.root, "build/darwin/icons.icns"),
		filepath.Join(app, "Contents/Resources/icon.icns"), 0o644); err != nil {
		return err
	}
	if err := s.run(ctx, "codesign", "--force", "--deep", "--sign", "-", app); err != nil {
		return err
	}
	// The zipped bundle is the update artifact: the updater replaces the whole .app with it.
	if err := s.run(ctx, "ditto", "-c", "-k", "--keepParent", app, p.out(p.artifact("macos-universal.app.zip"))); err != nil {
		return err
	}
	if err := os.Symlink("/Applications", filepath.Join(work, "dmg", "Applications")); err != nil {
		return err
	}
	dmg := p.out(p.artifact("macos-universal.dmg"))
	if err := os.Remove(dmg); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := s.run(ctx, "hdiutil", "create", "-volname", productName, "-srcfolder", filepath.Join(work, "dmg"),
		"-ov", "-format", "UDZO", dmg); err != nil {
		return err
	}
	return p.done(dmg)
}
