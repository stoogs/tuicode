package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"tuicode/internal/hw"
)

const (
	setDevice = iota
	setOpencodeJSON
	setOllamaModels
	setFlashAttn
	setKVCache
	setPrune
	setCount
)

func (m Model) updateSettings(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "esc", "backspace":
		m.screen = screenDashboard
		return m, nil
	case "enter":
		switch m.settingsCursor {
		case setPrune:
			m.status = "pruning unused derived models…"
			m.errMsg = ""
			return m, m.pruneCmd()
		case setOllamaModels:
			dir := ollamaModelsDir()
			if err := exec.Command("xdg-open", dir).Start(); err != nil {
				m.setError("open file manager failed: " + err.Error())
			} else {
				m.status = "opened " + dir
				m.errMsg = ""
			}
			return m, nil
		}
		return m, nil
	case "up", "k":
		m.settingsCursor = wrap(m.settingsCursor-1, setCount)
		return m, nil
	case "down", "j", "tab":
		m.settingsCursor = wrap(m.settingsCursor+1, setCount)
		return m, nil
	case "left", "h":
		return m.adjustSetting(-1)
	case "right", "l":
		return m.adjustSetting(+1)
	}
	return m, nil
}

func (m Model) adjustSetting(dir int) (tea.Model, tea.Cmd) {
	switch m.settingsCursor {
	case setDevice:
		modes := []hw.DeviceMode{hw.Auto, hw.CPUOnly, hw.GPUOnly}
		cur := 0
		for i, mode := range modes {
			if mode == m.opts.DeviceMode {
				cur = i
			}
		}
		m.opts.DeviceMode = modes[wrap(cur+dir, len(modes))]
		m.opts.AppConfig.DeviceMode = string(m.opts.DeviceMode)
		m.persistAppConfig()
		// Re-detect with the new mode immediately.
		return m, m.pollCmd()
	}
	return m, nil
}

// persistAppConfig saves the app config and sets a transient status/error on m.
func (m *Model) persistAppConfig() {
	if m.opts.DryRun {
		m.status = "dry-run: settings not persisted"
		m.errMsg = ""
		return
	}
	if err := m.opts.Store.SaveAppConfig(m.opts.AppConfig); err != nil {
		m.setError("save settings failed: " + err.Error())
		return
	}
	m.status = "settings saved"
	m.errMsg = ""
}

func (m Model) viewSettings() string {
	w := m.contentWidth()
	var b strings.Builder
	b.WriteString(m.header())
	b.WriteString("\n\n")
	b.WriteString(sectionStyle.Render("SETTINGS"))
	b.WriteString("\n")
	b.WriteString(divider(w))
	b.WriteString("\n\n")

	rows := []struct {
		idx   int
		label string
		value string
		edit  bool
	}{
		{setDevice, "Device mode", string(m.opts.DeviceMode), true},
		{setOpencodeJSON, "opencode.json", m.opts.OpencodeJSON, false},
		{setOllamaModels, "Models folder", ollamaModelsDir() + "   (⏎ open in file manager)", false},
		{setFlashAttn, "Flash attention", envStatus("OLLAMA_FLASH_ATTENTION"), false},
		{setKVCache, "KV cache type", envStatus("OLLAMA_KV_CACHE_TYPE"), false},
		{setPrune, "Prune derived", "press ⏎ to delete unused tuicode/ models", false},
	}
	for _, r := range rows {
		cursor := "  "
		label := padRight(r.label, 18)
		val := r.value
		if r.idx == m.settingsCursor {
			cursor = accentStyle.Render("▸ ")
			label = accentStyle.Render(label)
			if r.edit {
				val = selectedStyle.Render(val + " ◂▸")
			} else {
				val = selectedStyle.Render(val)
			}
		} else if !r.edit {
			val = mutedStyle.Render(val)
		}
		b.WriteString(cursor + label + val + "\n")
	}
	b.WriteString("\n")
	b.WriteString(mutedStyle.Render("OLLAMA_MODELS / flash-attn / KV-cache are daemon-level (env on the ollama"))
	b.WriteString("\n")
	b.WriteString(mutedStyle.Render("service); values shown are this shell's view. Set them via a systemd override:"))
	b.WriteString("\n")
	b.WriteString("  " + accentStyle.Render("sudo systemctl edit ollama") + mutedStyle.Render("  →  [Service]"))
	b.WriteString("\n")
	b.WriteString(mutedStyle.Render("    Environment=\"OLLAMA_FLASH_ATTENTION=1\" \"OLLAMA_KV_CACHE_TYPE=q8_0\""))
	b.WriteString("\n")
	b.WriteString(mutedStyle.Render("  Flash-attn + KV-cache q8_0/q4_0 roughly halve/quarter context VRAM — big"))
	b.WriteString("\n")
	b.WriteString(mutedStyle.Render("  gains for fitting larger context. Per-model GPU layers: the GPU column."))
	b.WriteString("\n\n")

	if sl := m.statusLine(); sl != "" {
		b.WriteString(sl + "\n\n")
	}
	b.WriteString(divider(w))
	b.WriteString("\n")
	b.WriteString(footer(
		[2]string{"↑↓", "field"},
		[2]string{"←→", "change"},
		[2]string{"⏎", "run action"},
		[2]string{"esc", "back"},
	))
	return boxWrap(b.String(), m)
}

// envStatus reports an env var's value as this process sees it (best-effort —
// the daemon may have a different environment).
func envStatus(key string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return "not set (daemon default)"
}

// ollamaModelsDir resolves the on-disk models directory: OLLAMA_MODELS if set,
// else the user path (~/.ollama/models) or the systemd-service path, preferring
// whichever exists.
func ollamaModelsDir() string {
	if p := os.Getenv("OLLAMA_MODELS"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	userPath := filepath.Join(home, ".ollama", "models")
	if fi, err := os.Stat(userPath); err == nil && fi.IsDir() {
		return userPath
	}
	const sysPath = "/usr/share/ollama/.ollama/models"
	if fi, err := os.Stat(sysPath); err == nil && fi.IsDir() {
		return sysPath
	}
	return userPath
}

func padRight(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}
