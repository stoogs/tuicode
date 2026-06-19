package tui

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"tuicode/internal/hw"
)

const (
	setDevice = iota
	setDefaultContext
	setCompactAuto
	setCompactPrune
	setReservePct
	setOpencodeJSON
	setOllamaModels
	setFlashAttn
	setKVCache
	setPrune
	setCount
)

// reservePctChoices are the selectable compaction-reserve percentages.
var reservePctChoices = []int{0, 10, 15, 20, 25, 30, 40, 50}

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
			if err := openPath(dir); err != nil {
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
	case setDefaultContext:
		i := indexInt(contextChoices, m.opts.AppConfig.DefaultContext)
		m.opts.AppConfig.DefaultContext = contextChoices[clamp(i+dir, 0, len(contextChoices)-1)]
		m.persistAppConfig()
	case setCompactAuto:
		m.opts.AppConfig.Compaction.Auto = !m.opts.AppConfig.Compaction.Auto
		m.persistAppConfig()
	case setCompactPrune:
		m.opts.AppConfig.Compaction.Prune = !m.opts.AppConfig.Compaction.Prune
		m.persistAppConfig()
	case setReservePct:
		i := indexInt(reservePctChoices, m.opts.AppConfig.Compaction.ReservePct)
		if i < 0 {
			i = indexInt(reservePctChoices, 25) // snap an off-list value to the default
		}
		m.opts.AppConfig.Compaction.ReservePct = reservePctChoices[clamp(i+dir, 0, len(reservePctChoices)-1)]
		m.persistAppConfig()
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
		{setDefaultContext, "Default context", ctxDefaultLabel(m.opts.AppConfig.DefaultContext), true},
		{setCompactAuto, "Auto-compact", onOff(m.opts.AppConfig.Compaction.Auto), true},
		{setCompactPrune, "Prune tool outputs", onOff(m.opts.AppConfig.Compaction.Prune), true},
		{setReservePct, "Compact reserve", reserveLabel(m.opts.AppConfig.Compaction.ReservePct), true},
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
	b.WriteString(mutedStyle.Render("Default context seeds new models (each keeps its own once you change its CTX)."))
	b.WriteString("\n")
	b.WriteString(mutedStyle.Render("Auto-compact/prune/reserve are written to opencode.json so long sessions stay"))
	b.WriteString("\n")
	b.WriteString(mutedStyle.Render("inside the window — reserve scales with the context the model runs at."))
	b.WriteString("\n\n")
	d := m.opts.Deps.Distro
	b.WriteString(mutedStyle.Render("OLLAMA_MODELS / flash-attn / KV-cache are read by the Ollama daemon at startup —"))
	b.WriteString("\n")
	b.WriteString(mutedStyle.Render("restarting tuicode won't apply them (it's just a client). The values above are"))
	b.WriteString("\n")
	b.WriteString(mutedStyle.Render("read-only here; set them on the daemon, then restart Ollama:"))
	b.WriteString("\n")
	if d.IsMac() {
		// Per-var launchctl; the menu-bar app reads these on (re)launch. KV-cache
		// quant is ignored unless flash-attn is on, so that one comes first.
		b.WriteString("  " + accentStyle.Render("launchctl setenv OLLAMA_FLASH_ATTENTION 1") + mutedStyle.Render("     ← set this first"))
		b.WriteString("\n")
		b.WriteString("  " + accentStyle.Render("launchctl setenv OLLAMA_KV_CACHE_TYPE q8_0") + mutedStyle.Render("    ← needs flash-attn on"))
		b.WriteString("\n")
	} else {
		// One systemd override sets both at once (so flash-attn is already on).
		b.WriteString("  " + accentStyle.Render("sudo systemctl edit ollama") + mutedStyle.Render("   →   [Service]"))
		b.WriteString("\n")
		b.WriteString(mutedStyle.Render(`    Environment="OLLAMA_FLASH_ATTENTION=1" "OLLAMA_KV_CACHE_TYPE=q8_0"`))
		b.WriteString("\n")
	}
	b.WriteString("  " + mutedStyle.Render("restart: ") + accentStyle.Render(d.DaemonRestartCmd()))
	b.WriteString("\n")
	mem := "context VRAM"
	tail := "  context. Per-model GPU layers: the GPU column."
	if m.detection.Unified {
		// Apple Silicon: KV cache lives in unified memory, and the GPU column only
		// hurts (no memory split).
		mem = "context memory"
		tail = "  context in unified memory (the GPU column doesn't help)."
	}
	b.WriteString(mutedStyle.Render("  Flash-attn + KV-cache q8_0/q4_0 ≈ half/quarter the " + mem + " — fits larger"))
	b.WriteString("\n")
	b.WriteString(mutedStyle.Render(tail))
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

// envStatus reports an env var's effective value for display (read-only — set it
// on the daemon, see the note below the table). On macOS it prefers the value set
// via `launchctl setenv` (what the Ollama app inherits) over this shell's env.
func envStatus(key string) string {
	if v, ok := launchdEnv(key); ok {
		return v + "  (launchctl)"
	}
	if v := os.Getenv(key); v != "" {
		return v + "  (this shell)"
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

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

// ctxDefaultLabel renders the global default-context value (0 = each model uses
// its own default).
func ctxDefaultLabel(ctx int) string {
	if ctx <= 0 {
		return "auto (per-model default)"
	}
	return contextShort(ctx) + "   (new models start here)"
}

// reserveLabel renders the compaction-reserve percentage.
func reserveLabel(pct int) string {
	if pct <= 0 {
		return "off (OpenCode default headroom)"
	}
	return strconv.Itoa(pct) + "% of context   (compact at ~" + strconv.Itoa(100-pct) + "% full)"
}
