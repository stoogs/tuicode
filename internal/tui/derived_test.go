package tui

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"tuicode/internal/hw"
	"tuicode/internal/server"
	"tuicode/internal/store"
)

func TestServeTagDerivation(t *testing.T) {
	base := "deepseek-r1:14b"

	// All defaults → no derived model.
	def := store.DefaultModelConfig(base, store.DefaultResidency())
	if got := serveTag(base, def, hw.Auto); got != base {
		t.Errorf("default serveTag = %q, want base", got)
	}

	// Context set → derived.
	ctxCfg := def
	ctxCfg.ContextLength = 8192
	got := serveTag(base, ctxCfg, hw.Auto)
	if got == base || !isDerived(got) {
		t.Errorf("ctx serveTag = %q, want a derived tag", got)
	}

	// cpu-only forces a derived model with num_gpu 0 baked in.
	if got := serveTag(base, def, hw.CPUOnly); !isDerived(got) {
		t.Errorf("cpu-only serveTag = %q, want derived", got)
	}
	if p := derivedParams(def, hw.CPUOnly); p["num_gpu"] != 0 {
		t.Errorf("cpu-only derivedParams num_gpu = %v, want 0", p["num_gpu"])
	}
}

func TestLoadCreatesDerivedForContext(t *testing.T) {
	be := &fakeBackend{reachable: true,
		disk: []server.DiskModel{{Tag: "deepseek-r1:14b", Size: 9 * gib, ParamSize: "14B", Quant: "Q4_K_M"}},
	}
	m := New(testOpts(t, be, true))
	m.disk = be.disk
	m.screen = screenDashboard
	m.dashCursor = 0

	// Set context to 8192 via the CTX column, then load.
	m.dashCol = colContext
	for cfg := m.ensureConfig("deepseek-r1:14b"); cfg.ContextLength != 8192; cfg = m.ensureConfig("deepseek-r1:14b") {
		model, _ := m.updateDashboard(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(".")})
		m = model.(Model)
	}
	_, cmd := m.doLoad("deepseek-r1:14b")
	drain(cmd)

	if len(be.createCalls) != 1 {
		t.Fatalf("expected 1 create (derived model), got %d", len(be.createCalls))
	}
	cc := be.createCalls[0]
	if cc.from != "deepseek-r1:14b" {
		t.Errorf("derived from = %q", cc.from)
	}
	if cc.params["num_ctx"] != 8192 {
		t.Errorf("derived num_ctx = %v, want 8192", cc.params["num_ctx"])
	}
	if len(be.loadCalls) != 1 || be.loadCalls[0].tag != cc.name {
		t.Errorf("should load the derived tag %q, got %v", cc.name, be.loadCalls)
	}
}

func TestCPUOnlyBakesNumGPUZero(t *testing.T) {
	be := &fakeBackend{reachable: true,
		disk: []server.DiskModel{{Tag: "llama3.2:1b", Size: gib}},
	}
	opts := testOpts(t, be, true)
	opts.DeviceMode = hw.CPUOnly
	m := New(opts)
	m.disk = be.disk
	m.screen = screenDashboard

	_, cmd := m.doLoad("llama3.2:1b")
	drain(cmd)
	if len(be.createCalls) != 1 || be.createCalls[0].params["num_gpu"] != 0 {
		t.Errorf("cpu-only load should create a derived model with num_gpu=0, got %v", be.createCalls)
	}
}

func TestReloadOnContextChangeBeforeLaunch(t *testing.T) {
	// Model loaded as the base (default ctx). User raises ctx, then opens it →
	// must reload (create+load the derived) rather than exec immediately.
	be := &fakeBackend{reachable: true,
		disk:   []server.DiskModel{{Tag: "a:1b", Size: gib, ParamSize: "1B", Quant: "Q4_K_M"}},
		loaded: []server.LoadedModel{loadedModel("a:1b")},
	}
	m := New(testOpts(t, be, true))
	m.disk = be.disk
	m.loaded = be.loaded
	m.screen = screenDashboard
	m.dashCol = colContext
	model, _ := m.updateDashboard(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(".")}) // ctx → 8192
	m = model.(Model)

	model2, cmd := m.updateDashboard(tea.KeyMsg{Type: tea.KeyEnter}) // open
	mm := model2.(Model)
	if mm.launchAfter != "a:1b" {
		t.Errorf("expected launchAfter set for reload-then-launch, got %q", mm.launchAfter)
	}
	if mm.pending["a:1b"] != "load" {
		t.Errorf("expected a reload (pending load), got %q", mm.pending["a:1b"])
	}
	drain(cmd)
	if len(be.createCalls) != 1 {
		t.Errorf("expected derived create on reload, got %d", len(be.createCalls))
	}
}

