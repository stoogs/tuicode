package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"tuicode/internal/hw"
	"tuicode/internal/server"
	"tuicode/internal/store"
)

// editable columns — the cursor's column index maps directly to these, and they
// appear in the table in this same order (CTX, GPU, PRESET).
const (
	colContext = iota
	colNumGPU
	colPreset
	colCount
)

// table layout: column widths and which table columns are editable.
//
//	ST MODEL SIZE PARAMS ON CTX GPU PRESET
var (
	colWidths      = []int{4, 22, 6, 12, 4, 7, 6, 14}
	colHeaders     = []string{"ST", "MODEL", "SIZE", "PARAMS", "ON", "CTX", "GPU", "PRESET"}
	editableIdx    = []int{5, 6, 7} // CTX, GPU, PRESET
	tableWidthHint = sum(colWidths) + len(colWidths) + 1
)

func sum(xs []int) int {
	t := 0
	for _, x := range xs {
		t += x
	}
	return t
}

// header renders the top status line shared by all live screens.
func (m Model) header() string {
	w := m.contentWidth()
	left := titleStyle.Render("tuicode")

	var parts []string
	parts = append(parts, "device: "+string(m.opts.DeviceMode))
	det := m.detection
	if det.GPU != nil {
		parts = append(parts, fmt.Sprintf("GPU %.0fGB", det.GPU.TotalGB()))
	} else {
		parts = append(parts, fmt.Sprintf("RAM %.0fGB", det.RAM.TotalGB()))
	}
	parts = append(parts, "Ollama "+dot(m.daemon.Reachable))
	right := headerStyle.Render(strings.Join(parts, " · "))

	return padBetween(left, right, w) + "\n" + divider(w)
}

// footer renders a key-hint line.
func footer(pairs ...[2]string) string {
	var parts []string
	for _, p := range pairs {
		parts = append(parts, keyStyle.Render("["+p[0]+"]")+" "+helpStyle.Render(p[1]))
	}
	return strings.Join(parts, "   ")
}

func (m Model) statusLine() string {
	if m.errMsg != "" {
		return badStyle.Render("✗ " + m.errMsg)
	}
	if m.status != "" {
		return goodStyle.Render(m.status)
	}
	return ""
}

func (m Model) viewDashboard() string {
	w := m.contentWidth()
	barW := min(w, tableWidthHint)
	var b strings.Builder
	b.WriteString(m.header())
	b.WriteString("\n")

	// Banner line — always one line so nothing below shifts.
	switch {
	case !m.hasPolled:
		b.WriteString(mutedStyle.Render("connecting to Ollama…"))
	case !m.daemon.Reachable:
		b.WriteString(badStyle.Render("Ollama daemon not reachable — ") +
			accentStyle.Render("sudo systemctl start ollama"))
	default:
		b.WriteString(mutedStyle.Render(fmt.Sprintf("%d model(s) on disk · %d loaded", len(m.disk), len(m.loaded))))
	}
	b.WriteString("\n\n")

	// The model table (all models on disk, live state + inline-editable config).
	b.WriteString(m.renderTable())
	b.WriteString("\n\n")

	// Info zone — always exactly 3 lines (stable layout, no bouncing).
	b.WriteString(m.renderInfo())
	b.WriteString("\n\n")

	// Memory bars: system RAM, then VRAM (when a GPU is in play).
	b.WriteString(m.renderRamBar(barW))
	b.WriteString("\n")
	if m.detection.GPU != nil {
		b.WriteString(m.renderVramBar(barW))
	} else {
		b.WriteString(mutedStyle.Render("VRAM  (no GPU — RAM is authoritative)"))
	}
	b.WriteString("\n\n")

	// Session line.
	b.WriteString(m.renderSession())
	b.WriteString("\n")

	// Status line — always one line (blank when empty) so it never bounces.
	b.WriteString(m.statusLine())
	b.WriteString("\n\n")

	// Delete confirmation takes over the footer area when active.
	if m.confirm != "" {
		b.WriteString(badStyle.Render("Delete "+m.confirm+"?") +
			mutedStyle.Render(" removes it from disk (Ollama rm). ") +
			keyStyle.Render("[y]") + helpStyle.Render(" yes   ") +
			keyStyle.Render("[N]") + helpStyle.Render(" no (default)"))
		b.WriteString("\n")
		b.WriteString(mutedStyle.Render("any other key cancels"))
		return boxWrap(b.String(), m)
	}

	// "Loaded — open in OpenCode?" prompt takes over the footer area.
	if m.loadedPrompt != "" {
		b.WriteString(goodStyle.Render("✓ "+m.loadedPrompt+" loaded") + "   " +
			keyStyle.Render("[Enter]") + helpStyle.Render(" continue in OpenCode"))
		b.WriteString("\n")
		b.WriteString(mutedStyle.Render("any other key stays on the dashboard"))
		return boxWrap(b.String(), m)
	}

	b.WriteString(footer(
		[2]string{"↑↓", "model"},
		[2]string{"←→/tab", "column"},
		[2]string{",.", "change"},
		[2]string{"⏎/l", "load → open"},
		[2]string{"o", "open"},
	))
	b.WriteString("\n")
	b.WriteString(footer(
		[2]string{"esc/u", "unload"},
		[2]string{"d", "device"},
		[2]string{"c", "configure"},
		[2]string{"del", "delete"},
		[2]string{"p", "pull"},
		[2]string{"s", "settings"},
		[2]string{"q", "quit"},
	))
	return boxWrap(b.String(), m)
}

