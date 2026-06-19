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

// table layout: column widths and which table columns are editable. There's no
// status column — a model's state is shown by the row colour (green = loaded,
// yellow = loading, red = stopping/deleting).
//
//	★ MODEL SIZE PARAMS GPU/CPU CTX GPU PRESET
var (
	colWidths      = []int{1, 21, 6, 12, 8, 7, 6, 14}
	colHeaders     = []string{"★", "MODEL", "SIZE", "PARAMS", "GPU/CPU", "CTX", "GPU", "PRESET"}
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
	switch {
	case det.Unified && det.GPU != nil:
		parts = append(parts, fmt.Sprintf("Unified %.0fGB", det.GPU.TotalGB()))
	case det.GPU != nil:
		parts = append(parts, fmt.Sprintf("GPU %.0fGB", det.GPU.TotalGB()))
	default:
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
			accentStyle.Render(m.opts.Deps.Distro.DaemonStartCmd()))
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

	// Memory bars: system RAM, then VRAM (when a GPU is in play). The projected
	// footprint of the selected, not-yet-loaded model is overlaid on whichever
	// pool would hold it.
	proj := m.selectedProjectionGB()
	switch {
	case m.detection.Unified:
		// Apple Silicon: GPU and CPU share one pool — a single bar, not two.
		b.WriteString(m.renderUnifiedBar(barW, proj))
		b.WriteString("\n")
		b.WriteString(mutedStyle.Render(m.detection.GPUName + " · unified memory (GPU shares system RAM)"))
	case m.detection.GPU != nil:
		b.WriteString(m.renderRamBar(barW, 0))
		b.WriteString("\n")
		b.WriteString(m.renderVramBar(barW, proj))
	default:
		b.WriteString(m.renderRamBar(barW, proj))
		b.WriteString("\n")
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
		[2]string{"c", "continue"},
		[2]string{"C", "global"},
	))
	b.WriteString("\n")
	b.WriteString(footer(
		[2]string{"o", "ollama pull"},
		[2]string{"p", "prefs"},
		[2]string{"esc/u", "unload"},
		[2]string{"d", "device"},
		[2]string{"f", "★ fav"},
		[2]string{"del", "delete"},
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

// renderRow renders one model row. The whole row is tinted by state — green when
// loaded, yellow while loading, red while stopping/deleting — so there's no need
// for a separate status column.
func (m Model) renderRow(i int, dm server.DiskModel) string {
	cfg := m.ensureConfig(dm.Tag)
	lm, _, loaded := m.loadedFor(dm.Tag)
	selected := i == m.dashCursor

	// Row state colour (stateful=false → a stopped model, rendered normally).
	rowStyle, stateful := mutedStyle, false
	switch m.pending[dm.Tag] {
	case "load":
		rowStyle, stateful = warnStyle, true
	case "unload", "delete":
		rowStyle, stateful = badStyle, true
	default:
		if loaded {
			rowStyle, stateful = goodStyle, true
		}
	}
	noStyle := lipgloss.NewStyle()
	// styled renders a cell in the row's state colour when stateful, else in fb.
	styled := func(text string, fb lipgloss.Style) string {
		if stateful {
			return rowStyle.Render(text)
		}
		return fb.Render(text)
	}

	// favourite star column (always yellow so it reads as a marker)
	star := " "
	if dm.Tag == m.opts.AppConfig.Favourite {
		star = "★"
	}
	starCell := warnStyle.Render(center(star, colWidths[0]))

	modelText := cell(dm.Tag, colWidths[1])
	modelCell := styled(modelText, noStyle)
	if selected {
		modelCell = accentStyle.Render(modelText) // selection beats state colour
	}

	sizeCell := styled(cell(fmtGBShort(dm.Size), colWidths[2]), noStyle)
	paramsCell := styled(cell(paramsQuant(dm), colWidths[3]), noStyle)

	// live placement column (where the resident instance runs)
	onStr := "—"
	if loaded {
		onStr = shortProc(lm)
	}
	onCell := styled(cell(onStr, colWidths[4]), noStyle)

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
			editCells[k] = selectedStyle.Render(text) // the cell cursor
		case k == colNumGPU && cpuForced:
			// Flag CPU-only mode loudly so it isn't a silent surprise.
			editCells[k] = badStyle.Render(text)
		case stateful:
			editCells[k] = rowStyle.Render(text)
		case selected:
			editCells[k] = text
		default:
			editCells[k] = mutedStyle.Render(text)
		}
	}

	cells := []string{
		starCell, modelCell, sizeCell, paramsCell, onCell,
		editCells[0], editCells[1], editCells[2],
	}
	var out strings.Builder
	out.WriteString(bar())
	for _, c := range cells {
		out.WriteString(" " + c + bar())
	}
	return out.String()
}

