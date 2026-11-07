//go:build !unix

package main

import "github.com/BorrowedMirror/rust-king-ledger/internal/app"

// listenSignals: no POSIX signals here; `rkl --trigger` works on every OS.
func listenSignals() {}

// startSignalTriggers: see listenSignals.
func startSignalTriggers(*app.Flow) {}
