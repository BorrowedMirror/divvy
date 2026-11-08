package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The Windows package is cross-compiled: the Go build needs no cgo on Windows, the resources
// (icon, manifest, version info) go into a .syso that Wails' CLI writes, and the system makensis
// builds the installer from assets/rkl.nsi.
//
// NSIS installers can be built for the amd64-unicode or x86-unicode target; both run on 64-bit
// Windows. Distribution packages of NSIS often ship only one of them (Arch: amd64, Debian and
// Ubuntu: x86), so this uses whichever the system copy has. When it has neither, it copies the
// system data directory to target/go/nsis/ and adds the x86-unicode stubs and plugins from the
// official NSIS zip (pinned by SHA-256).

// nsisTargets in order of preference.
var nsisTargets = []string{"amd64-unicode", "x86-unicode"}

// nsisZips are the official NSIS Windows zips this knows the SHA-256 of, by NSIS version. The
// zip must match the system makensis version; add a line when the system NSIS is upgraded.
var nsisZips = map[string]string{
	"3.13": "ba63dffc4410ee89193e1cb5a41989991bd77c61068da17e3156d136b7b0b3d8",
}

// The .syso Go links into cmd/rkl for windows/amd64 builds; it exists only during the build.
const sysoName = "rsrc_windows_amd64.syso"

func (p packager) windows(ctx context.Context) error {
	if err := requireTools(tool{"makensis", "nsis (apt: nsis; Arch: nsis from the AUR; Windows: winget install NSIS.NSIS)"}); err != nil {
		return err
	}
	if err := requireWebDist(p.root); err != nil {
		return err
	}
	work, err := p.fresh("windows")
	if err != nil {
		return err
	}
	s := sh{dir: p.root}

	// Resources: icon, manifest (DPI awareness, common controls) and version info.
	manifest, info, syso := filepath.Join(work, "rkl.exe.manifest"), filepath.Join(work, "info.json"), filepath.Join(work, sysoName)
	if err := p.render("rkl.exe.manifest.tmpl", manifest); err != nil {
		return err
	}
	if err := p.render("info.json.tmpl", info); err != nil {
		return err
	}
	icon := filepath.Join(p.root, "build/windows/icon.ico")
	if err := wails3(ctx, s, p.root, "generate", "syso", "-arch", "amd64", "-icon", icon, "-manifest", manifest, "-info", info, "-out", syso); err != nil {
		return err
	}

	exe := filepath.Join(work, binaryName+".exe")
	if err := p.buildWindowsExe(ctx, s, syso, exe); err != nil {
		return err
	}
	// The bare exe is the update artifact: the updater swaps it in place inside the install.
	if err := copyFile(exe, p.out(p.artifact("windows-x64.exe")), 0o755); err != nil {
		return err
	}

	// The evergreen WebView2 bootstrapper, embedded in Wails' CLI.
	if err := wails3(ctx, s, p.root, "generate", "webview2bootstrapper", "-dir", work); err != nil {
		return err
	}
	script := filepath.Join(work, "rkl.nsi")
	if err := copyFromAssets("rkl.nsi", script); err != nil {
		return err
	}
	makensis, env, target, err := p.nsis(ctx)
	if err != nil {
		return err
	}
	installer := p.out(p.artifact("windows-x64-setup.exe"))
	args := []string{
		"-V2", "-WX", "-X" + "Target " + target,
		"-DVERSION=" + p.version,
		"-DVERSION4=" + numericVersion(p.version),
		"-DBINARY=" + exe,
		"-DICON=" + icon,
		"-DLICENSE=" + filepath.Join(p.root, "LICENSE"),
		"-DWEBVIEW2=" + filepath.Join(work, "MicrosoftEdgeWebview2Setup.exe"),
		"-DOUTFILE=" + installer,
		script,
	}
	if err := s.with(env...).run(ctx, makensis, args...); err != nil {
		return err
	}
	return p.done(installer)
}

// buildWindowsExe links the .syso into cmd/rkl only for the duration of the build: Go picks up
// .syso files from the package directory alone (go build -overlay does not cover them).
func (p packager) buildWindowsExe(ctx context.Context, s sh, syso, exe string) error {
	linked := filepath.Join(p.root, "cmd/rkl", sysoName)
	if err := copyFile(syso, linked, 0o644); err != nil {
		return err
	}
	defer os.Remove(linked)
	return s.with("GOOS=windows", "GOARCH=amd64", "CGO_ENABLED=0").run(ctx, "go", "build", "-tags", "production",
		"-trimpath", "-ldflags", ldflags(p.version, "-H", "windowsgui"), "-o", exe, "./cmd/rkl")
}

