package deps

import (
	"os"
	"strings"
)

// Family identifies a distro family for install-command selection.
type Family string

const (
	Arch    Family = "arch"
	Fedora  Family = "fedora"
	Debian  Family = "debian" // Ubuntu/Debian
	Unknown Family = "unknown"
)

// Distro describes the detected Linux distribution.
type Distro struct {
	ID     string // raw os-release ID, e.g. "arch", "ubuntu"
	Pretty string // PRETTY_NAME
	Family Family
}

// osReleasePath is overridable in tests.
var osReleasePath = "/etc/os-release"

// DetectDistro reads /etc/os-release. Defaults to Arch (primary target) when
// the file is missing or ambiguous.
func DetectDistro() Distro {
	content, err := os.ReadFile(osReleasePath)
	if err != nil {
		return Distro{Family: Arch}
	}
	return ParseOSRelease(string(content))
}

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
	// Ambiguous → default to Arch (primary target).
	d.Family = Arch
	return d
}

// InstallCommands returns copy-pasteable install lines for the missing tool on
// this distro family.
func (d Distro) InstallCommands(tool string) []string {
	switch tool {
	case "opencode":
		switch d.Family {
		case Arch:
			return []string{
				"sudo pacman -S opencode",
				"# or, for the latest: paru -S opencode-bin",
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

// Label is a human-readable distro name for the prereq screen heading.
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
	default:
		return "Linux"
	}
}
