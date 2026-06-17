// Package deps detects external prerequisites (opencode, ollama, nvidia-smi)
// and the host Linux distribution for distro-aware install guidance.
package deps

import (
	"bufio"
	"context"
	"os/exec"
	"strings"

	"tuicode/internal/server"
)

// Tool is the detection result for a single external dependency.
type Tool struct {
	Name     string // "opencode", "ollama", "nvidia-smi"
	Found    bool
	Path     string
	Version  string // best-effort, may be empty
	Required bool
}

// Report is the full dependency-check result shown at startup.
type Report struct {
	OpenCode  Tool
	Ollama    Tool
	NvidiaSMI Tool
	Daemon    server.DaemonStatus // Ollama daemon reachability
	Distro    Distro
}

// OK reports whether all hard requirements are satisfied (tools present).
// Note: a down daemon is not a hard failure — the dashboard surfaces it.
func (r Report) OK() bool {
	return r.OpenCode.Found && r.Ollama.Found
}

// Missing returns the names of missing required tools.
func (r Report) Missing() []string {
	var m []string
	if !r.OpenCode.Found {
		m = append(m, "OpenCode")
	}
	if !r.Ollama.Found {
		m = append(m, "Ollama")
	}
	return m
}

// lookPath is overridable in tests.
var lookPath = exec.LookPath

// runVersion is overridable in tests; returns combined output of `bin args...`.
var runVersion = func(ctx context.Context, bin string, args ...string) string {
	cmd := exec.CommandContext(ctx, bin, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// Check runs the full dependency detection. The backend is used to probe the
// Ollama daemon; pass nil to skip the daemon probe.
func Check(ctx context.Context, backend server.Backend) Report {
	r := Report{Distro: DetectDistro()}

	r.OpenCode = detectTool(ctx, "opencode", true, []string{"--version"})
	r.Ollama = detectTool(ctx, "ollama", true, []string{"--version"})
	r.NvidiaSMI = detectTool(ctx, "nvidia-smi", false, nil)

	if r.Ollama.Found && backend != nil {
		r.Daemon = backend.Status(ctx)
	}
	return r
}

func detectTool(ctx context.Context, name string, required bool, versionArgs []string) Tool {
	t := Tool{Name: name, Required: required}
	path, err := lookPath(name)
	if err != nil {
		return t
	}
	t.Found = true
	t.Path = path
	if len(versionArgs) > 0 {
		t.Version = cleanVersion(runVersion(ctx, name, versionArgs...))
	}
	return t
}

// cleanVersion extracts a version-ish token from noisy --version output.
func cleanVersion(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	// Take the first line; strip a leading program name if present.
	line := s
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	fields := strings.Fields(line)
	// Prefer a field that looks like a version (contains a digit and a dot).
	for _, f := range fields {
		if strings.ContainsAny(f, "0123456789") && strings.Contains(f, ".") {
			return strings.TrimPrefix(f, "v")
		}
	}
	return line
}

// ParseOSRelease parses /etc/os-release content into a Distro. Exported for tests.
func ParseOSRelease(content string) Distro {
	vals := map[string]string{}
	sc := bufio.NewScanner(strings.NewReader(content))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			continue
		}
		key := strings.TrimSpace(line[:eq])
		val := strings.Trim(strings.TrimSpace(line[eq+1:]), `"'`)
		vals[key] = val
	}
	return classifyDistro(vals["ID"], vals["ID_LIKE"], vals["PRETTY_NAME"])
}
