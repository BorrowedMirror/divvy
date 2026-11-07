//go:build !unix

package main

import "github.com/BorrowedMirror/rust-king-ledger/internal/app"

// startSignalTriggers: no POSIX signals here; `rkl --trigger` works on every OS.
func startSignalTriggers(*app.Flow) {}
