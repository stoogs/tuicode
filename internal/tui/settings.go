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
	setManageCompaction
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
		m.settingsCursor = m.stepSetting(-1)
		return m, nil
	case "down", "j", "tab":
		m.settingsCursor = m.stepSetting(+1)
		return m, nil
	case "left", "h":
		return m.adjustSetting(-1)
	case "right", "l":
		return m.adjustSetting(+1)
	}
	return m, nil
}

// settingEnabled reports whether a setting row is interactive. The compaction
// sub-settings are disabled (skipped, greyed) while "Manage compaction" is off,
// since they wouldn't be written.
func (m Model) settingEnabled(idx int) bool {
	switch idx {
	case setCompactAuto, setCompactPrune, setReservePct:
		return m.opts.AppConfig.Compaction.Manage
	}
	return true
}

// stepSetting moves the cursor by dir, wrapping and skipping disabled rows.
func (m Model) stepSetting(dir int) int {
	cur := m.settingsCursor
	for i := 0; i < setCount; i++ {
		cur = wrap(cur+dir, setCount)
		if m.settingEnabled(cur) {
			return cur
		}
	}
	return m.settingsCursor
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
	case setManageCompaction:
		m.opts.AppConfig.Compaction.Manage = !m.opts.AppConfig.Compaction.Manage
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

	c := m.opts.AppConfig.Compaction
	rows := []struct {
		idx   int
		label string
		value string
		edit  bool
	}{
		{setDevice, "Device mode", string(m.opts.DeviceMode), true},
		{setDefaultContext, "Default context", ctxDefaultLabel(m.opts.AppConfig.DefaultContext), true},
		{setManageCompaction, "Manage compaction", manageLabel(m.opts.AppConfig.Compaction.Manage), true},
		{setCompactAuto, "    Auto-compact", onOff(c.Auto), true},
		{setCompactPrune, "    Prune tool outputs", onOff(c.Prune), true},
		{setReservePct, "    Compact reserve", reserveLabel(c.ReservePct), true},
		{setOpencodeJSON, "opencode.json", m.opts.OpencodeJSON, false},
		{setOllamaModels, "Models folder", ollamaModelsDir() + "   (⏎ open in file manager)", false},
		{setFlashAttn, "Flash attention", envStatus("OLLAMA_FLASH_ATTENTION"), false},
		{setKVCache, "KV cache type", envStatus("OLLAMA_KV_CACHE_TYPE"), false},
		{setPrune, "Prune derived", "press ⏎ to delete unused tuicode/ models", false},
	}
	for _, r := range rows {
		cursor := "  "
		label := padRight(r.label, 24)
		val := r.value
		switch {
		case !m.settingEnabled(r.idx):
			// Disabled compaction sub-setting: greyed and not selectable.
			label = mutedStyle.Render(label)
			val = mutedStyle.Render(val)
		case r.idx == m.settingsCursor:
			cursor = accentStyle.Render("▸ ")
			label = accentStyle.Render(label)
			if r.edit {
				val = selectedStyle.Render(val + " ◂▸")
			} else {
				val = selectedStyle.Render(val)
			}
		case !r.edit:
			val = mutedStyle.Render(val)
		}
		b.WriteString(cursor + label + val + "\n")
	}
	b.WriteString("\n")
	b.WriteString(mutedStyle.Render(m.settingHelp()))
	b.WriteString("\n\n")
	// The daemon-env commands are tall, so only show them when the flash-attn or
	// KV-cache row is highlighted (keeps the screen short the rest of the time).
	if m.settingsCursor == setFlashAttn || m.settingsCursor == setKVCache {
		d := m.opts.Deps.Distro
		b.WriteString(mutedStyle.Render("Read by the Ollama daemon at startup — set on the daemon, then restart it:"))
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
		if m.detection.Unified {
			mem = "context memory"
		}
		b.WriteString(mutedStyle.Render("  flash-attn + KV-cache q8_0/q4_0 ≈ half/quarter the " + mem + "."))
		b.WriteString("\n")
	}
	b.WriteString("\n")

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

// manageLabel describes the compaction master switch.
func manageLabel(manage bool) string {
	if manage {
		return "on   (writes the 3 settings below)"
	}
	return "off  (your block left untouched)"
}

// settingHelp returns a one-line hint for the highlighted setting, so the screen
// stays short instead of carrying a tall static help block.
func (m Model) settingHelp() string {
	switch m.settingsCursor {
	case setDevice:
		return "Which memory pool drives fit estimates: auto / cpu-only / gpu-only."
	case setDefaultContext:
		return "Seeds new models; each keeps its own value once you change its CTX."
	case setManageCompaction:
		return "On: writes auto/prune/reserve to opencode.json (merged). Off: hand-maintain."
	case setCompactAuto:
		return "OpenCode summarises older turns when the context window fills."
	case setCompactPrune:
		return "Drops earlier tool results (file reads, command output) to reclaim tokens."
	case setReservePct:
		return "Tokens of headroom kept free; scales with context. Applies on next launch."
	case setFlashAttn, setKVCache:
		return "Daemon env var (read-only here) — set it on Ollama and restart, see below."
	default:
		return "Compaction/context settings apply to opencode.json on next launch — no restart."
	}
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
		return "off  (OpenCode default)"
	}
	return strconv.Itoa(pct) + "%  (compact at ~" + strconv.Itoa(100-pct) + "% full)"
}
