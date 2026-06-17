package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"tuicode/internal/hw"
	"tuicode/internal/server"
)

func runes(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

func TestManualAddTypeAndPull(t *testing.T) {
	be := &fakeBackend{reachable: true}
	m := New(testOpts(t, be, true))
	m.screen = screenOnboarding
	m.onboard.tab = tabManual

	// Typing must not trigger 'q' quit / 'n' skip — it builds the tag.
	for _, s := range []string{"qwen3.6:35b-a3b"} {
		model, _ := m.updateOnboardingKey(runes(s))
		m = model.(Model)
	}
	if m.onboard.manual != "qwen3.6:35b-a3b" {
		t.Fatalf("manual = %q", m.onboard.manual)
	}
	if m.screen != screenOnboarding {
		t.Fatalf("typing changed screen to %v", m.screen)
	}
	// Backspace edits.
	model, _ := m.updateOnboardingKey(tea.KeyMsg{Type: tea.KeyBackspace})
	m = model.(Model)
	if m.onboard.manual != "qwen3.6:35b-a3" {
		t.Fatalf("after backspace = %q", m.onboard.manual)
	}
	// Enter pulls the typed tag.
	model, cmd := m.updateOnboardingKey(tea.KeyMsg{Type: tea.KeyEnter})
	mm := model.(Model)
	if !mm.onboard.pulling || mm.onboard.pullTag != "qwen3.6:35b-a3" {
		t.Fatalf("expected pulling %q, got pulling=%v tag=%q", "qwen3.6:35b-a3", mm.onboard.pulling, mm.onboard.pullTag)
	}
	drain(cmd)
}

func TestManualTabSwitchCyclesThree(t *testing.T) {
	m := New(testOpts(t, &fakeBackend{reachable: true}, true))
	m.screen = screenOnboarding
	for i, want := range []int{tabRecommended, tabManual, tabTrending} {
		model, _ := m.updateOnboardingKey(tea.KeyMsg{Type: tea.KeyRight})
		m = model.(Model)
		if m.onboard.tab != want {
			t.Errorf("step %d: tab = %d, want %d", i, m.onboard.tab, want)
		}
	}
}

func TestPredictSplitFromLayers(t *testing.T) {
	be := &fakeBackend{reachable: true,
		disk: []server.DiskModel{{Tag: "m:14b", Size: 8 * gib, ParamSize: "14B", Quant: "Q4_K_M"}},
	}
	m := New(testOpts(t, be, true))
	m.disk = be.disk
	m.detection = hw.Detection{Mode: hw.Auto, RAM: hw.Memory{Total: 64 * gib},
		GPU: &hw.Memory{Total: 16 * gib, Free: 16 * gib}, HasGPU: true}
	m.details["m:14b"] = server.ModelDetails{Tag: "m:14b", BlockCount: 40}
	// Force 20 of 40 layers onto the GPU → ~50% split.
	cfg := m.ensureConfig("m:14b")
	cfg.NumGPU = 20
	m.configs["m:14b"] = cfg

	line, ok := m.predictSplit("m:14b")
	if !ok {
		t.Fatal("predictSplit returned ok=false")
	}
	if !strings.Contains(line, "20/40 layers") {
		t.Errorf("expected 20/40 layers in %q", line)
	}
	if !strings.Contains(line, "CPU/GPU") {
		t.Errorf("expected a split percentage in %q", line)
	}
}

func TestGPUChoicesCapsAtLayerCount(t *testing.T) {
	m := New(testOpts(t, &fakeBackend{reachable: true}, true))
	// Unknown layer count → fallback list goes up to 96 then "all" (99).
	if c := m.gpuChoices("unknown:1b"); c[len(c)-1] != 99 {
		t.Errorf("fallback should end in 99 (all), got %v", c)
	} else if c[len(c)-2] < 80 {
		t.Errorf("fallback should reach high layer counts, got %v", c)
	}
	// Known 27-layer model → steps stop at 24, then "all" (99).
	m.details["small:4b"] = server.ModelDetails{Tag: "small:4b", BlockCount: 27}
	c := m.gpuChoices("small:4b")
	if c[len(c)-1] != 99 {
		t.Fatalf("expected trailing 99 (all), got %v", c)
	}
	maxStep := c[len(c)-2]
	if maxStep != 24 {
		t.Errorf("27-layer model should step to 24 then all, got max step %d (%v)", maxStep, c)
	}
	for _, n := range c {
		if n > 27 && n != 99 {
			t.Errorf("offered %d layers > 27-layer model", n)
		}
	}
}

func TestInfoSwitchesOffMeasuredWhenContextChanges(t *testing.T) {
	be := &fakeBackend{reachable: true,
		disk:   []server.DiskModel{{Tag: "a:1b", Size: gib, ParamSize: "1B", Quant: "Q4_K_M"}},
		loaded: []server.LoadedModel{loadedModel("a:1b")}, // resident as base (default ctx)
	}
	m := New(testOpts(t, be, true))
	m.width, m.height = 100, 40
	m.disk = be.disk
	m.loaded = be.loaded
	m.screen = screenDashboard
	m.dashCursor = 0
	m.detection = hw.Detection{Mode: hw.Auto, RAM: hw.Memory{Total: 64 * gib},
		GPU: &hw.Memory{Source: "gpu", Total: 16 * gib, Free: 12 * gib}, HasGPU: true}

	// Loaded with matching (default) settings → INFO shows the measured size.
	if info := m.renderInfo(); !strings.Contains(info, "measured") {
		t.Fatalf("expected 'measured' for an up-to-date loaded model:\n%s", info)
	}

	// Raise the context: the resident instance no longer matches, so INFO must
	// fall back to the estimate for the NEW context (not the stale measured value).
	cfg := m.ensureConfig("a:1b")
	cfg.ContextLength = 32768
	m.configs["a:1b"] = cfg
	info := m.renderInfo()
	if strings.Contains(info, "measured") {
		t.Errorf("after a context change INFO should drop 'measured':\n%s", info)
	}
	if !strings.Contains(info, "est. mem") || !strings.Contains(info, "reload to apply") {
		t.Errorf("expected estimate + 'reload to apply' after change:\n%s", info)
	}
}
