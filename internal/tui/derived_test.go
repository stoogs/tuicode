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

func TestGlobalDefaultContextFollows(t *testing.T) {
	be := &fakeBackend{reachable: true, disk: []server.DiskModel{{Tag: "a:1b", Size: gib}}}
	m := New(testOpts(t, be, true))
	m.disk = be.disk
	base := "a:1b"

	// No per-model context, no global default → auto (no derived).
	if got := m.effectiveContext(m.ensureConfig(base)); got != 0 {
		t.Errorf("effectiveContext = %d, want 0 (auto)", got)
	}

	// A global default → an un-set model follows it (and now needs a derived model).
	m.opts.AppConfig.DefaultContext = 32768
	if got := m.effectiveContext(m.ensureConfig(base)); got != 32768 {
		t.Errorf("effectiveContext = %d, want 32768 (follows global)", got)
	}
	if got := m.servedConfig(base); got.ContextLength != 32768 {
		t.Errorf("servedConfig ctx = %d, want 32768", got.ContextLength)
	}
	if !needsDerived(m.servedConfig(base), hw.Auto) {
		t.Error("a model following a 32k global default should need a derived model")
	}

	// An explicit per-model context overrides the global default.
	cfg := m.ensureConfig(base)
	cfg.ContextLength = 16384
	m.configs[base] = cfg
	if got := m.effectiveContext(m.ensureConfig(base)); got != 16384 {
		t.Errorf("effectiveContext = %d, want 16384 (per-model override)", got)
	}
}

// With a non-zero global default, the CTX column must still let you land on
// "default" (stored 0) as a sticky bottom stop and step back up from it.
func TestContextColumnDefaultBackstop(t *testing.T) {
	be := &fakeBackend{reachable: true, disk: []server.DiskModel{{Tag: "a:1b", Size: gib}}}
	m := New(testOpts(t, be, true))
	m.disk = be.disk
	m.screen = screenDashboard
	m.dashCursor = 0
	m.dashCol = colContext
	m.opts.AppConfig.DefaultContext = 65536 // global default 64k
	base := "a:1b"

	// Down at default (stored 0) stays at default — the reachable backstop, even
	// though it resolves to the 64k global default.
	model, _ := m.updateDashboard(runes(","))
	m = model.(Model)
	if got := m.ensureConfig(base).ContextLength; got != 0 {
		t.Errorf("down at default should stay 0 (backstop), got %d", got)
	}
	// Up pins the smallest explicit context; down returns to default.
	model, _ = m.updateDashboard(runes("."))
	m = model.(Model)
	if got := m.ensureConfig(base).ContextLength; got != 4096 {
		t.Errorf("up from default should pin 4096, got %d", got)
	}
	model, _ = m.updateDashboard(runes(","))
	m = model.(Model)
	if got := m.ensureConfig(base).ContextLength; got != 0 {
		t.Errorf("down from 4k should return to default, got %d", got)
	}
}

