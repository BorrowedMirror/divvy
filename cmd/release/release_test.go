package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateVersion(t *testing.T) {
	for _, v := range []string{"0.1.0", "1.20.3", "0.1.0-rc.1", "2.0.0-beta-2.x"} {
		if err := validateVersion(v); err != nil {
			t.Errorf("validateVersion(%q) = %v, want nil", v, err)
		}
	}
	for _, v := range []string{"", "1.2", "1.2.3.4", "v1.2.3", "01.2.3", "1.2.x", "1.2.3-", "1.2.3-rc..1", "1.2.3-rc_1"} {
		if err := validateVersion(v); err == nil {
			t.Errorf("validateVersion(%q) = nil, want an error", v)
		}
	}
}

func TestNumericVersion(t *testing.T) {
	for in, want := range map[string]string{"0.1.0": "0.1.0.0", "0.1.0-rc.1": "0.1.0.0", "3.4.5-x-y": "3.4.5.0"} {
		if got := numericVersion(in); got != want {
			t.Errorf("numericVersion(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLdflagsSetsTheVersion(t *testing.T) {
	got := ldflags("1.2.3", "-H", "windowsgui")
	want := "-s -w -X " + modulePath + "/internal/app.Version=1.2.3 -X " + modulePath + "/internal/trade.Version=1.2.3 -H windowsgui"
	if got != want {
		t.Errorf("ldflags = %q, want %q", got, want)
	}
}

func TestAppVersionReadsTheTauriConfig(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, versionFile), `{"productName": "x", "version": "0.3.0-rc.2"}`)
	v, err := appVersion(root)
	if err != nil || v != "0.3.0-rc.2" {
		t.Fatalf("appVersion = %q, %v", v, err)
	}
	write(t, filepath.Join(root, versionFile), `{"version": "0.3"}`)
	if _, err := appVersion(root); err == nil {
		t.Fatal("appVersion accepted a bad version")
	}
}

func TestNSISHeaderInfo(t *testing.T) {
	v, err := nsisVersion("v3.13\n")
	if err != nil || v != "3.13" {
		t.Fatalf("nsisVersion = %q, %v", v, err)
	}
	if _, err := nsisVersion("oops"); err == nil {
		t.Fatal("nsisVersion accepted garbage")
	}
	hdr := "Defined symbols: NSIS_AMD64=1,NSIS_CPU=amd64,NSISDIR=/usr/share/nsis,PREFIX_CONF=/etc"
	if got := nsisDir(hdr); got != "/usr/share/nsis" {
		t.Errorf("nsisDir = %q", got)
	}
	if got := nsisDir("nothing here"); got != "" {
		t.Errorf("nsisDir = %q, want empty", got)
	}
}

func TestPickNSISTarget(t *testing.T) {
	dir := t.TempDir()
	if got := pickNSISTarget(dir); got != "" {
		t.Fatalf("empty dir: %q", got)
	}
	add := func(target string) {
		write(t, filepath.Join(dir, "Stubs", "lzma_solid-"+target), "stub")
		write(t, filepath.Join(dir, "Plugins", target, "nsExec.dll"), "dll")
	}
	add("x86-unicode")
	if got := pickNSISTarget(dir); got != "x86-unicode" {
		t.Fatalf("x86 only: %q", got)
	}
	add("amd64-unicode")
	if got := pickNSISTarget(dir); got != "amd64-unicode" {
		t.Fatalf("both: %q, want amd64-unicode", got)
	}
	// A stub without the plugin is not enough.
	write(t, filepath.Join(dir, "Stubs", "lzma_solid-x86-ansi"), "stub")
	if hasNSISTarget(dir, "x86-ansi") {
		t.Fatal("x86-ansi has no nsExec plugin")
	}
}

func TestTemplatesRender(t *testing.T) {
	p := packager{root: t.TempDir(), version: "0.1.0-rc.1"}
	for _, name := range []string{"rkl.exe.manifest.tmpl", "info.json.tmpl", "rkl.desktop.tmpl", "Info.plist.tmpl"} {
		dst := filepath.Join(p.root, name)
		if err := p.render(name, dst); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		b, err := os.ReadFile(dst)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "{{") {
			t.Errorf("%s has unrendered fields:\n%s", name, b)
		}
	}
	b, _ := os.ReadFile(filepath.Join(p.root, "info.json.tmpl"))
	if !strings.Contains(string(b), `"file_version": "0.1.0.0"`) || !strings.Contains(string(b), `"ProductVersion": "0.1.0-rc.1"`) {
		t.Errorf("info.json:\n%s", b)
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
