package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"tuicode/internal/hw"
	"tuicode/internal/store"
)

// --- dashboard keys (single-page model table) ---

func (m Model) updateDashboard(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Delete confirmation modal takes precedence.
	if m.confirm != "" {
		return m.updateConfirm(msg)
	}
	// "Loaded — open in OpenCode?" prompt: Enter opens; any other key dismisses
	// and is then handled normally.
	if m.loadedPrompt != "" {
		tag := m.loadedPrompt
		m.loadedPrompt = ""
		if k := msg.String(); k == "enter" || k == " " {
			return m.launchOpenCode(tag)
		}
		// fall through to handle the key normally on a dismissed prompt
	}
	switch msg.String() {
	case "q":
		// Graceful quit: unload resident models + prune derived, then exit.
		return m.beginShutdown()

	// delete the selected model (with a yes/no confirm, default no)
	case "backspace", "delete", "x":
		if tag, ok := m.selectedDiskTag(); ok {
			m.confirm = tag
		}
		return m, nil

	// row navigation
	case "up", "k":
		if m.dashCursor > 0 {
			m.dashCursor--
			m.clearTransient() // last edit's confirmation belongs to the old row
		}
		return m, m.ensureDetailsCmd()
	case "down", "j":
		if m.dashCursor < len(m.disk)-1 {
			m.dashCursor++
			m.clearTransient()
		}
		return m, m.ensureDetailsCmd()

	// toggle favourite (selected on startup); only one favourite at a time
	case "f":
		if tag, ok := m.selectedDiskTag(); ok {
			if m.opts.AppConfig.Favourite == tag {
				m.opts.AppConfig.Favourite = ""
				m.status = "unfavourited " + tag
			} else {
				m.opts.AppConfig.Favourite = tag
				m.status = "★ favourite → " + tag
			}
			m.errMsg = ""
			if !m.opts.DryRun {
				_ = m.opts.Store.SaveAppConfig(m.opts.AppConfig)
			}
		}
		return m, nil

	// column navigation (the editable columns) — arrows + Tab (l is "load")
	case "left":
		m.dashCol = wrap(m.dashCol-1, colCount)
		return m, nil
	case "right", "tab":
		m.dashCol = wrap(m.dashCol+1, colCount)
		return m, nil
	case "shift+tab":
		m.dashCol = wrap(m.dashCol-1, colCount)
		return m, nil

	// value change on the focused column
	case ",", "<":
		return m.adjustColumn(-1)
	case ".", ">":
		return m.adjustColumn(+1)

	// actions on the selected model
	case "enter", " ":
		// Progressive: load if stopped; once loaded with the current settings,
		// open OpenCode on it.
		tag, ok := m.selectedDiskTag()
		if !ok {
			return m, nil
		}
		if st := m.pending[tag]; st != "" {
			m.status = tag + " " + st + " in progress…"
			return m, nil
		}
		// loadedFor reports the resident instance's serve tag; serveTag encodes
		// the VRAM-affecting settings (context + GPU layers). When they match,
		// open OpenCode. When they differ — i.e. a VRAM-affecting setting changed
		// since load — (re)load to apply it and STOP at the "loaded — [⏎]
		// continue" prompt, so the new split/VRAM can be measured before
		// launching. doLoad does not chain a launch, so this never opens OpenCode
		// until the model is resident with the chosen settings.
		serve := serveTag(tag, m.ensureConfig(tag), m.opts.DeviceMode)
		if _, actual, loaded := m.loadedFor(tag); loaded && actual == serve {
			return m.launchOpenCode(tag)
		}
		return m.doLoad(tag)
	case "l", "L":
		// Explicit load (vim-friendly).
		if tag, ok := m.selectedDiskTag(); ok {
			return m.doLoad(tag)
		}
		return m, nil
	case "u", "esc":
		// Unload the selected model if it's loaded.
		if tag, ok := m.selectedDiskTag(); ok {
			return m.doUnload(tag)
		}
		return m, nil
	case "c":
		// Continue the selected model's last OpenCode session (loading first if
		// needed). No-op with a hint when there's no recorded session.
		if tag, ok := m.selectedDiskTag(); ok {
			return m.continueSession(tag)
		}
		return m, nil
	case "C":
		// Continue the most recent OpenCode session across all models.
		return m.continueGlobal()
	case "d":
		// Cycle device mode (auto → cpu-only → gpu-only) for estimation/testing.
		return m.cycleDevice()
	case "p":
		// Preferences: the per-model configure screen (context/GPU/sampler).
		if tag, ok := m.selectedDiskTag(); ok {
			m = m.enterConfigure(tag)
		}
		return m, nil
	case "o":
		// ollama pull — pull a model via the onboarding flow (trending list).
		m.onboard.dismissed = false
		m.onboard.done = false
		m.onboard.cursor = 0
		m.screen = screenOnboarding
		return m, nil
	case "s":
		m.screen = screenSettings
		return m, nil
	case "r":
		return m, tea.Batch(m.listCmd(), m.pollCmd())
	}
	return m, nil
}

