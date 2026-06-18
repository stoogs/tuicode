//go:build linux

package tui

import "os/exec"

// openPath opens a file or directory in the desktop's default handler.
func openPath(path string) error {
	return exec.Command("xdg-open", path).Start()
}
