package deps

import (
	"strings"
)

// Family identifies an OS/distro family for install-command selection.
type Family string

const (
	Arch    Family = "arch"
	Fedora  Family = "fedora"
	Debian  Family = "debian" // Ubuntu/Debian
	MacOS   Family = "macos"
	Unknown Family = "unknown"
)

// Distro describes the detected host OS / Linux distribution.
type Distro struct {
	ID     string // raw os-release ID, e.g. "arch", "ubuntu" (empty on macOS)
	Pretty string // PRETTY_NAME (or "macOS <version>")
	Family Family
}

// classifyDistro maps os-release fields to a Family. Shared so ParseOSRelease
// (used in tests on any platform) stays available everywhere; the file-reading
// DetectDistro lives in the per-OS files.
func classifyDistro(id, idLike, pretty string) Distro {
	id = strings.ToLower(strings.TrimSpace(id))
	idLike = strings.ToLower(strings.TrimSpace(idLike))
	d := Distro{ID: id, Pretty: pretty}

	tokens := append([]string{id}, strings.Fields(idLike)...)
	for _, tok := range tokens {
		switch tok {
		case "arch", "archlinux", "cachyos", "endeavouros", "manjaro", "artix":
			d.Family = Arch
			return d
		case "fedora", "rhel", "centos", "rocky", "almalinux":
			d.Family = Fedora
			return d
		case "ubuntu", "debian", "pop", "linuxmint", "elementary":
			d.Family = Debian
			return d
		}
	}
	// Ambiguous → default to Arch (primary Linux target).
	d.Family = Arch
	return d
}

// InstallCommands returns copy-pasteable install lines for the missing tool on
// this OS/distro family.
func (d Distro) InstallCommands(tool string) []string {
	switch tool {
	case "opencode":
		switch d.Family {
		case Arch:
			return []string{
				"sudo pacman -S opencode",
				"# or, for the latest: paru -S opencode-bin",
			}
		case MacOS:
			return []string{
				"brew install sst/tap/opencode",
				"# or: curl -fsSL https://opencode.ai/install | bash",
			}
		case Fedora, Debian:
			return []string{
				"curl -fsSL https://opencode.ai/install | bash",
				"# or: npm i -g opencode-ai",
			}
		}
	case "ollama":
		switch d.Family {
		case Arch:
			return []string{
				"sudo pacman -S ollama-cuda          # NVIDIA GPU",
				"# or ollama-rocm (AMD) / ollama (CPU)",
				"sudo systemctl enable --now ollama",
				"ollama pull llama3.2:1b             # small tool-capable starter",
			}
		case MacOS:
			return []string{
				"brew install ollama                 # or download Ollama.app",
				"brew services start ollama          # or: ollama serve",
				"ollama pull llama3.2:1b             # small tool-capable starter",
			}
		case Fedora, Debian:
			return []string{
				"curl -fsSL https://ollama.com/install.sh | sh",
				"ollama pull llama3.2:1b             # small tool-capable starter",
			}
		}
	}
	// Fallback (unknown family).
	switch tool {
	case "opencode":
		return []string{"curl -fsSL https://opencode.ai/install | bash"}
	case "ollama":
		return []string{"curl -fsSL https://ollama.com/install.sh | sh"}
	}
	return nil
}

// Label is a human-readable OS name for the prereq screen heading.
func (d Distro) Label() string {
	if d.Pretty != "" {
		return d.Pretty
	}
	switch d.Family {
	case Arch:
		return "Arch Linux"
	case Fedora:
		return "Fedora"
	case Debian:
		return "Ubuntu/Debian"
	case MacOS:
		return "macOS"
	default:
		return "Linux"
	}
}

// DaemonStartCmd is the platform-appropriate command to start the Ollama daemon.
func (d Distro) DaemonStartCmd() string {
	if d.Family == MacOS {
		return "ollama serve   (or: brew services start ollama)"
	}
	return "sudo systemctl start ollama"
}

// IsMac reports whether the host is macOS.
func (d Distro) IsMac() bool { return d.Family == MacOS }

// DaemonRestartCmd is how to restart the Ollama daemon so newly-set environment
// variables take effect.
func (d Distro) DaemonRestartCmd() string {
	if d.Family == MacOS {
		return "quit Ollama (menu-bar icon → Quit) & reopen   ·   or: brew services restart ollama"
	}
	return "sudo systemctl restart ollama"
}
