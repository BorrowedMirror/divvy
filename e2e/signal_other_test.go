//go:build e2e && !unix

package e2e

import "errors"

// signalTrigger: signal triggers exist on unix only.
func signalTrigger(int, bool) error {
	return errors.New("DIVVY_E2E_TRIGGER=signal needs a unix OS")
}
