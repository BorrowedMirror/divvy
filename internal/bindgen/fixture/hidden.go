//go:build bindgen_hidden

package fixture

// Hidden exists only in builds with the bindgen_hidden tag, like the test hook's methods.
func (Service) Hidden() {}
