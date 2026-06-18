//go:build linux

package tui

// launchdEnv has no analogue on Linux (systemd service env isn't readable from
// here), so the settings screen falls back to this shell's view.
func launchdEnv(key string) (string, bool) { return "", false }
