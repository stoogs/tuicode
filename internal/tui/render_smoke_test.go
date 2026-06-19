package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"tuicode/internal/hw"
	"tuicode/internal/server"
)

func TestSettingsViewFitsAndGroups(t *testing.T) {
	be := &fakeBackend{reachable: true}
	m := New(testOpts(t, be, true))
	m.width, m.height = 100, 40
	m.screen = screenSettings

	// Device mode is the top editable row — it must always render (regression:
	// a too-tall settings view scrolled the top off-screen).
	out := m.viewSettings()
	if !strings.Contains(out, "Device mode") {
		t.Fatal("Device mode row missing from settings view")
	}
	// Keep the screen short so it doesn't overflow typical terminals. (The tall
	// daemon-command block only appears on the flash/KV rows.)
	if n := strings.Count(out, "\n"); n > 30 {
		t.Errorf("settings view too tall: %d lines", n)
	}

	// Compaction sub-rows are indented under "Manage compaction".
	if !strings.Contains(out, "    Auto-compact") || !strings.Contains(out, "    Prune tool outputs") {
		t.Error("compaction sub-rows should be indented under Manage compaction")
	}

	// With management off, the sub-rows are skipped by cursor navigation.
	m.opts.AppConfig.Compaction.Manage = false
	m.settingsCursor = setManageCompaction
	if next := m.stepSetting(+1); next == setCompactAuto {
		t.Errorf("cursor should skip disabled compaction sub-rows, landed on %d", next)
	}
	// With management on, the next row IS the first sub-row.
	m.opts.AppConfig.Compaction.Manage = true
	if next := m.stepSetting(+1); next != setCompactAuto {
		t.Errorf("with management on, next row should be Auto-compact, got %d", next)
	}
}

func TestRowStateAndLoadingView(t *testing.T) {
	// Force a color profile so style differences are observable in the test env,
	// then restore it so this can't leak into other tests.
	defer lipgloss.SetColorProfile(lipgloss.ColorProfile())
	lipgloss.SetColorProfile(2) // 16-color ANSI

	be := &fakeBackend{reachable: true,
		disk:   []server.DiskModel{{Tag: "a:1b", Size: gib, ParamSize: "1B", Quant: "Q4_K_M"}},
		loaded: []server.LoadedModel{loadedModel("a:1b")},
	}
	m := New(testOpts(t, be, true))
	m.width, m.height = 100, 40
	m.disk = be.disk
	m.loaded = be.loaded
	m.screen = screenDashboard
	m.detection = hw.Detection{Mode: hw.Auto, RAM: hw.Memory{Total: 64 * gib},
		GPU: &hw.Memory{Source: "gpu", Total: 16 * gib, Free: 6 * gib}, HasGPU: true}

	// No ST column header anymore.
	for _, h := range colHeaders {
		if h == "ST" {
			t.Fatal("ST column should be gone")
		}
	}

	// A loaded row is tinted green (color 10); a stopped row is not.
	loadedRow := m.renderRow(0, be.disk[0])
	if !strings.Contains(loadedRow, "[92m") {
		t.Errorf("loaded row should be green-tinted:\n%q", loadedRow)
	}
	m.loaded = nil
	stoppedRow := m.renderRow(0, be.disk[0])
	if strings.Contains(stoppedRow, "[92m") {
		t.Errorf("stopped row should not be green:\n%q", stoppedRow)
	}

	// Loading view is the calm static text (no animated bar).
	m.pending["a:1b"] = "load"
	if info := m.renderInfo(); !strings.Contains(info, "LOADING…") {
		t.Errorf("expected static LOADING…, got:\n%s", info)
	}
}

func TestNoProjectionOverlayWhileLoading(t *testing.T) {
	be := &fakeBackend{reachable: true,
		disk: []server.DiskModel{
			{Tag: "a:1b", Size: gib, ParamSize: "1B", Quant: "Q4_K_M"},
			{Tag: "big:14b", Size: 9 * gib, ParamSize: "14B", Quant: "Q4_K_M"},
		},
	}
	m := New(testOpts(t, be, true))
	m.disk = be.disk
	m.dashCursor = 0 // selected = a:1b
	m.detection = hw.Detection{Mode: hw.Auto, RAM: hw.Memory{Total: 64 * gib},
		GPU: &hw.Memory{Source: "gpu", Total: 16 * gib, Free: 16 * gib}, HasGPU: true}

	// Not loaded yet → a preview projection is shown.
	if m.selectedProjectionGB() <= 0 {
		t.Fatal("expected a projection preview before loading")
	}
	// A DIFFERENT model loading (the reported bug) must also suppress the selected
	// model's projection — free VRAM is dropping, so the overlay would flash red.
	m.pending["big:14b"] = "load"
	if proj := m.selectedProjectionGB(); proj != 0 {
		t.Errorf("projection should be 0 while another model loads, got %.2f", proj)
	}
	// And the just-loaded settling frame (loadedPrompt) likewise suppresses it.
	delete(m.pending, "big:14b")
	m.loadedPrompt = "big:14b"
	if proj := m.selectedProjectionGB(); proj != 0 {
		t.Errorf("projection should be 0 during the loaded-prompt settle frame, got %.2f", proj)
	}
}

