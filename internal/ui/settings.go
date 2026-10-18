package ui

import "github.com/unixsuperhero/tuios-deck/internal/config"

// SetSettings attaches the loaded settings and the file they save to.
func (m *Model) SetSettings(c *config.Config, path string) {
	m.settings, m.settingsPath = c, path
}