// renderTable draws the full model table with box-drawing borders.
func (m Model) renderTable() string {
	var b strings.Builder
	b.WriteString(tableRule("┌", "┬", "┐"))
	b.WriteString("\n")
	b.WriteString(headerRow())
	b.WriteString("\n")
	b.WriteString(tableRule("├", "┼", "┤"))
	b.WriteString("\n")

	if len(m.disk) == 0 {
		msg := "no models on disk — press [p] to pull a starter"
		if !m.daemon.Reachable {
			msg = "daemon down — start ollama to discover models"
		}
		b.WriteString(spanRow(mutedStyle.Render(msg)))
		b.WriteString("\n")
	} else {
		for i, dm := range m.disk {
			b.WriteString(m.renderRow(i, dm))
			b.WriteString("\n")
		}
	}
	b.WriteString(tableRule("└", "┴", "┘"))
	return b.String()
}

// tableRule renders a horizontal border line with the given corner runes.
func tableRule(left, mid, right string) string {
	parts := make([]string, len(colWidths))
	for i, w := range colWidths {
		parts[i] = strings.Repeat("─", w+1) // +1 for the leading pad space
	}
	return dividerStyle.Render(left + strings.Join(parts, mid) + right)
}

func bar() string { return dividerStyle.Render("│") }

// headerRow renders the bold column-label row.
func headerRow() string {
	var cells []string
	for i, label := range colHeaders {
		cells = append(cells, " "+sectionStyle.Render(cell(label, colWidths[i])))
	}
	return bar() + strings.Join(cells, bar()) + bar()
}

// spanRow renders a single message spanning the table width.
func spanRow(content string) string {
	inner := sum(colWidths) + len(colWidths) - 1 // widths + inner pads
	plain := lipglossWidth(content)
	pad := inner - plain
	if pad < 0 {
		pad = 0
	}
	return bar() + " " + content + strings.Repeat(" ", pad) + bar()
}

