//go:build darwin

package tui

import (
	"os/exec"
	"strings"
)

// launchdEnv returns a variable set via `launchctl setenv` — the value the Ollama
// menu-bar app inherits on launch — so the settings screen can show the
// daemon-relevant value rather than just this shell's. ok=false when it's unset
// or launchctl is unavailable.
func launchdEnv(key string) (string, bool) {
	out, err := exec.Command("launchctl", "getenv", key).Output()
	if err != nil {
		return "", false
	}
	if v := strings.TrimSpace(string(out)); v != "" {
		return v, true
	}
	return "", false
}
