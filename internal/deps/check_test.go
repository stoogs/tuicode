package deps

import (
	"context"
	"errors"
	"testing"
)

func TestDetectToolPresent(t *testing.T) {
	defer restore()
	lookPath = func(name string) (string, error) {
		if name == "opencode" {
			return "/usr/bin/opencode", nil
		}
		return "", errors.New("not found")
	}
	runVersion = func(ctx context.Context, bin string, args ...string) string {
		return "opencode 1.17.3"
	}
	r := Check(context.Background(), nil)
	if !r.OpenCode.Found {
		t.Fatal("opencode should be found")
	}
	if r.OpenCode.Version != "1.17.3" {
		t.Errorf("version = %q", r.OpenCode.Version)
	}
	if r.Ollama.Found {
		t.Error("ollama should be missing")
	}
	if r.OK() {
		t.Error("OK() should be false when ollama missing")
	}
	if got := r.Missing(); len(got) != 1 || got[0] != "Ollama" {
		t.Errorf("missing = %v", got)
	}
}

func TestDetectAllPresent(t *testing.T) {
	defer restore()
	lookPath = func(name string) (string, error) { return "/usr/bin/" + name, nil }
	runVersion = func(ctx context.Context, bin string, args ...string) string {
		switch bin {
		case "opencode":
			return "1.17.3"
		case "ollama":
			return "ollama version is 0.5.4"
		}
		return ""
	}
	r := Check(context.Background(), nil)
	if !r.OK() {
		t.Fatal("OK() should be true")
	}
	if r.Ollama.Version != "0.5.4" {
		t.Errorf("ollama version = %q", r.Ollama.Version)
	}
	if !r.NvidiaSMI.Found {
		t.Error("nvidia-smi should be found")
	}
}

func restore() {
	lookPath = func(name string) (string, error) { return "", errors.New("stub") }
	runVersion = func(ctx context.Context, bin string, args ...string) string { return "" }
}

func TestParseOSReleaseArch(t *testing.T) {
	d := ParseOSRelease(`NAME="Arch Linux"
PRETTY_NAME="Arch Linux"
ID=arch
BUILD_ID=rolling`)
	if d.Family != Arch {
		t.Errorf("family = %q, want arch", d.Family)
	}
}

func TestParseOSReleaseCachyOS(t *testing.T) {
	d := ParseOSRelease(`NAME="CachyOS"
ID=cachyos
ID_LIKE=arch
PRETTY_NAME="CachyOS"`)
	if d.Family != Arch {
		t.Errorf("family = %q, want arch (via ID_LIKE)", d.Family)
	}
}

func TestParseOSReleaseFedora(t *testing.T) {
	d := ParseOSRelease(`ID=fedora
PRETTY_NAME="Fedora Linux 41"`)
	if d.Family != Fedora {
		t.Errorf("family = %q, want fedora", d.Family)
	}
}

func TestParseOSReleaseUbuntu(t *testing.T) {
	d := ParseOSRelease(`ID=ubuntu
ID_LIKE=debian
PRETTY_NAME="Ubuntu 24.04 LTS"`)
	if d.Family != Debian {
		t.Errorf("family = %q, want debian", d.Family)
	}
}

func TestParseOSReleaseAmbiguousDefaultsArch(t *testing.T) {
	d := ParseOSRelease(`ID=weirdos`)
	if d.Family != Arch {
		t.Errorf("family = %q, want arch (default)", d.Family)
	}
}

func TestInstallCommands(t *testing.T) {
	arch := Distro{Family: Arch}
	if cmds := arch.InstallCommands("ollama"); len(cmds) == 0 || cmds[0] != "sudo pacman -S ollama-cuda          # NVIDIA GPU" {
		t.Errorf("arch ollama cmds = %v", cmds)
	}
	deb := Distro{Family: Debian}
	if cmds := deb.InstallCommands("ollama"); len(cmds) == 0 || cmds[0] != "curl -fsSL https://ollama.com/install.sh | sh" {
		t.Errorf("debian ollama cmds = %v", cmds)
	}
}

func TestCleanVersion(t *testing.T) {
	cases := map[string]string{
		"opencode 1.17.3":         "1.17.3",
		"ollama version is 0.5.4": "0.5.4",
		"v2.0.1":                  "2.0.1",
		"weird":                   "weird",
	}
	for in, want := range cases {
		if got := cleanVersion(in); got != want {
			t.Errorf("cleanVersion(%q) = %q, want %q", in, got, want)
		}
	}
}