// nsis picks the makensis to run, the environment for it and the NSIS target.
func (p packager) nsis(ctx context.Context) (makensis string, env []string, target string, err error) {
	s := sh{dir: p.root}
	hdr, _ := s.output(ctx, "makensis", "-HDRINFO") // exits non-zero by design
	dir := nsisDir(hdr)
	if dir == "" {
		// makensis on Windows finds its data next to itself; the installer packages there
		// carry the x86-unicode target.
		return "makensis", nil, "x86-unicode", nil
	}
	if t := pickNSISTarget(dir); t != "" {
		return "makensis", nil, t, nil
	}
	out, err := s.output(ctx, "makensis", "-VERSION")
	if err != nil {
		return "", nil, "", err
	}
	version, err := nsisVersion(out)
	if err != nil {
		return "", nil, "", err
	}
	data, err := p.nsisX86(ctx, dir, version)
	if err != nil {
		return "", nil, "", err
	}
	return "makensis", []string{"NSISDIR=" + data}, "x86-unicode", nil
}

// nsisX86 builds target/go/nsis/<version>/nsis: the system data directory plus the x86-unicode
// stubs and plugins from the official zip. A finished setup is reused.
func (p packager) nsisX86(ctx context.Context, systemDir, version string) (string, error) {
	work := p.out("nsis", version)
	data := filepath.Join(work, "nsis")
	if hasNSISTarget(data, "x86-unicode") {
		return data, nil
	}
	want, ok := nsisZips[version]
	if !ok {
		return "", fmt.Errorf("the system NSIS %s has no amd64-unicode or x86-unicode stubs and there is no pinned checksum for its zip; add one to nsisZips in cmd/release/windows.go", version)
	}
	if err := requireTools(tool{"unzip", "unzip"}, tool{"cp", "coreutils"}); err != nil {
		return "", err
	}
	if err := os.RemoveAll(work); err != nil {
		return "", err
	}
	if err := os.MkdirAll(work, 0o755); err != nil {
		return "", err
	}
	s := sh{dir: work}
	if err := s.run(ctx, "cp", "-rL", systemDir, data); err != nil {
		return "", err
	}
	url := fmt.Sprintf("https://downloads.sourceforge.net/project/nsis/NSIS%%203/%s/nsis-%s.zip", version, version)
	zip := filepath.Join(work, "nsis.zip")
	if err := download(ctx, url, want, zip); err != nil {
		return "", err
	}
	unpacked := filepath.Join(work, "zip")
	if err := s.run(ctx, "unzip", "-q", zip, "-d", unpacked); err != nil {
		return "", err
	}
	src := filepath.Join(unpacked, "nsis-"+version)
	stubs, err := os.ReadDir(filepath.Join(src, "Stubs"))
	if err != nil {
		return "", err
	}
	for _, e := range stubs {
		if strings.Contains(e.Name(), "-x86-") {
			if err := copyFile(filepath.Join(src, "Stubs", e.Name()), filepath.Join(data, "Stubs", e.Name()), 0o644); err != nil {
				return "", err
			}
		}
	}
	if err := s.run(ctx, "cp", "-r", filepath.Join(src, "Plugins/x86-unicode"), filepath.Join(data, "Plugins")); err != nil {
		return "", err
	}
	if err := os.RemoveAll(unpacked); err != nil {
		return "", err
	}
	if !hasNSISTarget(data, "x86-unicode") {
		return "", errors.New("the NSIS zip had no x86-unicode stubs")
	}
	return data, nil
}

// nsisDir is the data directory from `makensis -HDRINFO`'s NSISDIR= symbol.
func nsisDir(hdrinfo string) string {
	for f := range strings.FieldsFuncSeq(hdrinfo, func(r rune) bool { return r == ',' || r == '\n' }) {
		if d, ok := strings.CutPrefix(strings.TrimSpace(f), "NSISDIR="); ok {
			return d
		}
	}
	return ""
}

// nsisVersion turns `makensis -VERSION` output (v3.13) into 3.13.
func nsisVersion(out string) (string, error) {
	v := strings.TrimPrefix(strings.TrimSpace(out), "v")
	if v == "" || v[0] < '0' || v[0] > '9' {
		return "", fmt.Errorf("unexpected `makensis -VERSION` output: %q", out)
	}
	return v, nil
}

// hasNSISTarget reports whether an NSIS data directory has what rkl.nsi needs for a target:
// the solid LZMA stub and the nsExec plugin.
func hasNSISTarget(dir, target string) bool {
	if st, err := os.Stat(filepath.Join(dir, "Stubs", "lzma_solid-"+target)); err != nil || st.IsDir() {
		return false
	}
	_, err := os.Stat(filepath.Join(dir, "Plugins", target, "nsExec.dll"))
	return err == nil
}

// pickNSISTarget is the preferred target the data directory supports, or "".
func pickNSISTarget(dir string) string {
	for _, t := range nsisTargets {
		if hasNSISTarget(dir, t) {
			return t
		}
	}
	return ""
}

// download fetches url to dst, checking its SHA-256.
func download(ctx context.Context, url, wantSHA256, dst string) error {
	fmt.Fprintln(os.Stderr, "fetch", url)
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", url, resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(body)
	if got := hex.EncodeToString(sum[:]); got != wantSHA256 {
		return fmt.Errorf("%s has SHA-256 %s, expected %s", url, got, wantSHA256)
	}
	return os.WriteFile(dst, body, 0o644)
}

func copyFromAssets(name, dst string) error {
	b, err := fs.ReadFile(assets, "assets/"+name)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, b, 0o644)
}