// numGPUChoices is the fallback GPU-offload step list (auto, CPU, every 4 layers
// up to 96, then "all") used when the model's real layer count isn't known yet.
// Prefer (m Model).gpuChoices, which caps the steps at the model's actual layers.
var numGPUChoices = gpuChoicesUpTo(96)

// gpuChoicesUpTo builds the offload steps: auto, CPU (0), every 2 layers up to
// max, then "all" (99 = offload everything).
func gpuChoicesUpTo(max int) []int {
	c := []int{store.NumGPUAuto, 0}
	for n := 2; n < max; n += 2 {
		c = append(c, n)
	}
	return append(c, 99) // 99 = "all" layers
}

// gpuChoices returns the offload steps for a tag, capped at its real layer count
// (from `ollama show`) when known — e.g. a 27-layer model stops at 24 then
// "all", instead of offering up to 96. Falls back to numGPUChoices otherwise.
func (m Model) gpuChoices(tag string) []int {
	if d, ok := m.details[tag]; ok && d.BlockCount > 0 {
		return gpuChoicesUpTo(d.BlockCount)
	}
	return numGPUChoices
}

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

// modelMeta returns the best-known params (billions), quant, and on-disk size
// (bytes) for a tag.
func (m Model) modelMeta(tag string) (paramsB float64, quant string, sizeBytes int64) {
	cfg := m.ensureConfig(tag)
	paramsB, quant = cfg.ParamsBillions, cfg.Quant
	for _, dm := range m.disk {
		if dm.Tag == tag {
			sizeBytes = dm.Size
			if paramsB == 0 {
				paramsB = hw.ParseParamsBillions(dm.ParamSize)
			}
			if quant == "" {
				quant = dm.Quant
			}
		}
	}
	return paramsB, quant, sizeBytes
}

// reclaimableGB is the memory currently held by loaded models in the given pool
// ("gpu" or "ram") that would be freed when a new model loads — tuicode loads one
// model at a time, unloading any other first. Used so the fit preview measures a
// new model against the memory it would *actually* have, not just what's free now.
func (m Model) reclaimableGB(source string) float64 {
	var b int64
	for _, lm := range m.loaded {
		switch source {
		case "gpu":
			b += lm.SizeVRAM
		case "unified":
			b += lm.Size // one pool — the whole model is reclaimable
		default: // "ram"
			b += lm.Size - lm.SizeVRAM
		}
	}
	return float64(b) / gib
}

// usageFor projects a model's footprint at the given context, feeding in
// architecture hints (layer count, sliding-window) from `ollama show` when
// available so sliding-window models aren't wildly overestimated.
func (m Model) usageFor(tag string, ctx int) hw.Usage {
	pB, q, sz := m.modelMeta(tag)
	var layers, window int
	if d, ok := m.details[tag]; ok {
		layers, window = d.BlockCount, d.SlidingWindow
	}
	return hw.EstimateUsageArch(pB, q, ctx, sz, layers, window)
}

// ctxNote describes the context for the INFO line. For a default context it
// notes the ~4k Ollama loads at plus the model's max (from `ollama show`).
func (m Model) ctxNote(tag string, ctx int) string {
	if ctx > 0 {
		return contextShort(ctx)
	}
	s := "default ~4k"
	if d, ok := m.details[tag]; ok && d.ContextLength > 0 {
		s += " · max " + contextShort(d.ContextLength)
	}
	return s
}

