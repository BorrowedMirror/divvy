package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// updaterPrivateKeyEnv holds the base64 ed25519 private key (seed or full key) in the release
// workflow's protected environment.
const updaterPrivateKeyEnv = "DIVVY_UPDATER_PRIVATE_KEY"

// updateArtifact is one platform's update in the manifest: the file the updater swaps in.
type updateArtifact struct {
	suffix   string   // artifact name suffix written by `package`
	platform string   // runtime.GOOS of the installs it is for
	arches   []string // runtime.GOARCH values it covers
}

// updateArtifacts are the files `package` writes for the updater: the bare Windows exe (not the
// installer), the AppImage (the updater replaces $APPIMAGE) and the zipped universal .app.
var updateArtifacts = []updateArtifact{
	{"windows-x64.exe", "windows", []string{"amd64"}},
	{"linux-x86_64.AppImage", "linux", []string{"amd64"}},
	{"macos-universal.app.zip", "darwin", []string{"arm64", "amd64"}},
}

// manifestArtifact and updateManifest are Wails' update manifest (the endpoint provider's
// schemaVersion 1). Wails' own types are unexported (wails/v3/pkg/updater/providers/endpoint),
// so they are mirrored here; TestManifestIsReadByWailsEndpointProvider checks the provider
// reads what this writes.
type manifestArtifact struct {
	URL           string `json:"url"`
	Filename      string `json:"filename"`
	Size          int64  `json:"size"`
	Platform      string `json:"platform"`
	Arch          string `json:"arch"`
	DigestAlgo    string `json:"digestAlgo"`
	Digest        string `json:"digest"`
	SignatureAlgo string `json:"signatureAlgo"`
	Signature     string `json:"signature"`
}

type updateManifest struct {
	SchemaVersion int                `json:"schemaVersion"`
	Version       string             `json:"version"`
	Notes         string             `json:"notes,omitempty"`
	PublishedAt   string             `json:"publishedAt"`
	Artifacts     []manifestArtifact `json:"artifacts"`
}

// runUpdaterKeygen prints a new updater key pair. The public key goes in the release
// environment as DIVVY_UPDATER_PUBKEY (it is linked into builds), the private key as
// DIVVY_UPDATER_PRIVATE_KEY. Installed builds trust only the key they were built with.
func runUpdaterKeygen(stdout io.Writer) error {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "%s=%s\n%s=%s\n",
		updaterKeyEnv, base64.StdEncoding.EncodeToString(pub),
		updaterPrivateKeyEnv, base64.StdEncoding.EncodeToString(priv.Seed()))
	return err
}

// runUpdateManifest signs the update artifacts in -dir and writes update.json there.
func runUpdateManifest(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("update-manifest", flag.ContinueOnError)
	version := flags.String("version", "", "release version (required)")
	dir := flags.String("dir", "", "directory holding the packaged artifacts (required)")
	baseURL := flags.String("base-url", "", "URL the artifacts are downloaded from, ending in / (required)")
	notes := flags.String("notes", "", "release notes shown in the update banner")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *version == "" || *dir == "" || *baseURL == "" {
		return errors.New("update-manifest: -version, -dir and -base-url are required")
	}
	key, err := privateKey(os.Getenv(updaterPrivateKeyEnv))
	if err != nil {
		return err
	}
	m, err := buildManifest(*dir, *version, *baseURL, *notes, key, time.Now())
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	out := filepath.Join(*dir, "update.json")
	if err := os.WriteFile(out, append(b, '\n'), 0o644); err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "wrote %s (%d artifacts)\n", out, len(m.Artifacts))
	return err
}

// privateKey decodes a base64 ed25519 seed (32 bytes) or full private key (64 bytes).
func privateKey(s string) (ed25519.PrivateKey, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, fmt.Errorf("%s is not set", updaterPrivateKeyEnv)
	}
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", updaterPrivateKeyEnv, err)
	}
	switch len(b) {
	case ed25519.SeedSize:
		return ed25519.NewKeyFromSeed(b), nil
	case ed25519.PrivateKeySize:
		return ed25519.PrivateKey(b), nil
	}
	return nil, fmt.Errorf("%s: %d bytes, want a 32-byte seed or 64-byte key", updaterPrivateKeyEnv, len(b))
}

// buildManifest hashes and signs every update artifact found in dir. Each signature is raw
// ed25519 over the SHA-256 digest, which is what Wails' updater verifies against the public
// key linked into the app. A missing artifact is an error: a release must update every OS.
func buildManifest(dir, version, baseURL, notes string, key ed25519.PrivateKey, now time.Time) (updateManifest, error) {
	m := updateManifest{SchemaVersion: 1, Version: version, Notes: notes, PublishedAt: now.UTC().Format(time.RFC3339)}
	if !strings.HasSuffix(baseURL, "/") {
		baseURL += "/"
	}
	for _, a := range updateArtifacts {
		name := fmt.Sprintf("%s-%s-%s", binaryName, version, a.suffix)
		f, err := os.Open(filepath.Join(dir, name))
		if err != nil {
			return updateManifest{}, fmt.Errorf("update artifact: %w", err)
		}
		h := sha256.New()
		size, err := io.Copy(h, f)
		f.Close()
		if err != nil {
			return updateManifest{}, err
		}
		digest := h.Sum(nil)
		sig := ed25519.Sign(key, digest)
		for _, arch := range a.arches {
			m.Artifacts = append(m.Artifacts, manifestArtifact{
				URL:           baseURL + name,
				Filename:      name,
				Size:          size,
				Platform:      a.platform,
				Arch:          arch,
				DigestAlgo:    "sha256",
				Digest:        base64.StdEncoding.EncodeToString(digest),
				SignatureAlgo: "ed25519",
				Signature:     base64.StdEncoding.EncodeToString(sig),
			})
		}
	}
	return m, nil
}
