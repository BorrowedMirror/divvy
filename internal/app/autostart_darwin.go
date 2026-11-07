package app

import (
	"errors"
	"os"
)

func setAutostart(enabled bool, path string) error {
	home, ok := os.LookupEnv("HOME")
	if !ok {
		return errors.New("environment variable not found: HOME")
	}
	agent := MacLaunchAgentPath(home)
	if enabled {
		return writeAutostartFile(agent, LaunchAgent(path))
	}
	return removeAutostartFile(agent)
}
