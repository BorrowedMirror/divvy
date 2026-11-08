package main

import (
	"bytes"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"text/template"

	"github.com/BorrowedMirror/rust-king-ledger/internal/product"
)

//go:embed assets
var assets embed.FS

// Product metadata shared by the installers.
const (
	productName  = product.Name
	binaryName   = product.Binary
	identifier   = product.ID
	publisher    = "RKL contributors"
	description  = product.Description
	copyrightMsg = "Copyright (c) 2026 RKL contributors. Apache-2.0."
)

// outDir is where packaging writes, relative to the module root (gitignored via target/).
const outDir = "target/go"

// macOSMin is the oldest macOS the bundle declares (LSMinimumSystemVersion and the cgo target).
const macOSMin = "12.0"

// packager builds the release binary for one OS and wraps it in that OS's installer.
type packager struct {
	root    string
	version string
}

// meta is the data the templates in assets/ see.
type meta struct {
	ProductName, Binary, Identifier, Publisher, Description, Copyright string
	Version, NumericVersion, MacOSMin                                  string
}

func (p packager) meta() meta {
	return meta{
		ProductName:    productName,
		Binary:         binaryName,
		Identifier:     identifier,
		Publisher:      publisher,
		Description:    description,
		Copyright:      copyrightMsg,
		Version:        p.version,
		NumericVersion: numericVersion(p.version),
		MacOSMin:       macOSMin,
	}
}

// render executes assets/<name> and writes it to dst.
func (p packager) render(name, dst string) error {
	t, err := template.New(name).Option("missingkey=error").ParseFS(assets, "assets/"+name)
	if err != nil {
		return err
	}
	var b bytes.Buffer
	if err := t.Execute(&b, p.meta()); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, b.Bytes(), 0o644)
}

// out is a path under target/go/.
func (p packager) out(elem ...string) string {
	return filepath.Join(append([]string{p.root, outDir}, elem...)...)
}

// fresh empties and recreates a work directory under target/go/.
func (p packager) fresh(elem ...string) (string, error) {
	dir := p.out(elem...)
	if err := os.RemoveAll(dir); err != nil {
		return "", err
	}
	return dir, os.MkdirAll(dir, 0o755)
}

// artifact is the file name of a release asset, e.g. rkl-0.1.0-windows-x64-setup.exe.
func (p packager) artifact(suffix string) string {
	return fmt.Sprintf("%s-%s-%s", binaryName, p.version, suffix)
}

func (p packager) done(path string) error {
	st, err := os.Stat(path)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "\nwrote %s (%.1f MiB)\n", path, float64(st.Size())/(1<<20))
	return nil
}

// copyFile copies src to dst with the permission bits mode, creating dst's directory.
func copyFile(src, dst string, mode os.FileMode) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, b, mode)
}