// renderInfo renders the information zone for the selected model. It always
// emits exactly three lines so the layout below never shifts. While the model is
// loading it shows an animated progress bar instead of the estimate.
func (m Model) renderInfo() string {
	tag, ok := m.selectedDiskTag()
	if !ok {
		return mutedStyle.Render("INFO  no model selected") + "\n\n\n"
	}
	if m.pending[tag] == "load" {
		return m.renderLoadingInfo(tag)
	}

	cfg := m.ensureConfig(tag)
	usage := m.usageFor(tag, cfg.ContextLength)

	mem := m.detection.Authoritative()
	totalGB := float64(mem.Total) / gib
	freeGB := float64(mem.Free) / gib
	reserveGB := float64(m.detection.Reserve()) / gib
	src := strings.ToUpper(mem.Source)
	lm, actual, loaded := m.loadedFor(tag)
	// "Measured" only applies when the resident instance matches the current
	// settings. After a CTX/GPU change the serve tag differs, so we fall back to
	// the estimate for the *new* settings until the user reloads.
	serve := serveTag(tag, cfg, m.opts.DeviceMode)
	upToDate := loaded && actual == serve

	var b strings.Builder
	b.WriteString(sectionStyle.Render("INFO  ") + accentStyle.Render(tag) + m.capabilityNote(tag) + "\n")

	// --- estimated memory line (green while the CTX column is focused) ---
	estStyle := mutedStyle
	if m.dashCol == colContext {
		estStyle = goodStyle
	}
	switch {
	case upToDate && lm.Size > 0:
		// Loaded with the current settings → show the real footprint from
		// `ollama ps`, not the estimate (estimates are rough — sliding-window
		// KV, MoE, etc.).
		b.WriteString("  " + estStyle.Render(fmt.Sprintf(
			"mem       %.1fGB resident  (measured)", float64(lm.Size)/gib)) + "\n")
	case m.memorySettling():
		// Mid-load: VRAM is in flux, so show nothing projected (no green/red flash).
		b.WriteString("  " + mutedStyle.Render("mem       measuring… (loading)") + "\n")
	case !usage.Known:
		b.WriteString("  " + mutedStyle.Render("est. mem   unknown (params/quant not detected)") + "\n")
	default:
		est := fmt.Sprintf("est. mem  %.1fGB   weights %.1f + ctx %s ≈ %.1f",
			usage.TotalGB, usage.WeightsGB, m.ctxNote(tag, cfg.ContextLength), usage.KVGB)
		if d, ok := m.details[tag]; ok && d.SlidingWindow > 0 {
			est += " (sliding-window)"
		}
		if loaded {
			est += " · reload to apply"
		}
		// Loading a model first unloads any other resident model, so its VRAM is
		// reclaimable — count it as available when checking whether this one fits.
		reclaimGB := m.reclaimableGB(mem.Source)
		avail := freeGB + reclaimGB
		swap := ""
		if reclaimGB > 0.1 {
			swap = " after unload"
		}
		noLive := freeGB <= 0 && reclaimGB <= 0
		fit := ""
		switch {
		case m.detection.Unified:
			// Unified memory has two distinct limits. The hard one is the
			// Metal-addressable ceiling (total − reserve ≈ 70%): above it the model
			// genuinely can't be GPU-resident. The soft one is currently-free pages:
			// below the ceiling but above free, macOS reclaims/compresses to load it
			// — it works, just under pressure — so that's a ⚠, not a ✗. (The live
			// `available` figure already nets out the OS/apps, so we don't add the
			// reserve to the requirement here.)
			ceilGB := totalGB - reserveGB
			switch {
			case usage.TotalGB > ceilGB:
				fit = badStyle.Render(fmt.Sprintf("   ✗ exceeds usable %s (needs %.1f, cap %.1f)", src, usage.TotalGB, ceilGB))
			case noLive:
				// no live free figure, but within the ceiling — assume it loads.
			case usage.TotalGB <= avail:
				fit = goodStyle.Render(fmt.Sprintf("   ✓ fits (%.1f %s free%s)", avail, src, swap))
			default:
				fit = warnStyle.Render(fmt.Sprintf("   ⚠ tight — %.1f %s free, macOS reclaims to load", avail, src))
			}
		case noLive:
			// no live free figure; fall back to total
			if usage.TotalGB+reserveGB > totalGB {
				fit = warnStyle.Render("   may not fit")
			}
		case usage.TotalGB+reserveGB <= avail:
			fit = goodStyle.Render(fmt.Sprintf("   ✓ fits (%.1f %s free%s)", avail, src, swap))
		default:
			fit = badStyle.Render(fmt.Sprintf("   ✗ needs %.1f, only %.1f %s free%s", usage.TotalGB+reserveGB, avail, src, swap))
		}
		b.WriteString("  " + estStyle.Render(est) + fit + "\n")
	}

	// --- CPU/GPU split + VRAM line (green while the GPU column is focused) ---
	// Live placement when loaded; otherwise the benchmark reference for the tag.
	if m.memorySettling() {
		label := "split"
		if m.detection.Unified {
			label = "place"
		}
		b.WriteString("  " + mutedStyle.Render(label+"     measuring… (loading)") + "\n")
		b.WriteString(m.infoParamsLine(cfg))
		return b.String()
	}
	splitStyle := mutedStyle
	if m.dashCol == colNumGPU {
		splitStyle = goodStyle
	}
	// Live placement only when the resident instance matches the current
	// settings; after a change, predict the new split until reload.
	b.WriteString("  " + splitStyle.Render(m.splitLine(tag, lm, upToDate)) + "\n")

	b.WriteString(m.infoParamsLine(cfg))
	return b.String()
}

