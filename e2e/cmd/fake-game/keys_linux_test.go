//go:build linux

package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestKeysymName(t *testing.T) {
	assert.Equal(t, "KeyC", keysymName(0x63, 54))
	assert.Equal(t, "KeyC", keysymName(0x43, 54))
	assert.Equal(t, "Digit1", keysymName(0x31, 10))
	assert.Equal(t, "Enter", keysymName(0xff0d, 36))
	assert.Equal(t, "ControlLeft", keysymName(0xffe3, 37))
	assert.Equal(t, "Unidentified(Xkb(200))", keysymName(0x1008ff11, 200))
}