// renderRow renders one model row.
func (m Model) renderRow(i int, dm server.DiskModel) string {
	cfg := m.ensureConfig(dm.Tag)
	lm, _, loaded := m.loadedFor(dm.Tag)
	selected := i == m.dashCursor

	glyph, gstyle := m.stateGlyph(dm.Tag)
	stateCell := gstyle.Render(center(glyph, colWidths[0]))

	modelText := cell(dm.Tag, colWidths[1])
	modelCell := modelText
	if selected {
		modelCell = accentStyle.Render(modelText)
	}

	sizeCell := cell(fmtGBShort(dm.Size), colWidths[2])

	// params/quant column, e.g. "7B Q4_K_M"
	paramsCell := cell(paramsQuant(dm), colWidths[3])

	// live placement column (where the resident instance runs)
	onStr := "—"
	if loaded {
		onStr = shortProc(lm)
	}
	onCell := cell(onStr, colWidths[4])

	// editable values, in the same order as editableIdx (CTX, GPU, PRESET)
	ctxStr := contextShort(cfg.ContextLength)
	if loaded && cfg.ContextLength > 0 && lm.Context > 0 && lm.Context != cfg.ContextLength {
		ctxStr += "*" // configured value differs from the loaded value (reload to apply)
	}
	gpuStr := numGPUShort(cfg, m.opts.DeviceMode)
	presetStr := store.Presets()[matchPreset(cfg.Parameters)].Name
	cpuForced := m.opts.DeviceMode == hw.CPUOnly

	editVals := []string{ctxStr, gpuStr, presetStr}
	editCells := make([]string, len(editableIdx))
	for k, tcol := range editableIdx {
		text := cell(editVals[k], colWidths[tcol])
		switch {
		case selected && m.dashCol == k:
			editCells[k] = selectedStyle.Render(text)
		case k == colNumGPU && cpuForced:
			// Flag CPU-only mode loudly so it isn't a silent surprise.
			editCells[k] = badStyle.Render(text)
		case selected:
			editCells[k] = text
		default:
			editCells[k] = mutedStyle.Render(text)
		}
	}

	cells := []string{
		stateCell, modelCell, sizeCell, paramsCell, onCell,
		editCells[0], editCells[1], editCells[2],
	}
	var out strings.Builder
	out.WriteString(bar())
	for _, c := range cells {
		out.WriteString(" " + c + bar())
	}
	return out.String()
}

// stateGlyph returns the status glyph + style for a model.
//
//	loading → ◐ yellow   stopping/deleting → ● red
//	running → ● green     stopped → ○ grey
func (m Model) stateGlyph(tag string) (string, lipgloss.Style) {
	switch m.pending[tag] {
	case "load":
		return "◐", warnStyle
	case "unload", "delete":
		return "●", badStyle
	}
	if _, _, loaded := m.loadedFor(tag); loaded {
		return "●", goodStyle
	}
	return "○", mutedStyle
}

// numGPUChoices are the GPU-offload steps cycled with ,/. (-1 = auto, 0 = CPU).
var numGPUChoices = []int{store.NumGPUAuto, 0, 8, 16, 24, 32, 48, 64, 99}

// numGPUShort renders the GPU-layers cell value.
func numGPUShort(cfg store.ModelConfig, mode hw.DeviceMode) string {
	if mode == hw.CPUOnly {
		return "cpu!" // forced by device mode
	}
	switch {
	case cfg.NumGPU < 0:
		return "auto"
	case cfg.NumGPU == 0:
		return "cpu"
	case cfg.NumGPU >= 99:
		return "all"
	default:
		return fmt.Sprintf("%d", cfg.NumGPU)
	}
}

// numGPULabel is a verbose form for status messages.
func numGPULabel(cfg store.ModelConfig, mode hw.DeviceMode) string {
	if mode == hw.CPUOnly {
		return "cpu (forced by device mode)"
	}
	switch {
	case cfg.NumGPU < 0:
		return "auto"
	case cfg.NumGPU == 0:
		return "cpu (0 layers)"
	case cfg.NumGPU >= 99:
		return "all layers"
	default:
		return fmt.Sprintf("%d layers", cfg.NumGPU)
	}
}

// modelMeta returns the best-known params (billions) and quant for a tag.
func (m Model) modelMeta(tag string) (float64, string) {
	cfg := m.ensureConfig(tag)
	paramsB, quant := cfg.ParamsBillions, cfg.Quant
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
	return paramsB, quant
}