// infoParamsLine renders the sampler-params line (green while the PRESET column
// is focused). No trailing newline — it's the last line of the INFO zone.
func (m Model) infoParamsLine(cfg store.ModelConfig) string {
	presetName := store.Presets()[matchPreset(cfg.Parameters)].Name
	params := fmt.Sprintf("params     %s · temp %.2f · top_p %.2f · top_k %d",
		presetName, cfg.Parameters.Temperature, cfg.Parameters.TopP, cfg.Parameters.TopK)
	if m.dashCol == colPreset {
		return "  " + goodStyle.Render(params)
	}
	return "  " + mutedStyle.Render(params)
}

// splitLine renders the CPU/GPU split + VRAM line for the INFO zone: the live
// placement when the model is loaded, the benchmark reference when known, or the
// configured offload setting otherwise.
func (m Model) splitLine(tag string, lm server.LoadedModel, loaded bool) string {
	if loaded {
		if m.detection.Unified {
			// Unified memory: no VRAM-vs-RAM split. Show measured placement only.
			return fmt.Sprintf("place     %s · %.1fGB  (live)",
				lm.Processor(), float64(lm.SizeVRAM)/gib)
		}
		return fmt.Sprintf("split     %s · %.1fGB VRAM  (live)",
			lm.Processor(), float64(lm.SizeVRAM)/gib)
	}
	// Before loading, predict the split from the chosen GPU-offload setting and
	// the model's layer count (when known).
	if pred, ok := m.predictSplit(tag); ok {
		return pred
	}
	if r, ok := recFor(m.opts.Recommended, tag); ok {
		ref := "ref"
		if g := m.opts.Recommended.RefGPUGB; g > 0 {
			ref = fmt.Sprintf("ref %.0fGB GPU", g)
		}
		return fmt.Sprintf("split     %s · ~%gGB  (%s)", splitLabel(r.GPUPercent), r.MemGB, ref)
	}
	cfg := m.ensureConfig(tag)
	return "split     " + numGPULabel(cfg, m.opts.DeviceMode) + " offload — load to measure"
}

