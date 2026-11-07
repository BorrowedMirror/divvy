//go:build e2e && unix

package e2e

import "syscall"

// signalTrigger sends the app SIGUSR2 (price check) or SIGUSR1 (pinned price check).
func signalTrigger(pid int, pinned bool) error {
	sig := syscall.SIGUSR2
	if pinned {
		sig = syscall.SIGUSR1
	}
	return syscall.Kill(pid, sig)
}
