package autostart

import (
	"fmt"
	"os"
)

// set writes or removes `~/Library/LaunchAgents/app.divvy.plist`.
func set(enabled bool, path string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("no home directory for the launch agent: %w", err)
	}
	agent := launchAgentPath(home)
	if enabled {
		return writeFile(agent, launchAgent(path))
	}
	return removeFile(agent)
}
