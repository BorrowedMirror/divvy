//go:build linux && !cgo

package wayland

import "github.com/BorrowedMirror/divvy/internal/platform"

// vkConn is never opened without cgo.
type vkConn struct{}

func openVirtualKeyboard(string) (*vkConn, error) {
	return nil, platform.Backend("virtual keyboard: built without cgo")
}

func (*vkConn) key(uint16, bool) error {
	return platform.Backend("virtual keyboard: built without cgo")
}
func (*vkConn) sync() error { return nil }
func (*vkConn) close()      {}
