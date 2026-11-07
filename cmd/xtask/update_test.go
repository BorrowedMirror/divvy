package main

import (
	"bytes"
	"context"
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

	"github.com/wailsapp/wails/v3/pkg/updater"
	"github.com/wailsapp/wails/v3/pkg/updater/providers/endpoint"
)

func writeArtifacts(t *testing.T, dir, version string) {
	t.Helper()
	for _, a := range updateArtifacts {
		name := binaryName + "-" + version + "-" + a.suffix
		if err := os.WriteFile(filepath.Join(dir, name), []byte("payload for "+a.platform), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// The manifest must be what Wails' endpoint provider reads: it picks the running platform's
// artifact and turns its digest and signature into the Verification the updater checks.
func TestManifestIsReadByWailsEndpointProvider(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeArtifacts(t, dir, "9.9.9")
	m, err := buildManifest(dir, "9.9.9", "https://example.invalid/dl", "notes", priv, time.Unix(0, 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Artifacts) != 4 {
		t.Fatalf("artifacts = %d, want 4 (windows, linux, darwin arm64 and amd64)", len(m.Artifacts))
	}
	body, _ := json.Marshal(m)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(body) }))
	defer srv.Close()

	p, err := endpoint.New(endpoint.Config{URL: srv.URL + "/update.json"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []struct{ platform, arch string }{{"windows", "amd64"}, {"linux", "amd64"}, {"darwin", "arm64"}} {
		rel, err := p.Check(context.Background(), updater.CheckRequest{CurrentVersion: "0.1.0", Platform: want.platform, Arch: want.arch})
		if err != nil || rel == nil {
			t.Fatalf("%s/%s: rel=%v err=%v", want.platform, want.arch, rel, err)
		}
		if rel.Version != "9.9.9" || !strings.HasPrefix(rel.Artifact.Filename, "rkl-9.9.9-") || rel.Artifact.Platform != want.platform {
			t.Errorf("%s: release %+v", want.platform, rel)
		}
		v := rel.Verification
		if v == nil || v.DigestAlgo != "sha256" || len(v.Digest) != 32 || v.SignatureAlgo != "ed25519" {
			t.Fatalf("%s: verification %+v", want.platform, v)
		}
		if !ed25519.Verify(pub, v.Digest, v.Signature) {
			t.Errorf("%s: signature does not verify over the digest", want.platform)
		}
	}
}

func TestManifestNeedsEveryArtifact(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	dir := t.TempDir()
	writeArtifacts(t, dir, "1.0.0")
	if err := os.Remove(filepath.Join(dir, "rkl-1.0.0-linux-x86_64.AppImage")); err != nil {
		t.Fatal(err)
	}
	if _, err := buildManifest(dir, "1.0.0", "https://x/", "", priv, time.Now()); err == nil {
		t.Fatal("a release missing an OS's artifact must not get a manifest")
	}
}

func TestKeygenOutputRoundTrips(t *testing.T) {
	var out bytes.Buffer
	if err := runUpdaterKeygen(&out); err != nil {
		t.Fatal(err)
	}
	vals := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		k, v, _ := strings.Cut(line, "=")
		vals[k] = v
	}
	priv, err := privateKey(vals[updaterPrivateKeyEnv])
	if err != nil {
		t.Fatal(err)
	}
	pub, _ := base64.StdEncoding.DecodeString(vals[updaterKeyEnv])
	if !bytes.Equal(pub, priv.Public().(ed25519.PublicKey)) {
		t.Fatal("public key does not match the private key")
	}
	if _, err := privateKey(""); err == nil {
		t.Fatal("empty key must fail")
	}
}