// predictSplit estimates the CPU/GPU split and VRAM use for the selected model
// *before* loading, from the model's transformer-layer count (`ollama show`),
// its estimated footprint, and the chosen GPU-offload setting. It assumes
// roughly equal-sized layers, so it's an estimate (labelled "est"), but it lets
// you dial the `GPU` column in and see the likely outcome live. ok=false when we
// lack the layer count or a usable footprint estimate.
func (m Model) predictSplit(tag string) (string, bool) {
	cfg := m.ensureConfig(tag)
	u := m.usageFor(tag, cfg.ContextLength)
	if !u.Known || u.TotalGB <= 0 {
		return "", false
	}
	if m.detection.Unified {
		// Unified memory is one pool, so there's no VRAM-overflow CPU/GPU split —
		// the whole model runs on the GPU (Metal). The only ceiling is Metal's
		// ~70% wired limit; above it, layers fall back to the CPU (slower, same
		// memory) unless iogpu.wired_limit_mb is raised.
		if ceil := gpuCeilGB(m.detection); ceil > 0 && u.TotalGB > ceil {
			return fmt.Sprintf("place     GPU (Metal) · ~%.1fGB · past ~70%% wired limit (slower)", u.TotalGB), true
		}
		return fmt.Sprintf("place     GPU (Metal) · ~%.1fGB  (unified — no CPU split)", u.TotalGB), true
	}
	d, ok := m.details[tag]
	if !ok || d.BlockCount <= 0 {
		return "", false
	}
	layers := d.BlockCount
	n := effectiveNumGPU(cfg, m.opts.DeviceMode)
	auto := n < 0

	var gpuLayers int
	if auto {
		// Ollama fills VRAM with as many layers as fit; estimate how many.
		perLayer := u.TotalGB / float64(layers)
		if perLayer > 0 {
			gpuLayers = int(m.freeVRAMGB() / perLayer)
		}
	} else {
		gpuLayers = n
	}
	gpuLayers = clamp(gpuLayers, 0, layers)

	frac := float64(gpuLayers) / float64(layers)
	predVRAM := frac * u.TotalGB
	how := "est"
	if auto {
		how = "est, auto"
	}
	return fmt.Sprintf("split     ~%s · ~%.1fGB VRAM  (%s, %d/%d layers)",
		splitLabel(int(frac*100+0.5)), predVRAM, how, gpuLayers, layers), true
}

// freeVRAMGB is the GPU memory available for layer offload (0 = no GPU).
func (m Model) freeVRAMGB() float64 {
	g := m.detection.GPU
	if g == nil {
		return 0
	}
	if m.detection.Unified {
		// Apple Silicon: Ollama offloads to the GPU up to Metal's wired limit (a
		// fraction of *total* RAM ≈ total − reserve), and macOS reclaims pages to
		// make room — it isn't bounded by currently-free memory the way a discrete
		// card's VRAM is. Using live-free here wrongly predicts a CPU-heavy split.
		budget := float64(g.Total - m.detection.Reserve())
		if budget < 0 {
			budget = 0
		}
		return budget / gib
	}
	avail := g.Free
	if avail <= 0 {
		avail = g.Total
	}
	avail -= m.detection.Reserve() // leave headroom
	if avail < 0 {
		avail = 0
	}
	return float64(avail) / gib
}

// capabilityNote appends a compact capability summary to the INFO header, with a
// loud warning when the model can't tool-call (OpenCode requires it). Empty
// until `ollama show` has been fetched for the tag.
func (m Model) capabilityNote(tag string) string {
	d, ok := m.details[tag]
	if !ok || len(d.Capabilities) == 0 {
		return ""
	}
	if !d.HasCapability("tools") {
		return "   " + badStyle.Render("⚠ no tools — OpenCode needs tool-calling")
	}
	extras := make([]string, 0, 3)
	for _, c := range []string{"vision", "audio", "thinking"} {
		if d.HasCapability(c) {
			extras = append(extras, c)
		}
	}
	note := "   " + goodStyle.Render("✓ tools")
	if len(extras) > 0 {
		note += mutedStyle.Render(" · " + strings.Join(extras, " · "))
	}
	return note
}

// splitLabel renders a GPU-share percentage as Ollama's processor wording.
func splitLabel(gpuPct int) string {
	switch {
	case gpuPct >= 100:
		return "100% GPU"
	case gpuPct <= 0:
		return "100% CPU"
	default:
		return fmt.Sprintf("%d%%/%d%% CPU/GPU", 100-gpuPct, gpuPct)
	}
}

