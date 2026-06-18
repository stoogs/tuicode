//go:build linux

package deps

import "os"

// osReleasePath is overridable in tests.
var osReleasePath = "/etc/os-release"

// DetectDistro reads /etc/os-release. Defaults to Arch (primary Linux target)
// when the file is missing or ambiguous.
func DetectDistro() Distro {
	content, err := os.ReadFile(osReleasePath)
	if err != nil {
		return Distro{Family: Arch}
	}
	return ParseOSRelease(string(content))
}