// updateConfirm handles the delete-confirmation modal. Default is "no": any key
// other than y/Y cancels.
func (m Model) updateConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	tag := m.confirm
	switch msg.String() {
	case "y", "Y":
		m.confirm = ""
		var cmds []tea.Cmd
		// Unload first if a variant is resident, then delete from disk.
		if _, actual, loaded := m.loadedFor(tag); loaded {
			cmds = append(cmds, m.unloadCmd(actual))
		}
		m.pending[tag] = "delete"
		cmds = append(cmds, m.setStatus("deleting "+tag+"…"), m.deleteCmd(tag))
		return m, tea.Batch(cmds...)
	default:
		// Anything else = no.
		m.confirm = ""
		m.status = "delete cancelled"
		return m, nil
	}
}

// cycleDevice advances the device mode and re-detects memory.
func (m Model) cycleDevice() (tea.Model, tea.Cmd) {
	modes := []hw.DeviceMode{hw.Auto, hw.CPUOnly, hw.GPUOnly}
	cur := 0
	for i, mode := range modes {
		if mode == m.opts.DeviceMode {
			cur = i
		}
	}
	m.opts.DeviceMode = modes[wrap(cur+1, len(modes))]
	m.opts.AppConfig.DeviceMode = string(m.opts.DeviceMode)
	if !m.opts.DryRun {
		_ = m.opts.Store.SaveAppConfig(m.opts.AppConfig)
	}
	m.status = "device mode → " + string(m.opts.DeviceMode)
	m.errMsg = ""
	return m, m.pollCmd()
}

// doLoad enforces single-model residency and applies the model's configured
// context/GPU via a derived model. It unloads every other resident model, then
// creates (if needed) and loads the serve tag.
func (m Model) doLoad(base string) (tea.Model, tea.Cmd) {
	cfg := m.ensureConfig(base)
	mode := m.opts.DeviceMode
	serve := serveTag(base, cfg, mode)
	keepAlive := cfg.Residency.KeepAlive()

	// Already resident with the right config and nothing to launch? No-op.
	if _, actual, loaded := m.loadedFor(base); loaded && actual == serve && m.launchAfter != base {
		m.setError(base + " is already loaded")
		return m, nil
	}

	var cmds []tea.Cmd
	// Only one model loaded at a time — unload anything else (incl. a stale
	// derived variant of this same base).
	var swapped int
	for _, lm := range m.loaded {
		if lm.Tag == serve {
			continue
		}
		m.pending[lm.Tag] = "unload"
		cmds = append(cmds, m.unloadCmd(lm.Tag))
		swapped++
	}

	m.pending[base] = "load"
	note := "loading " + base + "…"
	if cfg.ContextLength > 0 || effectiveNumGPU(cfg, mode) >= 0 {
		note = fmt.Sprintf("loading %s (ctx %s, gpu %s)…", base, contextShort(cfg.ContextLength), numGPULabel(cfg, mode))
	}
	if swapped > 0 {
		note = "swapping → " + base + " (freeing VRAM first)…"
	}
	cmds = append(cmds, m.setStatus(note))
	cmds = append(cmds, m.loadServeCmd(base, serve, derivedParams(cfg, mode), keepAlive))

	if !m.spinning {
		m.spinning = true
		cmds = append(cmds, spinnerCmd())
	}
	return m, tea.Batch(cmds...)
}

// doUnload unloads whatever instance is resident for the base model. A no-op
// (no error) when nothing is loaded, so esc/u on a stopped model is harmless.
func (m Model) doUnload(base string) (tea.Model, tea.Cmd) {
	_, actual, loaded := m.loadedFor(base)
	if !loaded {
		return m, nil
	}
	m.pending[base] = "unload"
	cmd := m.setStatus("unloading " + base + "…")
	return m, tea.Batch(cmd, m.unloadCmd(actual))
}

// adjustColumn changes the focused editable column on the selected model and
// persists the per-model config.
func (m Model) adjustColumn(dir int) (tea.Model, tea.Cmd) {
	tag, ok := m.selectedDiskTag()
	if !ok {
		return m, nil
	}
	cfg := m.ensureConfig(tag)
	switch m.dashCol {
	case colContext:
		i := indexInt(contextChoices, cfg.ContextLength)
		cfg.ContextLength = contextChoices[clamp(i+dir, 0, len(contextChoices)-1)]
		m.status = tag + " context → " + contextLabel(cfg.ContextLength)
	case colNumGPU:
		choices := m.gpuChoices(tag)
		i := indexInt(choices, cfg.NumGPU)
		cfg.NumGPU = choices[clamp(i+dir, 0, len(choices)-1)]
		m.status = tag + " gpu layers → " + numGPULabel(cfg, m.opts.DeviceMode)
	case colPreset:
		presets := store.Presets()
		cur := matchPreset(cfg.Parameters)
		p := presets[wrap(cur+dir, len(presets))]
		cfg.Parameters = p.Params
		m.status = fmt.Sprintf("%s preset → %s (temp %.2f · top_p %.2f · top_k %d)",
			tag, p.Name, p.Params.Temperature, p.Params.TopP, p.Params.TopK)
	}
	m.errMsg = ""
	m.saveConfig(cfg)
	return m, nil
}

// --- prereq keys ---

func (m Model) updatePrereq(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "enter", "esc", "ctrl+c":
		m.quit = true
		return m, tea.Quit
	}
	return m, nil
}
