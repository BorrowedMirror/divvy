package main

import (
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
func (p packager) linux() error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("package linux runs on Linux (this is %s)", runtime.GOOS)
	}
	arch, ok := linuxArch[runtime.GOARCH]
	if !ok {
		return fmt.Errorf("package linux: unsupported architecture %s", runtime.GOARCH)
	}
	if err := requireTools([][2]string{
		{"ldd", "libc (glibc)"},
		{"file", "file (linuxdeploy needs it)"},
		{"readelf", "binutils"},
	}); err != nil {
		return err
	}
	if err := requireWebDist(p.root); err != nil {
		return err
	}
	w3, err := wails3(p.root)
	if err != nil {
		return err
	}
	work, err := p.fresh("linux")
	if err != nil {
		return err
	}
	s := sh{dir: p.root}

	bin := filepath.Join(work, binaryName)
	if err := s.with("CGO_ENABLED=1").run("go", "build", "-tags", "gtk3", "-trimpath",
		"-ldflags", ldflags(p.version), "-o", bin, "./cmd/rkl"); err != nil {
		return err
	}
	// The generator names the icon in the AppDir after the file, so it must match Icon= in the
	// desktop entry.
	icon := filepath.Join(work, binaryName+".png")
	if err := copyFile(filepath.Join(p.root, "apps/desktop/src-tauri/icons/128x128@2x.png"), icon, 0o644); err != nil {
		return err
	}
	desktop := filepath.Join(work, binaryName+".desktop")
	if err := p.render("rkl.desktop.tmpl", desktop); err != nil {
		return err
	}

	// Kept between runs so linuxdeploy is downloaded once.
	build := p.out("appimage")
	if err := os.MkdirAll(build, 0o755); err != nil {
		return err
	}
	if err := s.run(w3, "generate", "appimage", "-binary", bin, "-icon", icon, "-desktopfile", desktop,
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