func TestVramBarGreensOnlyModelShare(t *testing.T) {
	defer lipgloss.SetColorProfile(lipgloss.ColorProfile())
	lipgloss.SetColorProfile(2)

	be := &fakeBackend{reachable: true}
	m := New(testOpts(t, be, true))
	// 6GB VRAM used by the model, but 10GB total in use (4GB by other things).
	m.loaded = []server.LoadedModel{{Tag: "a:1b", Size: 6 * gib, SizeVRAM: 6 * gib}}
	m.detection = hw.Detection{Mode: hw.Auto, RAM: hw.Memory{Total: 64 * gib},
		GPU: &hw.Memory{Source: "gpu", Total: 16 * gib, Free: 6 * gib}, HasGPU: true} // 10GB used

	bar := m.renderVramBar(60, 0)
	if !strings.Contains(bar, "[92m") { // green = model's 6GB
		t.Errorf("expected a green model segment:\n%q", bar)
	}
	if !strings.Contains(bar, "[97m") && !strings.Contains(bar, "[37m") { // white = the other 4GB used
		t.Errorf("expected a neutral (white) segment for non-model usage:\n%q", bar)
	}
}

func TestFitCountsReclaimableVRAM(t *testing.T) {
	be := &fakeBackend{reachable: true,
		disk: []server.DiskModel{{Tag: "new:14b", Size: 8 * gib, ParamSize: "14B", Quant: "Q4_K_M"}},
	}
	m := New(testOpts(t, be, true))
	m.width, m.height = 100, 40
	m.disk = be.disk
	m.dashCursor = 0 // selected = new:14b (not loaded)
	// Another model holds 8GB of the 16GB GPU, leaving only 8GB free.
	m.loaded = []server.LoadedModel{{Tag: "other:7b", Size: 8 * gib, SizeVRAM: 8 * gib}}
	m.detection = hw.Detection{Mode: hw.Auto, RAM: hw.Memory{Total: 64 * gib},
		GPU: &hw.Memory{Source: "gpu", Total: 16 * gib, Free: 8 * gib}, HasGPU: true}

	info := m.renderInfo()
	// new:14b (~8.5GB) wouldn't fit in the 8GB free now, but loading it unloads
	// the other model first — so it DOES fit, and the verdict says so.
	if !strings.Contains(info, "fits") || !strings.Contains(info, "after unload") {
		t.Errorf("expected '✓ fits … after unload' (reclaimable VRAM counted):\n%s", info)
	}
	if strings.Contains(info, "✗") {
		t.Errorf("should not report ✗ when it fits after unloading the current model:\n%s", info)
	}
}

func TestNoProjectedTextWhileSettling(t *testing.T) {
	be := &fakeBackend{reachable: true,
		disk: []server.DiskModel{
			{Tag: "a:1b", Size: gib, ParamSize: "1B", Quant: "Q4_K_M"},
			{Tag: "big:14b", Size: 9 * gib, ParamSize: "14B", Quant: "Q4_K_M"},
		},
	}
	m := New(testOpts(t, be, true))
	m.width, m.height = 100, 40
	m.disk = be.disk
	m.dashCursor = 0       // a:1b selected
	m.dashCol = colContext // would normally green the estimate line
	m.detection = hw.Detection{Mode: hw.Auto, RAM: hw.Memory{Total: 64 * gib},
		GPU: &hw.Memory{Source: "gpu", Total: 16 * gib, Free: 4 * gib}, HasGPU: true}

	// A different model loading → the selected model's INFO must not show any
	// estimate/fit/split projection (the source of the green/red flash).
	m.pending["big:14b"] = "load"
	info := m.renderInfo()
	if !strings.Contains(info, "measuring…") {
		t.Errorf("expected 'measuring…' placeholders while loading:\n%s", info)
	}
	for _, leak := range []string{"est. mem", "fits", "✗", "ref ", "live"} {
		if strings.Contains(info, leak) {
			t.Errorf("projected text %q leaked while settling:\n%s", leak, info)
		}
	}
}

func TestHyperlinkOSC8(t *testing.T) {
	got := hyperlink("https://ollama.com/library", "lib")
	want := "\x1b]8;;https://ollama.com/library\x1b\\lib\x1b]8;;\x1b\\"
	if got != want {
		t.Errorf("hyperlink = %q, want %q", got, want)
	}
}