// renderInfo renders the information zone for the selected model. It always
// emits exactly three lines so the layout below never shifts. While the model is
// loading it shows an animated progress bar instead of the estimate.
func (m Model) renderInfo() string {
	tag, ok := m.selectedDiskTag()
	if !ok {
		return mutedStyle.Render("INFO  no model selected") + "\n\n"
	}
	if m.pending[tag] == "load" {
		return m.renderLoadingInfo(tag)
	}

	cfg := m.ensureConfig(tag)
	paramsB, quant := m.modelMeta(tag)
	usage := hw.EstimateUsage(paramsB, quant, cfg.ContextLength)

	mem := m.detection.Authoritative()
	totalGB := float64(mem.Total) / gib
	reserveGB := float64(m.detection.Reserve()) / gib
	src := strings.ToUpper(mem.Source)

	var b strings.Builder
	b.WriteString(sectionStyle.Render("INFO  ") + accentStyle.Render(tag) + "\n")

	// --- estimated memory line (green while the CTX column is focused) ---
	estStyle := mutedStyle
	if m.dashCol == colContext {
		estStyle = goodStyle
	}
	if !usage.Known {
		b.WriteString("  " + mutedStyle.Render("est. mem   unknown (params/quant not detected)") + "\n")
	} else {
		ctxNote := contextShort(cfg.ContextLength)
		est := fmt.Sprintf("est. mem   %.1fGB   weights %.1f + ctx(%s) %.1f",
			usage.TotalGB, usage.WeightsGB, ctxNote, usage.KVGB)
		fit := ""
		if totalGB > 0 {
			if usage.TotalGB+reserveGB <= totalGB {
				fit = goodStyle.Render(fmt.Sprintf("   fits %.0fGB %s ✓", totalGB, src))
			} else {
				fit = warnStyle.Render(fmt.Sprintf("   may spill to CPU (> %.0fGB %s)", totalGB, src))
			}
		}
		b.WriteString("  " + estStyle.Render(est) + fit + "\n")
	}

	// --- params line (green while the PRESET column is focused) ---
	presetName := store.Presets()[matchPreset(cfg.Parameters)].Name
	params := fmt.Sprintf("params     %s · temp %.2f · top_p %.2f · top_k %d",
		presetName, cfg.Parameters.Temperature, cfg.Parameters.TopP, cfg.Parameters.TopK)
	if m.dashCol == colPreset {
		b.WriteString("  " + goodStyle.Render(params))
	} else {
		b.WriteString("  " + mutedStyle.Render(params))
	}
	return b.String()
}

// renderLoadingInfo renders the animated loading bar for a model being loaded.
func (m Model) renderLoadingInfo(tag string) string {
	var b strings.Builder
	b.WriteString(sectionStyle.Render("INFO  ") + warnStyle.Render("loading "+tag) + "\n")

	// If the model has partially appeared in ps we can show a real %, otherwise
	// an indeterminate moving bar (Ollama doesn't stream VRAM-load progress).
	barW := 28
	lm, _, present := m.loadedFor(tag)
	if present && lm.Size > 0 && lm.SizeVRAM > 0 && lm.SizeVRAM < lm.Size {
		frac := float64(lm.SizeVRAM) / float64(lm.Size)
		b.WriteString("  loading  " + progressBar(frac, barW) +
			fmt.Sprintf("  %d%%\n", int(frac*100+0.5)))
	} else {
		b.WriteString("  loading  " + movingBar(m.spinFrame, barW) + "  " +
			mutedStyle.Render("warming into VRAM…") + "\n")
	}
	b.WriteString("  " + mutedStyle.Render("(other models are being unloaded — one model loads at a time)"))
	return b.String()
}

// renderSession renders the OpenCode session line for the selected model.
func (m Model) renderSession() string {
	tag, ok := m.selectedDiskTag()
	if !ok {
		return mutedStyle.Render("SESSION  no model selected")
	}
	_, _, loaded := m.loadedFor(tag)
	label := sectionStyle.Render("SESSION  ") + mutedStyle.Render("OpenCode not running")
	if loaded {
		return label + "   " + footer([2]string{"o", "open OpenCode on " + tag})
	}
	return label + "   " + mutedStyle.Render("(load "+tag+" to open a session)")
}

