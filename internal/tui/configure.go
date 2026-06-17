package tui

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"tuicode/internal/hw"
	"tuicode/internal/store"
)

// configure fields (rows)
const (
	fldPreset = iota
	fldContext
	fldNumGPU
	fldCount
)

type configureState struct {
	tag         string
	displayName string
	field       int

	presetIdx int
	context   int // 0 = model default
	numGPU    int
	params    store.Parameters

	paramsB float64
	quant   string
	est     hw.Estimate
	from    screenID // screen to return to
}

// contextChoices steps the context in 8k increments (0 = model default).
var contextChoices = buildContextChoices()

func buildContextChoices() []int {
	c := []int{0}
	for k := 8192; k <= 131072; k += 8192 {
		c = append(c, k)
	}
	return c
}

// enterConfigure initializes the configure screen for a tag.
func (m Model) enterConfigure(tag string) Model {
	cfg := m.ensureConfig(tag)

	// Pull param/quant metadata from the disk listing when available.
	paramsB := cfg.ParamsBillions
	quant := cfg.Quant
	for _, dm := range m.disk {
		if dm.Tag == tag {
			if paramsB == 0 {
				paramsB = hw.ParseParamsBillions(dm.ParamSize)
			}
			if quant == "" {
				quant = dm.Quant
			}
		}
	}

	st := configureState{
		tag:         tag,
		displayName: cfg.DisplayName,
		context:     cfg.ContextLength,
		numGPU:      cfg.NumGPU,
		params:      cfg.Parameters,
		paramsB:     paramsB,
		quant:       quant,
		from:        m.screen,
	}
	// Match preset index to stored params.
	st.presetIdx = matchPreset(cfg.Parameters)
	st.est = hw.EstimateContext(m.detection, paramsB, quant, 0)

	m.cfg = st
	m.screen = screenConfigure
	return m
}

func matchPreset(p store.Parameters) int {
	for i, pr := range store.Presets() {
		if pr.Params == p {
			return i
		}
	}
	return 0
}

func (m Model) updateConfigure(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	c := &m.cfg
	switch msg.String() {
	case "q", "esc", "backspace":
		m.screen = c.from
		return m, nil
	case "up", "k":
		c.field = wrap(c.field-1, fldCount)
		return m, nil
	case "down", "j", "tab":
		c.field = wrap(c.field+1, fldCount)
		return m, nil
	case "left", "h", ",":
		m.adjustField(-1)
		return m, nil
	case "right", "l", ".":
		m.adjustField(+1)
		return m, nil
	case "e":
		// Use the estimated context.
		if c.est.MaxContext > 0 {
			c.context = c.est.MaxContext
		}
		return m, nil
	case "s", "enter":
		return m.saveConfigure()
	}
	return m, nil
}

// adjustField changes the value of the focused field by dir (-1/+1).
func (m *Model) adjustField(dir int) {
	c := &m.cfg
	switch c.field {
	case fldPreset:
		presets := store.Presets()
		c.presetIdx = wrap(c.presetIdx+dir, len(presets))
		c.params = presets[c.presetIdx].Params
	case fldContext:
		i := indexInt(contextChoices, c.context)
		i = clamp(i+dir, 0, len(contextChoices)-1)
		c.context = contextChoices[i]
	case fldNumGPU:
		i := indexInt(numGPUChoices, c.numGPU)
		c.numGPU = numGPUChoices[clamp(i+dir, 0, len(numGPUChoices)-1)]
	}
}

func (m Model) saveConfigure() (tea.Model, tea.Cmd) {
	c := m.cfg
	cfg := m.ensureConfig(c.tag)
	cfg.ContextLength = c.context
	cfg.NumGPU = c.numGPU
	cfg.Parameters = c.params
	if cfg.ParamsBillions == 0 {
		cfg.ParamsBillions = c.paramsB
	}
	if cfg.Quant == "" {
		cfg.Quant = c.quant
	}
	m.saveConfig(cfg) // updates cache + persists (no-op write on dry-run)
	note := "saved config for " + c.tag
	if m.opts.DryRun {
		note = "dry-run: config for " + c.tag + " not persisted"
	}
	cmd := m.setStatus(note)
	m.screen = c.from
	return m, cmd
}

func (m Model) viewConfigure() string {
	w := m.contentWidth()
	c := m.cfg
	var b strings.Builder
	b.WriteString(m.header())
	b.WriteString("\n\n")
	b.WriteString(sectionStyle.Render("CONFIGURE  ") + accentStyle.Render(c.tag))
	b.WriteString("\n")
	b.WriteString(divider(w))
	b.WriteString("\n\n")

	cfgForGPU := store.ModelConfig{NumGPU: c.numGPU}
	rows := []struct {
		field int
		label string
		value string
	}{
		{fldPreset, "Preset", fmt.Sprintf("%s  (temp %.2f · top_p %.2f · top_k %d)",
			store.Presets()[c.presetIdx].Name, c.params.Temperature, c.params.TopP, c.params.TopK)},
		{fldContext, "Context", contextLabel(c.context)},
		{fldNumGPU, "GPU layers", numGPULabel(cfgForGPU, m.opts.DeviceMode)},
	}
	for _, r := range rows {
		cursor := "  "
		label := fmt.Sprintf("%-14s", r.label)
		val := r.value
		if r.field == c.field {
			cursor = accentStyle.Render("▸ ")
			label = accentStyle.Render(label)
			val = selectedStyle.Render(val)
		}
		b.WriteString(cursor + label + val + "\n")
	}
	b.WriteString("\n")

	// Estimate hint.
	if c.est.ParamsBillions > 0 {
		line := fmt.Sprintf("estimate: ~%s ctx fits in %s (%.1fGB footprint, %s)",
			fmtCtx(c.est.MaxContext), c.est.Source, c.est.FootprintGB, m.detectionLabel())
		b.WriteString(mutedStyle.Render(line) + "\n")
		if c.est.Warning != "" {
			b.WriteString(warnStyle.Render("⚠ "+c.est.Warning) + "\n")
		}
		b.WriteString(mutedStyle.Render("press [e] to use the estimated context") + "\n")
	} else {
		b.WriteString(mutedStyle.Render("estimate unavailable (unknown params/quant)") + "\n")
	}
	b.WriteString("\n")
	b.WriteString(mutedStyle.Render("context/GPU are baked into a derived model at load time so OpenCode honors them"))
	b.WriteString("\n\n")

	if sl := m.statusLine(); sl != "" {
		b.WriteString(sl + "\n\n")
	}
	b.WriteString(divider(w))
	b.WriteString("\n")
	b.WriteString(footer(
		[2]string{"↑↓", "field"},
		[2]string{"←→", "change"},
		[2]string{"e", "use estimate"},
		[2]string{"s", "save"},
		[2]string{"esc", "cancel"},
	))
	return boxWrap(b.String(), m)
}

func (m Model) detectionLabel() string {
	mem := m.detection.Authoritative()
	return fmt.Sprintf("%s %.0fGB", mem.Source, float64(mem.Total)/gib)
}

func contextLabel(ctx int) string {
	if ctx <= 0 {
		return "model default"
	}
	return strconv.Itoa(ctx)
}

// --- small helpers ---

func wrap(i, n int) int {
	if n <= 0 {
		return 0
	}
	i %= n
	if i < 0 {
		i += n
	}
	return i
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func indexInt(s []int, v int) int {
	for i, x := range s {
		if x == v {
			return i
		}
	}
	return 0
}
