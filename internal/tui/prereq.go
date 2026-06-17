package tui

import (
	"strings"
)

func (m Model) viewPrereq() string {
	r := m.opts.Deps
	var b strings.Builder
	b.WriteString(titleStyle.Render("tuicode can't start — missing prerequisites"))
	b.WriteString("\n\n")

	b.WriteString(toolLine("OpenCode", r.OpenCode.Found, r.OpenCode.Version))
	b.WriteString("\n")
	b.WriteString(toolLine("Ollama", r.Ollama.Found, r.Ollama.Version))
	b.WriteString("\n\n")

	b.WriteString("tuicode needs OpenCode and Ollama, installed separately.\n")
	b.WriteString(mutedStyle.Render("(installing OpenCode does not install a model server)"))
	b.WriteString("\n\n")

	distro := r.Distro
	b.WriteString(sectionStyle.Render(distro.Label() + ":"))
	b.WriteString("\n")
	for _, tool := range []struct {
		name  string
		found bool
	}{{"opencode", r.OpenCode.Found}, {"ollama", r.Ollama.Found}} {
		if tool.found {
			continue
		}
		b.WriteString("\n  " + accentStyle.Render("install "+tool.name+":") + "\n")
		for _, cmd := range distro.InstallCommands(tool.name) {
			b.WriteString("    " + cmd + "\n")
		}
	}
	b.WriteString("\n")
	b.WriteString(mutedStyle.Render("Re-run tuicode once the missing tools are installed and running."))
	b.WriteString("\n\n")
	b.WriteString(footer([2]string{"q", "quit"}))
	b.WriteString("\n")
	return b.String()
}

func toolLine(name string, found bool, version string) string {
	if found {
		v := ""
		if version != "" {
			v = " (" + version + ")"
		}
		return "  " + goodStyle.Render("✓ ") + name + "   " + mutedStyle.Render("found"+v)
	}
	return "  " + badStyle.Render("✗ ") + name + "   " + badStyle.Render("not found")
}