// renderLoadingInfo renders the animated loading bar for a model being loaded.
func (m Model) renderLoadingInfo(tag string) string {
	// Deliberately calm and static — no animated bar/percentage to flicker.
	// Same 4-line height as the estimate view so nothing below shifts.
	var b strings.Builder
	b.WriteString(sectionStyle.Render("INFO  ") + accentStyle.Render(tag) + "\n")
	b.WriteString("  " + warnStyle.Render("LOADING…") + mutedStyle.Render("   warming into VRAM") + "\n")
	b.WriteString("  " + mutedStyle.Render("(one model loads at a time — any other is unloaded first)") + "\n")
	b.WriteString("  " + mutedStyle.Render("memory + split are measured once it's resident"))
	return b.String()
}

// renderSession renders the OpenCode session zone: a header line with the
// selected model and its load state, the model's last session + continue
// command, and the most-recent session across all models. Always five lines so
// the layout below stays stable.
func (m Model) renderSession() string {
	const label = "  %-10s" // left-aligned label gutter
	blank := "\n"

	tag, ok := m.selectedDiskTag()
	if !ok {
		return sectionStyle.Render("SESSION") + "  " + mutedStyle.Render("no model selected") +
			strings.Repeat("\n", 4)
	}

	_, _, loaded := m.loadedFor(tag)
	state := mutedStyle.Render("○ not loaded")
	if loaded {
		state = goodStyle.Render("● loaded")
	}

	var b strings.Builder
	b.WriteString(sectionStyle.Render("SESSION") + "  " + accentStyle.Render(tag) + "   " + state + "\n")

	// This model's last session + continue command.
	if ls := m.ensureConfig(tag).LastSession; ls != nil && ls.ID != "" {
		b.WriteString(mutedStyle.Render(fmt.Sprintf(label, "last")) +
			helpStyle.Render("\""+clip(sessionTitle(ls), 52)+"\"") + "\n")
		b.WriteString(mutedStyle.Render(fmt.Sprintf(label, "continue")) +
			keyStyle.Render("[c]") + " " + mutedStyle.Render("opencode -s "+ls.ID) + "\n")
	} else {
		b.WriteString(mutedStyle.Render(fmt.Sprintf(label, "last")) +
			mutedStyle.Render("none yet — press ") + keyStyle.Render("[⏎]") +
			mutedStyle.Render(" to start a session") + "\n")
		b.WriteString(blank)
	}

	// Most-recent session across all models.
	if gBase, gRef := m.mostRecentSession(); gRef != nil {
		desc := gBase
		if t := sessionTitle(gRef); t != "" {
			desc += " · \"" + clip(t, 34) + "\""
		}
		b.WriteString(mutedStyle.Render(fmt.Sprintf(label, "global")) +
			keyStyle.Render("[C]") + " " + helpStyle.Render(desc))
	} else {
		b.WriteString(mutedStyle.Render(fmt.Sprintf(label, "global")) +
			mutedStyle.Render("no sessions recorded yet"))
	}
	return b.String()
}

// sessionTitle returns a display title for a session (falling back to its id).
func sessionTitle(s *store.SessionRef) string {
	if s.Title != "" {
		return s.Title
	}
	return s.ID
}

// clip truncates s to at most w runes, appending … when shortened (no padding).
func clip(s string, w int) string {
	r := []rune(s)
	if len(r) <= w {
		return s
	}
	if w <= 1 {
		return string(r[:w])
	}
	return string(r[:w-1]) + "…"
}

// memBar renders a labeled memory bar: white = already used, accent ▒ = the
// projected footprint of the selected (unloaded) model, dim = free. projGB == 0
// draws just used/free.
// memBar renders a labeled memory bar. usedGB is total memory in use; modelGB is
// the share attributable to the loaded model(s), drawn green; the rest of the
// used space is neutral white. projGB previews a not-yet-loaded model.
func memBar(label string, usedGB, modelGB, totalGB, projGB float64, w int) string {
	if totalGB <= 0 {
		return mutedStyle.Render(label + "  (unknown)")
	}
	if modelGB > usedGB {
		modelGB = usedGB
	}
	baseGB := usedGB - modelGB
	bar := stackedBar(baseGB/totalGB, modelGB/totalGB, projGB/totalGB, barWidth(w))
	s := fmt.Sprintf("%s  %s  %s / %s GB", label, bar, fmtGBf(usedGB), fmtGBf(totalGB))
	if projGB > 0 {
		s += mutedStyle.Render(fmt.Sprintf("   ▒ +%.1f to load", projGB))
	}
	return s
}

