package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateVersion(t *testing.T) {
	for _, v := range []string{"0.1.0", "1.20.3", "0.1.0-rc.1", "2.0.0-beta-2.x"} {
		require.NoError(t, validateVersion(v), v)
	}
	for _, v := range []string{"", "1.2", "1.2.3.4", "v1.2.3", "01.2.3", "1.2.x", "1.2.3-", "1.2.3-rc..1", "1.2.3-rc_1"} {
		assert.Error(t, validateVersion(v), v)
	}
}

func TestNumericVersion(t *testing.T) {
	for in, want := range map[string]string{"0.1.0": "0.1.0.0", "0.1.0-rc.1": "0.1.0.0", "3.4.5-x-y": "3.4.5.0"} {
		assert.Equal(t, want, numericVersion(in), in)
	}
}

func TestLdflagsSetsTheVersion(t *testing.T) {
	t.Setenv(updaterKeyEnv, "")
	want := "-s -w -X " + appVersionVar + "=1.2.3 -H windowsgui"
	assert.Equal(t, want, ldflags("1.2.3", "-H", "windowsgui"))
	t.Setenv(updaterKeyEnv, "KEY")
	assert.Contains(t, ldflags("1.2.3"), "-X "+updaterKeyVar+"=KEY")
}

func TestAppVersionReadsTheVersionFile(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, versionFile), "0.3.0-rc.2\n")
	v, err := appVersion(root)
	require.NoError(t, err)
	assert.Equal(t, "0.3.0-rc.2", v)
	write(t, filepath.Join(root, versionFile), "0.3\n")
	_, err = appVersion(root)
	assert.Error(t, err, "appVersion accepted a bad version")
}

func TestNSISHeaderInfo(t *testing.T) {
	v, err := nsisVersion("v3.13\n")
	require.NoError(t, err)
	assert.Equal(t, "3.13", v)
	_, err = nsisVersion("oops")
	require.Error(t, err, "nsisVersion accepted garbage")
	hdr := "Defined symbols: NSIS_AMD64=1,NSIS_CPU=amd64,NSISDIR=/usr/share/nsis,PREFIX_CONF=/etc"
	assert.Equal(t, "/usr/share/nsis", nsisDir(hdr))
	assert.Empty(t, nsisDir("nothing here"))
}

func TestPickNSISTarget(t *testing.T) {
	dir := t.TempDir()
	assert.Empty(t, pickNSISTarget(dir))
	add := func(target string) {
		write(t, filepath.Join(dir, "Stubs", "lzma_solid-"+target), "stub")
		write(t, filepath.Join(dir, "Plugins", target, "nsExec.dll"), "dll")
	}
	add("x86-unicode")
	assert.Equal(t, "x86-unicode", pickNSISTarget(dir))
	add("amd64-unicode")
	assert.Equal(t, "amd64-unicode", pickNSISTarget(dir))
	// A stub without the plugin is not enough.
	write(t, filepath.Join(dir, "Stubs", "lzma_solid-x86-ansi"), "stub")
	assert.False(t, hasNSISTarget(dir, "x86-ansi"), "x86-ansi has no nsExec plugin")
}

func TestTemplatesRender(t *testing.T) {
	p := packager{root: t.TempDir(), version: "0.1.0-rc.1"}
	for _, name := range []string{"divvy.exe.manifest.tmpl", "info.json.tmpl", "divvy.desktop.tmpl", "Info.plist.tmpl"} {
		dst := filepath.Join(p.root, name)
		require.NoError(t, p.render(name, dst), name)
		b, err := os.ReadFile(dst)
		require.NoError(t, err)
		assert.NotContains(t, string(b), "{{", "%s has unrendered fields", name)
	}
	b, err := os.ReadFile(filepath.Join(p.root, "info.json.tmpl"))
	require.NoError(t, err)
	assert.Contains(t, string(b), `"file_version": "0.1.0.0"`)
	assert.Contains(t, string(b), `"ProductVersion": "0.1.0-rc.1"`)
}

func write(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}