func TestDeleteConfirmFlow(t *testing.T) {
	be := &fakeBackend{reachable: true, disk: []server.DiskModel{{Tag: "a:1b", Size: gib}}}
	m := New(testOpts(t, be, true))
	m.disk = be.disk
	m.screen = screenDashboard

	// Backspace opens the confirm; default is no.
	model, _ := m.updateDashboard(tea.KeyMsg{Type: tea.KeyBackspace})
	m = model.(Model)
	if m.confirm != "a:1b" {
		t.Fatalf("expected confirm set, got %q", m.confirm)
	}
	// 'n' cancels, no delete.
	model, _ = m.updateDashboard(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	m = model.(Model)
	if m.confirm != "" || len(be.deleteCalls) != 0 {
		t.Errorf("'n' should cancel; confirm=%q deletes=%v", m.confirm, be.deleteCalls)
	}
	// Re-open and confirm with 'y'.
	model, _ = m.updateDashboard(tea.KeyMsg{Type: tea.KeyBackspace})
	m = model.(Model)
	_, cmd := m.updateDashboard(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	drain(cmd)
	if len(be.deleteCalls) != 1 || be.deleteCalls[0] != "a:1b" {
		t.Errorf("'y' should delete; calls=%v", be.deleteCalls)
	}
}

// Regression: a model with a non-default context is resident under its derived
// tag, so Enter/o must recognize it as loaded (via loadedFor) and open OpenCode
// rather than try to reload it.
func TestEnterOpensModelLoadedAsDerived(t *testing.T) {
	base := "a:1b"
	be := &fakeBackend{reachable: true,
		disk:   []server.DiskModel{{Tag: base, Size: gib, ParamSize: "1B", Quant: "Q4_K_M"}},
		loaded: []server.LoadedModel{{Tag: "tuicode/a-1b:c8192ga", Size: gib, SizeVRAM: gib, Context: 8192}},
	}
	opts := testOpts(t, be, true)
	m := New(opts)
	m.disk = be.disk
	m.loaded = be.loaded
	m.screen = screenDashboard
	cfg := m.ensureConfig(base)
	cfg.ContextLength = 8192
	m.configs[base] = cfg

	// sanity: the derived serve tag matches what's resident
	if serveTag(base, cfg, hw.Auto) != "tuicode/a-1b:c8192ga" {
		t.Fatalf("serveTag = %q", serveTag(base, cfg, hw.Auto))
	}

	model, cmd := m.updateDashboard(tea.KeyMsg{Type: tea.KeyEnter})
	mm := model.(Model)
	if mm.pending[base] == "load" {
		t.Errorf("Enter on a loaded (derived) model should open, not reload")
	}
	if len(be.createCalls) != 0 || len(be.loadCalls) != 0 {
		t.Errorf("no (re)load expected; create=%v load=%v", be.createCalls, be.loadCalls)
	}
	if cmd == nil {
		t.Fatal("expected an exec command")
	}
	data, err := os.ReadFile(opts.OpencodeJSON)
	if err != nil {
		t.Fatalf("opencode.json not written: %v", err)
	}
	var doc map[string]any
	json.Unmarshal(data, &doc)
	if doc["model"] != "ollama/tuicode/a-1b:c8192ga" {
		t.Errorf("default model = %v, want the derived serve tag", doc["model"])
	}
}

func TestPruneDerived(t *testing.T) {
	be := &fakeBackend{
		reachable: true,
		disk: []server.DiskModel{
			{Tag: "a:1b", Size: gib},                  // base — keep
			{Tag: "tuicode/a-1b:c8192ga", Size: gib},  // derived, loaded — keep
			{Tag: "tuicode/a-1b:c16384ga", Size: gib}, // derived, unused — prune
			{Tag: "tuicode/b-2b:c8192g0", Size: gib},  // derived, unused — prune
		},
		loaded: []server.LoadedModel{{Tag: "tuicode/a-1b:c8192ga", Size: gib, SizeVRAM: gib}},
	}
	n, err := pruneDerived(context.Background(), be)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("pruned %d, want 2", n)
	}
	want := map[string]bool{"tuicode/a-1b:c16384ga": true, "tuicode/b-2b:c8192g0": true}
	for _, d := range be.deleteCalls {
		if !want[d] {
			t.Errorf("unexpected delete of %q (base or loaded derived)", d)
		}
	}
	if len(be.deleteCalls) != 2 {
		t.Errorf("delete calls = %v, want exactly the 2 unused derived models", be.deleteCalls)
	}
}

func TestDerivedModelsHiddenFromList(t *testing.T) {
	be := &fakeBackend{reachable: true, disk: []server.DiskModel{
		{Tag: "a:1b", Size: gib},
		{Tag: "tuicode/a-1b:c8192ga", Size: gib}, // derived — must be hidden
	}}
	m := New(testOpts(t, be, true))
	model, _ := m.Update(listResultMsg{disk: be.disk})
	mm := model.(Model)
	if len(mm.disk) != 1 || mm.disk[0].Tag != "a:1b" {
		t.Errorf("derived models should be hidden from the table, got %v", mm.disk)
	}
}
