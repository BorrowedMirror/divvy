package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wailsapp/wails/v3/pkg/updater"
	"github.com/wailsapp/wails/v3/pkg/updater/providers/endpoint"
)

func writeArtifacts(t *testing.T, dir, version string) {
	t.Helper()
	for _, a := range updateArtifacts {
		name := binaryName + "-" + version + "-" + a.suffix
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("payload for "+a.platform), 0o644))
	}
}

// The manifest must be what Wails' endpoint provider reads: it picks the running platform's
// artifact and turns its digest and signature into the Verification the updater checks.
func TestManifestIsReadByWailsEndpointProvider(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	dir := t.TempDir()
	writeArtifacts(t, dir, "9.9.9")
	m, err := buildManifest(dir, "9.9.9", "https://example.invalid/dl", "notes", priv, time.Unix(0, 0))
	require.NoError(t, err)
	require.Len(t, m.Artifacts, 4, "windows, linux, darwin arm64 and amd64")
	body, err := json.Marshal(m)
	require.NoError(t, err)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(body) }))
	defer srv.Close()

	p, err := endpoint.New(endpoint.Config{URL: srv.URL + "/update.json"})
	require.NoError(t, err)
	for _, want := range []struct{ platform, arch string }{{"windows", "amd64"}, {"linux", "amd64"}, {"darwin", "arm64"}} {
		rel, err := p.Check(t.Context(), updater.CheckRequest{CurrentVersion: "0.1.0", Platform: want.platform, Arch: want.arch})
		require.NoError(t, err, want.platform)
		require.NotNil(t, rel, want.platform)
		assert.Equal(t, "9.9.9", rel.Version)
		assert.True(t, strings.HasPrefix(rel.Artifact.Filename, "divvy-9.9.9-"), rel.Artifact.Filename)
		assert.Equal(t, want.platform, rel.Artifact.Platform)
		v := rel.Verification
		require.NotNil(t, v, want.platform)
		assert.Equal(t, "sha256", v.DigestAlgo)
		assert.Len(t, v.Digest, 32)
		assert.Equal(t, "ed25519", v.SignatureAlgo)
		assert.True(t, ed25519.Verify(pub, v.Digest, v.Signature), "%s: signature does not verify over the digest", want.platform)
	}
}

func TestManifestNeedsEveryArtifact(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	dir := t.TempDir()
	writeArtifacts(t, dir, "1.0.0")
	require.NoError(t, os.Remove(filepath.Join(dir, "divvy-1.0.0-linux-x86_64.AppImage")))
	_, err = buildManifest(dir, "1.0.0", "https://x/", "", priv, time.Now())
	assert.Error(t, err, "a release missing an OS's artifact must not get a manifest")
}

func TestKeygenOutputRoundTrips(t *testing.T) {
	var out bytes.Buffer
	require.NoError(t, runUpdaterKeygen(&out))
	vals := map[string]string{}
	for line := range strings.Lines(strings.TrimSpace(out.String())) {
		k, v, _ := strings.Cut(strings.TrimSpace(line), "=")
		vals[k] = v
	}
	priv, err := privateKey(vals[updaterPrivateKeyEnv])
	require.NoError(t, err)
	pub, err := base64.StdEncoding.DecodeString(vals[updaterKeyEnv])
	require.NoError(t, err)
	assert.Equal(t, priv.Public(), ed25519.PublicKey(pub), "public key does not match the private key")
	_, err = privateKey("")
	assert.Error(t, err, "empty key must fail")
}
