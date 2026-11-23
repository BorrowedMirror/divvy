package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// linuxArch is the AppImage architecture name for a Go architecture.
var linuxArch = map[string]string{"amd64": "x86_64", "arm64": "aarch64"}

// linux builds the AppImage with Wails' generator (`wails3 generate appimage`): it copies the
// WebKitGTK helper processes into the AppDir and runs linuxdeploy with its GTK plugin, which it
// downloads into target/go/appimage/ on first use.
func (p packager) linux(ctx context.Context) error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("package linux runs on Linux (this is %s)", runtime.GOOS)
	}
	arch, ok := linuxArch[runtime.GOARCH]
	if !ok {
		return fmt.Errorf("package linux: unsupported architecture %s", runtime.GOARCH)
	}
	if err := requireTools(
		tool{"ldd", "libc (glibc)"},
		tool{"file", "file (linuxdeploy needs it)"},
		tool{"readelf", "binutils"},
	); err != nil {
		return err
	}
	if err := requireWebDist(p.root); err != nil {
		return err
	}
	work, err := p.fresh("linux")
	if err != nil {
		return err
	}
	s := sh{dir: p.root}

	bin := filepath.Join(work, binaryName)
	if err := s.with("CGO_ENABLED=1").run(ctx, "go", "build", "-tags", "gtk3,production", "-trimpath",
		"-ldflags", ldflags(p.version), "-o", bin, "./cmd/divvy"); err != nil {
		return err
	}
	// The generator names the icon in the AppDir after the file, so it must match Icon= in the
	// desktop entry.
	icon := filepath.Join(work, binaryName+".png")
	if err := copyFile(filepath.Join(p.root, "build/appicon.png"), icon, 0o644); err != nil {
		return err
	}
	desktop := filepath.Join(work, binaryName+".desktop")
	if err := p.render("divvy.desktop.tmpl", desktop); err != nil {
		return err
	}

	// Kept between runs so linuxdeploy is downloaded once.
	build := p.out("appimage")
	if err := os.MkdirAll(build, 0o755); err != nil {
		return err
	}
	if err := wails3(ctx, s, p.root, "generate", "appimage", "-binary", bin, "-icon", icon, "-desktopfile", desktop,
		"-outputdir", work, "-builddir", build); err != nil {
		return err
	}
	made := filepath.Join(work, fmt.Sprintf("%s-%s.AppImage", binaryName, arch))
	final := p.out(p.artifact(fmt.Sprintf("linux-%s.AppImage", arch)))
	if err := os.Rename(made, final); err != nil {
		return err
	}
	return p.done(final)
}