// renderRamBar renders the system-RAM usage bar (MemAvailable vs MemTotal).
func (m Model) renderRamBar(w int, projGB float64) string {
	ram := m.detection.RAM
	usedGB := float64(ram.Total-ram.Free) / gib
	// Model's RAM share = the part of resident models NOT on the GPU (CPU spill).
	modelGB := m.reclaimableGB("ram")
	if projGB > 0 {
		// Previewing a swap-in: the resident model unloads first, freeing its RAM.
		usedGB -= modelGB
		if usedGB < 0 {
			usedGB = 0
		}
		modelGB = 0
	}
	return memBar("RAM ", usedGB, modelGB, float64(ram.Total)/gib, projGB, w)
}

// renderVramBar renders the GPU VRAM usage bar from nvidia-smi (or summed
// footprints when free VRAM is unavailable).
func (m Model) renderVramBar(w int, projGB float64) string {
	g := m.detection.GPU
	if g == nil {
		return mutedStyle.Render("VRAM  (no GPU)")
	}
	modelGB := m.reclaimableGB("gpu")
	usedGB := modelGB
	if g.Free > 0 {
		// Live free figure includes non-model usage (display, other procs).
		usedGB = float64(g.Total-g.Free) / gib
	}
	if projGB > 0 {
		// Previewing a swap-in: the resident model unloads first, freeing its VRAM,
		// so draw the projection into that space rather than stacked on top of it.
		usedGB -= modelGB
		if usedGB < 0 {
			usedGB = 0
		}
		modelGB = 0
	}
	return memBar("VRAM", usedGB, modelGB, g.TotalGB(), projGB, w)
}

// renderUnifiedBar renders the single unified-memory bar for Apple Silicon,
// where the GPU and CPU share one physical pool. The live free figure (from
// vm_stat) includes the OS and other apps, so it reflects real headroom.
func (m Model) renderUnifiedBar(w int, projGB float64) string {
	g := m.detection.GPU
	if g == nil {
		return mutedStyle.Render("Mem  (unknown)")
	}
	modelGB := m.reclaimableGB("unified")
	usedGB := modelGB
	if g.Free > 0 {
		usedGB = float64(g.Total-g.Free) / gib
	}
	if projGB > 0 {
		// Previewing a swap-in: the resident model unloads first, freeing its
		// share, so draw the projection into that space rather than stacked on it.
		usedGB -= modelGB
		if usedGB < 0 {
			usedGB = 0
		}
		modelGB = 0
	}
	return memBar("Mem ", usedGB, modelGB, g.TotalGB(), projGB, w)
}

// selectedProjectionGB is the estimated footprint of the selected model when it
// is not yet loaded (0 when loaded or unknown) — used to preview the load on the
// memory bar.
func (m Model) selectedProjectionGB() float64 {
	tag, ok := m.selectedDiskTag()
	if !ok {
		return 0
	}
	// No preview overlay once it's loaded, or while memory readings are settling
	// (a load in flight consumes VRAM, so free memory is in flux and a projection
	// for another model would briefly read as an overflow — a red flash).
	if m.memorySettling() {
		return 0
	}
	if _, _, loaded := m.loadedFor(tag); loaded {
		return 0
	}
	u := m.usageFor(tag, m.ensureConfig(tag).ContextLength)
	if !u.Known {
		return 0
	}
	return u.TotalGB
}

func barWidth(w int) int {
	bw := w - 22
	if bw < 10 {
		bw = 10
	}
	return bw
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

// shortProc renders the GPU/CPU split for the GPU/CPU column: "100% GPU",
// "100% CPU", or a "50%/50%" (GPU/CPU) share when the model is split.
func shortProc(lm server.LoadedModel) string {
	g := lm.GPUPercent()
	switch {
	case g >= 100:
		return "100% GPU"
	case g <= 0:
		return "100% CPU"
	default:
		return fmt.Sprintf("%d%%/%d%%", g, 100-g)
	}
}