func TestLaunchWritesLimitAndCompaction(t *testing.T) {
	base := "a:1b"
	be := &fakeBackend{reachable: true, disk: []server.DiskModel{{Tag: base, Size: gib}}}
	opts := testOpts(t, be, true)
	m := New(opts)
	m.disk = be.disk
	m.opts.AppConfig.DefaultContext = 32768 // global default; the model follows it

	m.execOpenCodeSession(base, "") // writes opencode.json as a side effect

	data, err := os.ReadFile(opts.OpencodeJSON)
	if err != nil {
		t.Fatalf("opencode.json not written: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	// compaction block: reserved = 25% of 32768 = 8192.
	comp, ok := doc["compaction"].(map[string]any)
	if !ok {
		t.Fatalf("no compaction block: %v", doc)
	}
	if comp["auto"] != true || comp["prune"] != true {
		t.Errorf("compaction toggles = %v", comp)
	}
	if comp["reserved"].(float64) != 8192 {
		t.Errorf("reserved = %v, want 8192 (25%% of 32k)", comp["reserved"])
	}

	// per-model limit.context = the followed 32k window.
	serve := serveTag(base, m.servedConfig(base), hw.Auto)
	entry := doc["provider"].(map[string]any)["ollama"].(map[string]any)["models"].(map[string]any)[serve].(map[string]any)
	limit, ok := entry["limit"].(map[string]any)
	if !ok {
		t.Fatalf("no limit on entry %q: %v", serve, entry)
	}
	if limit["context"].(float64) != 32768 {
		t.Errorf("limit.context = %v, want 32768", limit["context"])
	}
}

func TestUnifiedMemoryLocksGPUSplit(t *testing.T) {
	be := &fakeBackend{reachable: true, disk: []server.DiskModel{{Tag: "a:1b", Size: gib}}}
	m := New(testOpts(t, be, true))
	m.disk = be.disk
	m.screen = screenDashboard
	base := "a:1b"

	// User had previously pinned a manual offload.
	cfg := m.ensureConfig(base)
	cfg.NumGPU = 20
	m.configs[base] = cfg

	// Discrete GPU: the manual value is honoured.
	m.detection = hw.Detection{Unified: false}
	if got := m.servedConfig(base).NumGPU; got != 20 {
		t.Errorf("discrete: served NumGPU = %d, want 20 (honoured)", got)
	}

	// Unified memory: the split is forced back to auto regardless of the stored
	// value (the guard), and the GPU column is skipped in navigation.
	m.detection = hw.Detection{Unified: true}
	if got := m.servedConfig(base).NumGPU; got != store.NumGPUAuto {
		t.Errorf("unified: served NumGPU = %d, want auto (%d)", got, store.NumGPUAuto)
	}
	if m.colEnabled(colNumGPU) {
		t.Error("unified: GPU column should be disabled")
	}
	m.dashCol = colContext
	if next := m.stepCol(+1); next == colNumGPU {
		t.Error("unified: column nav should skip the GPU column")
	}
	// The stored config is untouched (only the served view is forced).
	if m.ensureConfig(base).NumGPU != 20 {
		t.Error("stored NumGPU should be preserved, not overwritten")
	}
}

func TestLaunchManageCompactionOffPreservesUserBlock(t *testing.T) {
	base := "a:1b"
	be := &fakeBackend{reachable: true, disk: []server.DiskModel{{Tag: base, Size: gib}}}
	opts := testOpts(t, be, true)
	// User hand-maintains their own compaction block.
	userJSON := `{"$schema":"https://opencode.ai/config.json","compaction":{"auto":false,"reserved":12345}}`
	if err := os.WriteFile(opts.OpencodeJSON, []byte(userJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New(opts)
	m.disk = be.disk
	m.opts.AppConfig.Compaction.Manage = false // don't touch compaction

	m.execOpenCodeSession(base, "")

	data, err := os.ReadFile(opts.OpencodeJSON)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	json.Unmarshal(data, &doc)
	comp, ok := doc["compaction"].(map[string]any)
	if !ok {
		t.Fatalf("compaction block lost: %v", doc)
	}
	if comp["auto"] != false || comp["reserved"].(float64) != 12345 {
		t.Errorf("user compaction was modified: %v", comp)
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
	// Model loaded as the base (default ctx). User raises ctx — a VRAM-affecting
	// change — then presses Enter. tuicode must reload (create+load the derived)
	// to apply the change WITHOUT auto-launching OpenCode, so the new split/VRAM
	// can be measured. Only once it's reloaded does the next Enter open OpenCode.
	be := &fakeBackend{reachable: true,
		disk:   []server.DiskModel{{Tag: "a:1b", Size: gib, ParamSize: "1B", Quant: "Q4_K_M"}},
		loaded: []server.LoadedModel{loadedModel("a:1b")},
	}
	m := New(testOpts(t, be, true))
	m.disk = be.disk
	m.loaded = be.loaded
	m.screen = screenDashboard
	m.dashCol = colContext
	model, _ := m.updateDashboard(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(".")}) // ctx → 4096 (non-default)
	m = model.(Model)

	model2, cmd := m.updateDashboard(tea.KeyMsg{Type: tea.KeyEnter}) // apply + measure
	mm := model2.(Model)
	if mm.launchAfter != "" {
		t.Errorf("a settings-change reload must NOT auto-launch OpenCode, got launchAfter=%q", mm.launchAfter)
	}
	if mm.pending["a:1b"] != "load" {
		t.Errorf("expected a reload (pending load), got %q", mm.pending["a:1b"])
	}
	drain(cmd)
	if len(be.createCalls) != 1 {
		t.Errorf("expected derived create on reload, got %d", len(be.createCalls))
	}

	// On reload completion the dashboard shows the "loaded — [⏎] continue" prompt
	// rather than launching, so the user can read the measured split first.
	model3, _ := mm.Update(actionResultMsg{action: "load", tag: "a:1b"})
	if got := model3.(Model).loadedPrompt; got != "a:1b" {
		t.Errorf("expected loadedPrompt after measure-reload, got %q", got)
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

// Regression: a model with a non-default context is resident under its (stable)
// derived tag and was loaded with the current settings, so Enter/o must
// recognize it as loaded (via loadedCurrent) and open OpenCode rather than
// reload it.
func TestEnterOpensModelLoadedAsDerived(t *testing.T) {
	base := "a:1b"
	be := &fakeBackend{reachable: true,
		disk:   []server.DiskModel{{Tag: base, Size: gib, ParamSize: "1B", Quant: "Q4_K_M"}},
		loaded: []server.LoadedModel{{Tag: "tuicode/a-1b:tuned", Size: gib, SizeVRAM: gib, Context: 8192}},
	}
	opts := testOpts(t, be, true)
	m := New(opts)
	m.disk = be.disk
	m.loaded = be.loaded
	m.screen = screenDashboard
	cfg := m.ensureConfig(base)
	cfg.ContextLength = 8192
	m.configs[base] = cfg
	// Record that this session loaded the model with the current config, so the
	// stable serve tag is recognized as already-correct (no reload).
	m.applied[base] = configKey(cfg, hw.Auto)

	// sanity: the derived serve tag matches what's resident
	if serveTag(base, cfg, hw.Auto) != "tuicode/a-1b:tuned" {
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
	if doc["model"] != "ollama/tuicode/a-1b:tuned" {
		t.Errorf("default model = %v, want the derived serve tag", doc["model"])
	}
}

// A model resident under the stable derived tag but loaded with a STALE config
// (e.g. context bumped since it was loaded) must reload so the new settings take
// effect — the tag matching is no longer sufficient.
func TestEnterReloadsWhenConfigChanged(t *testing.T) {
	base := "a:1b"
	be := &fakeBackend{reachable: true,
		disk:   []server.DiskModel{{Tag: base, Size: gib, ParamSize: "1B", Quant: "Q4_K_M"}},
		loaded: []server.LoadedModel{{Tag: "tuicode/a-1b:tuned", Size: gib, SizeVRAM: gib, Context: 8192}},
	}
	m := New(testOpts(t, be, true))
	m.disk = be.disk
	m.loaded = be.loaded
	m.screen = screenDashboard
	cfg := m.ensureConfig(base)
	cfg.ContextLength = 8192
	m.configs[base] = cfg
	m.applied[base] = configKey(cfg, hw.Auto) // resident at 8k

	// User bumps the context to 16k; the resident 8k instance is now stale.
	cfg.ContextLength = 16384
	m.configs[base] = cfg

	model, _ := m.updateDashboard(tea.KeyMsg{Type: tea.KeyEnter})
	mm := model.(Model)
	if mm.pending[base] != "load" {
		t.Errorf("Enter after a context change should reload; pending=%v", mm.pending)
	}
}

func TestPruneDerived(t *testing.T) {
	be := &fakeBackend{
		reachable: true,
		disk: []server.DiskModel{
			{Tag: "a:1b", Size: gib},                  // base — keep
			{Tag: "tuicode/a-1b:tuned", Size: gib},    // derived, loaded — keep
			{Tag: "tuicode/a-1b:c16384ga", Size: gib}, // stale derived (old scheme), unused — prune
			{Tag: "tuicode/b-2b:tuned", Size: gib},    // derived, unused — prune
		},
		loaded: []server.LoadedModel{{Tag: "tuicode/a-1b:tuned", Size: gib, SizeVRAM: gib}},
	}
	n, err := pruneDerived(context.Background(), be)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("pruned %d, want 2", n)
	}
	want := map[string]bool{"tuicode/a-1b:c16384ga": true, "tuicode/b-2b:tuned": true}
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
		{Tag: "tuicode/a-1b:tuned", Size: gib}, // derived — must be hidden
	}}
	m := New(testOpts(t, be, true))
	model, _ := m.Update(listResultMsg{disk: be.disk})
	mm := model.(Model)
	if len(mm.disk) != 1 || mm.disk[0].Tag != "a:1b" {
		t.Errorf("derived models should be hidden from the table, got %v", mm.disk)
	}
}
