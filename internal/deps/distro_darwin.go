//go:build darwin

package deps

import (
	"os/exec"
	"strings"
)

// DetectDistro reports macOS, tagging the product version (e.g. "macOS 14.5")
// when `sw_vers` is available.
func DetectDistro() Distro {
	d := Distro{Family: MacOS, Pretty: "macOS"}
	if out, err := exec.Command("sw_vers", "-productVersion").Output(); err == nil {
		if v := strings.TrimSpace(string(out)); v != "" {
			d.Pretty = "macOS " + v
		}
	}
	return d
}
