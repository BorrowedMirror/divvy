// Package product is RKL's identity: the names the app, the OS and the installers know it by.
package product

const (
	// Name is the display name.
	Name = "Rust King Ledger"
	// Description is the one-line summary shown by installers and the OS.
	Description = "Path of Exile 2 price-check overlay"
	// Binary is the executable's name, also the program name on Linux.
	Binary = "rkl"
	// ID is the reverse-DNS identifier: the macOS bundle identifier, the GTK application id
	// and the single-instance lock, so every build is recognised as the same app.
	ID = "app.rkl"
)