// renderRamBar renders the system-RAM usage bar (MemAvailable vs MemTotal).
func (m Model) renderRamBar(w int) string {
	ram := m.detection.RAM
	totalGB := float64(ram.Total) / gib
	if totalGB <= 0 {
		return mutedStyle.Render("RAM   (unknown)")
	}
	usedGB := float64(ram.Total-ram.Free) / gib
	return fmt.Sprintf("RAM   %s  %s / %s GB", progressBar(usedGB/totalGB, barWidth(w)), fmtGBf(usedGB), fmtGBf(totalGB))
}

// renderVramBar renders the GPU VRAM usage bar from nvidia-smi (or summed
// footprints when free VRAM is unavailable).
func (m Model) renderVramBar(w int) string {
	g := m.detection.GPU
	if g == nil {
		return mutedStyle.Render("VRAM  (no GPU)")
	}
	totalGB := g.TotalGB()
	if totalGB <= 0 {
		return mutedStyle.Render("VRAM  (unknown)")
	}
	var usedGB float64
	if g.Free > 0 {
		usedGB = float64(g.Total-g.Free) / gib
	} else {
		var s int64
		for _, lm := range m.loaded {
			s += lm.SizeVRAM
		}
		usedGB = float64(s) / gib
	}
	return fmt.Sprintf("VRAM  %s  %s / %s GB", progressBar(usedGB/totalGB, barWidth(w)), fmtGBf(usedGB), fmtGBf(totalGB))
}

func barWidth(w int) int {
	bw := w - 22
	if bw < 10 {
		bw = 10
	}
	return bw
}

// movingBar renders an indeterminate "bouncing" progress bar for the given
// animation frame.
func movingBar(frame, width int) string {
	if width < 4 {
		width = 4
	}
	const win = 3
	span := width - win
	if span < 1 {
		span = 1
	}
	pos := frame % (2 * span)
	if pos > span {
		pos = 2*span - pos // bounce back
	}
	runes := make([]rune, width)
	for i := range runes {
		runes[i] = '░'
	}
	for i := 0; i < win && pos+i < width; i++ {
		runes[pos+i] = '▓'
	}
	return accentStyle.Render(string(runes))
}

// --- cell formatting helpers ---
//
// (min/max are Go 1.21+ builtins.)

// cell left-aligns s into width w (truncating with … when too long).
func cell(s string, w int) string {
	r := []rune(s)
	if len(r) > w {
		if w <= 1 {
			return string(r[:w])
		}
		return string(r[:w-1]) + "…"
	}
	return s + strings.Repeat(" ", w-len(r))
}

// center centers s within width w.
func center(s string, w int) string {
	r := []rune(s)
	if len(r) >= w {
		return cell(s, w)
	}
	left := (w - len(r)) / 2
	right := w - len(r) - left
	return strings.Repeat(" ", left) + s + strings.Repeat(" ", right)
}

func fmtGBShort(bytes int64) string {
	return fmt.Sprintf("%.1fGB", float64(bytes)/gib)
}

// paramsQuant formats a model's parameter size + quantization, e.g. "7B Q4_K_M".
func paramsQuant(dm server.DiskModel) string {
	s := strings.TrimSpace(strings.TrimSpace(dm.ParamSize) + " " + strings.TrimSpace(dm.Quant))
	if s == "" {
		return "—"
	}
	return s
}

func contextShort(ctx int) string {
	if ctx <= 0 {
		return "default"
	}
	if ctx >= 1024 && ctx%1024 == 0 {
		return fmt.Sprintf("%dk", ctx/1024)
	}
	return fmt.Sprintf("%d", ctx)
}

// shortProc renders the GPU/CPU split compactly: "GPU", "CPU", or "42%G".
func shortProc(lm server.LoadedModel) string {
	g := lm.GPUPercent()
	switch {
	case g >= 100:
		return "GPU"
	case g <= 0:
		return "CPU"
	default:
		return fmt.Sprintf("%d%%G", g)
	}
}
