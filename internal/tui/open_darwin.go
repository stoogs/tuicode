//go:build darwin

package tui

import "os/exec"

// openPath opens a file or directory in the macOS default handler (Finder).
func openPath(path string) error {
	return exec.Command("open", path).Start()
}
