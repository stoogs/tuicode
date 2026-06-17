package tui

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"tuicode/internal/deps"
	"tuicode/internal/hw"
	"tuicode/internal/server"
	"tuicode/internal/store"
)

// fakeBackend records calls and returns canned data.
type fakeBackend struct {
	mu          sync.Mutex
	disk        []server.DiskModel
	loaded      []server.LoadedModel
	reachable   bool
	loadCalls   []loadCall
	unloadCalls []string
	createCalls []createCall
	deleteCalls []string
}

type loadCall struct {
	tag       string
	keepAlive string
}

type createCall struct {
	name   string
	from   string
	params map[string]any
}

func (f *fakeBackend) Status(context.Context) server.DaemonStatus {
	return server.DaemonStatus{Reachable: f.reachable, Version: "0.5.4", Endpoint: "http://localhost:11434"}
}
func (f *fakeBackend) List(context.Context) ([]server.DiskModel, error)     { return f.disk, nil }
func (f *fakeBackend) Loaded(context.Context) ([]server.LoadedModel, error) { return f.loaded, nil }
func (f *fakeBackend) Load(_ context.Context, tag, keepAlive string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.loadCalls = append(f.loadCalls, loadCall{tag, keepAlive})
	return nil
}
func (f *fakeBackend) Unload(_ context.Context, tag string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.unloadCalls = append(f.unloadCalls, tag)
	return nil
}
func (f *fakeBackend) Create(_ context.Context, name, from string, params map[string]any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.createCalls = append(f.createCalls, createCall{name, from, params})
	return nil
}
func (f *fakeBackend) Delete(_ context.Context, tag string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleteCalls = append(f.deleteCalls, tag)
	return nil
}
func (f *fakeBackend) Pull(_ context.Context, tag string, p func(server.PullProgress)) error {
	if p != nil {
		p(server.PullProgress{Status: "pulling manifest"})
		p(server.PullProgress{Status: "downloading", Completed: 290, Total: 290})
	}
	return nil
}
func (f *fakeBackend) Show(_ context.Context, tag string) (server.ModelDetails, error) {
	return server.ModelDetails{Tag: tag, ContextLength: 32768}, nil
}

func testOpts(t *testing.T, be server.Backend, ok bool) Options {
	t.Helper()
	st := &store.Store{Dir: t.TempDir()}
	report := deps.Report{}
	if ok {
		report.OpenCode = deps.Tool{Name: "opencode", Found: true, Version: "1.17"}
		report.Ollama = deps.Tool{Name: "ollama", Found: true, Version: "0.5.4"}
	}
	return Options{
		Backend:      be,
		Store:        st,
		AppConfig:    store.DefaultAppConfig(),
		Deps:         report,
		DeviceMode:   hw.Auto,
		OpencodeJSON: t.TempDir() + "/opencode.json",
		WorkingDir:   t.TempDir(),
		Logf:         func(string) {},
	}
}

func loadedModel(tag string) server.LoadedModel {
	return server.LoadedModel{
		Tag: tag, Size: 10 * gib, SizeVRAM: 10 * gib, Context: 65536,
		ExpiresAt: time.Now().Add(16 * time.Minute),
	}
}

func TestNewSelectsPrereqWhenMissing(t *testing.T) {
	m := New(testOpts(t, &fakeBackend{}, false))
	if m.screen != screenPrereq {
		t.Fatalf("screen = %v, want prereq", m.screen)
	}
	v := m.viewPrereq()
	if !strings.Contains(v, "missing prerequisites") {
		t.Errorf("prereq view missing heading:\n%s", v)
	}
}

func TestNewSelectsDashboardWhenOK(t *testing.T) {
	m := New(testOpts(t, &fakeBackend{reachable: true}, true))
	if m.screen != screenDashboard {
		t.Fatalf("screen = %v, want dashboard", m.screen)
	}
}

// renderAll exercises every screen's View to catch nil/panic regressions.
func TestAllScreensRender(t *testing.T) {
	be := &fakeBackend{
		reachable: true,
		disk:      []server.DiskModel{{Tag: "qwen3-coder:30b", Size: 10 * gib, ParamSize: "30B", Quant: "Q4_K_M"}},
		loaded:    []server.LoadedModel{loadedModel("qwen3-coder:30b")},
	}
	m := New(testOpts(t, be, true))
	m.width, m.height = 100, 40
	m.disk = be.disk
	m.loaded = be.loaded
	m.daemon = be.Status(context.Background())
	m.detection = hw.Detection{Mode: hw.Auto, GPU: &hw.Memory{Source: "gpu", Total: 16 * gib, Free: 6 * gib}, HasGPU: true}

	for _, s := range []screenID{screenDashboard, screenSettings, screenOnboarding} {
		m.screen = s
		if got := m.View(); strings.TrimSpace(got) == "" {
			t.Errorf("screen %v rendered empty", s)
		}
	}
	// configure needs setup
	m2 := m.enterConfigure("qwen3-coder:30b")
	if got := m2.View(); strings.TrimSpace(got) == "" {
		t.Errorf("configure rendered empty")
	}
}

func TestLoadFromDashboardUsesResidencyKeepAlive(t *testing.T) {
	be := &fakeBackend{
		reachable: true,
		disk:      []server.DiskModel{{Tag: "gemma3:270m", Size: gib, ParamSize: "270M", Quant: "Q8_0"}},
	}
	m := New(testOpts(t, be, true))
	m.disk = be.disk
	m.screen = screenDashboard
	m.dashCursor = 0

	// Press enter to load the selected (stopped) model.
	model, cmd := m.updateDashboard(tea.KeyMsg{Type: tea.KeyEnter})
	mm := model.(Model)
	if mm.pending["gemma3:270m"] != "load" {
		t.Errorf("expected pending=load, got %q", mm.pending["gemma3:270m"])
	}
	if cmd == nil {
		t.Fatal("expected a command from load")
	}
	drain(cmd)
	if len(be.loadCalls) != 1 {
		t.Fatalf("expected 1 load call, got %d", len(be.loadCalls))
	}
	lc := be.loadCalls[0]
	if lc.tag != "gemma3:270m" {
		t.Errorf("load tag = %q", lc.tag)
	}
	if lc.keepAlive != "20m" { // default residency is auto-unload 20m
		t.Errorf("keepAlive = %q, want 20m", lc.keepAlive)
	}
}

func TestEnterOpensOpenCodeWhenLoaded(t *testing.T) {
	be := &fakeBackend{reachable: true,
		disk:   []server.DiskModel{{Tag: "a:1b", Size: gib}},
		loaded: []server.LoadedModel{loadedModel("a:1b")},
	}
	opts := testOpts(t, be, true)
	m := New(opts)
	m.disk = be.disk
	m.loaded = be.loaded
	m.screen = screenDashboard
	m.dashCursor = 0

	// Enter on an already-loaded model opens OpenCode → writes opencode.json
	// with the default model set, and issues no unload.
	_, cmd := m.updateDashboard(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected an exec command")
	}
	if len(be.unloadCalls) != 0 {
		t.Errorf("enter on a loaded model must not unload; calls=%v", be.unloadCalls)
	}
	data, err := os.ReadFile(opts.OpencodeJSON)
	if err != nil {
		t.Fatalf("opencode.json not written: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if doc["model"] != "ollama/a:1b" {
		t.Errorf("default model = %v, want ollama/a:1b", doc["model"])
	}
}

func TestEnterLoadsWhenStopped(t *testing.T) {
	be := &fakeBackend{reachable: true,
		disk: []server.DiskModel{{Tag: "a:1b", Size: gib}},
	}
	m := New(testOpts(t, be, true))
	m.disk = be.disk
	m.screen = screenDashboard
	m.dashCursor = 0

	model, cmd := m.updateDashboard(tea.KeyMsg{Type: tea.KeyEnter})
	if mm := model.(Model); mm.pending["a:1b"] != "load" {
		t.Errorf("enter on a stopped model should load it; pending=%q", mm.pending["a:1b"])
	}
	drain(cmd)
	if len(be.loadCalls) != 1 {
		t.Errorf("expected 1 load call, got %d", len(be.loadCalls))
	}
}

func TestInlineColumnEditPersists(t *testing.T) {
	be := &fakeBackend{reachable: true,
		disk: []server.DiskModel{{Tag: "qwen3-coder:30b", Size: 10 * gib, ParamSize: "30B", Quant: "Q4_K_M"}},
	}
	opts := testOpts(t, be, true)
	m := New(opts)
	m.disk = be.disk
	m.screen = screenDashboard
	m.dashCursor = 0

	// Focus the GPU column and change it with ".".
	m.dashCol = colNumGPU
	model, _ := m.updateDashboard(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(".")})
	mm := model.(Model)
	got := mm.configs["qwen3-coder:30b"].NumGPU
	if got == store.NumGPUAuto {
		t.Errorf("num_gpu did not change from auto, still %d", got)
	}
	// Persisted to disk.
	cfg, ok, err := opts.Store.LoadModelConfig("qwen3-coder:30b")
	if err != nil || !ok {
		t.Fatalf("config not persisted: ok=%v err=%v", ok, err)
	}
	if cfg.NumGPU != got {
		t.Errorf("persisted num_gpu %d != in-memory %d", cfg.NumGPU, got)
	}
}

func TestQuitUnloadsModels(t *testing.T) {
	be := &fakeBackend{reachable: true,
		disk:   []server.DiskModel{{Tag: "a:1b", Size: gib}},
		loaded: []server.LoadedModel{loadedModel("a:1b")},
	}
	m := New(testOpts(t, be, true))
	m.disk = be.disk
	m.loaded = be.loaded
	m.screen = screenDashboard

	model, cmd := m.updateDashboard(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	mm := model.(Model)
	if !mm.quitting {
		t.Fatal("q should begin graceful shutdown")
	}
	drain(cmd)
	if len(be.unloadCalls) != 1 || be.unloadCalls[0] != "a:1b" {
		t.Errorf("quit should unload resident models; calls=%v", be.unloadCalls)
	}
}

func TestCtrlCUnloadsModels(t *testing.T) {
	be := &fakeBackend{reachable: true,
		disk:   []server.DiskModel{{Tag: "a:1b", Size: gib}},
		loaded: []server.LoadedModel{loadedModel("a:1b")},
	}
	m := New(testOpts(t, be, true))
	m.disk = be.disk
	m.loaded = be.loaded
	m.screen = screenDashboard

	model, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if !model.(Model).quitting {
		t.Fatal("ctrl+c should begin graceful shutdown on the dashboard")
	}
	drain(cmd)
	if len(be.unloadCalls) != 1 {
		t.Errorf("ctrl+c should unload resident models; calls=%v", be.unloadCalls)
	}

	// Second ctrl+c forces an immediate quit (no further unloads needed).
	m2 := model.(Model)
	_, cmd2 := m2.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd2 == nil {
		t.Error("second ctrl+c should issue quit")
	}
}

func TestLoadedPromptOpensOnEnter(t *testing.T) {
	be := &fakeBackend{reachable: true,
		disk:   []server.DiskModel{{Tag: "a:1b", Size: gib}},
		loaded: []server.LoadedModel{loadedModel("a:1b")},
	}
	opts := testOpts(t, be, true)
	m := New(opts)
	m.disk = be.disk
	m.loaded = be.loaded
	m.screen = screenDashboard

	// Load completes → prompt appears.
	model, _ := m.Update(actionResultMsg{action: "load", tag: "a:1b"})
	mm := model.(Model)
	if mm.loadedPrompt != "a:1b" {
		t.Fatalf("expected loadedPrompt set, got %q", mm.loadedPrompt)
	}
	if !strings.Contains(mm.viewDashboard(), "continue in OpenCode") {
		t.Error("prompt banner not rendered")
	}
	// Enter opens OpenCode (writes opencode.json) and clears the prompt.
	model2, cmd := mm.updateDashboard(tea.KeyMsg{Type: tea.KeyEnter})
	if model2.(Model).loadedPrompt != "" {
		t.Error("prompt should clear on Enter")
	}
	if cmd == nil {
		t.Fatal("expected exec command")
	}
	if _, err := os.ReadFile(opts.OpencodeJSON); err != nil {
		t.Errorf("opencode.json not written: %v", err)
	}
}

func TestLoadedPromptDismissesOnOtherKey(t *testing.T) {
	be := &fakeBackend{reachable: true, disk: []server.DiskModel{{Tag: "a:1b", Size: gib}, {Tag: "b:2b", Size: gib}}}
	m := New(testOpts(t, be, true))
	m.disk = be.disk
	m.screen = screenDashboard
	m.loadedPrompt = "a:1b"

	// A navigation key dismisses the prompt and is handled normally.
	model, _ := m.updateDashboard(tea.KeyMsg{Type: tea.KeyDown})
	mm := model.(Model)
	if mm.loadedPrompt != "" {
		t.Error("prompt should dismiss on a non-Enter key")
	}
	if mm.dashCursor != 1 {
		t.Errorf("down should still move the cursor; got %d", mm.dashCursor)
	}
}

func TestFavouriteToggleAndStartupCursor(t *testing.T) {
	be := &fakeBackend{reachable: true, disk: []server.DiskModel{
		{Tag: "a:1b", Size: gib}, {Tag: "b:2b", Size: gib}, {Tag: "c:3b", Size: gib},
	}}
	opts := testOpts(t, be, true)
	m := New(opts)
	m.disk = be.disk
	m.screen = screenDashboard
	m.dashCursor = 1 // select b:2b

	// Press f → b:2b becomes the favourite, persisted.
	model, _ := m.updateDashboard(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")})
	mm := model.(Model)
	if mm.opts.AppConfig.Favourite != "b:2b" {
		t.Fatalf("favourite = %q, want b:2b", mm.opts.AppConfig.Favourite)
	}
	got, _ := opts.Store.LoadAppConfig()
	if got.Favourite != "b:2b" {
		t.Errorf("favourite not persisted: %q", got.Favourite)
	}
	// Star renders on that row.
	if !strings.Contains(mm.viewDashboard(), "★") {
		t.Error("favourite star not rendered")
	}

	// A fresh model with the favourite set positions the cursor on it at startup.
	opts2 := opts
	opts2.AppConfig.Favourite = "c:3b"
	m2 := New(opts2)
	model2, _ := m2.Update(listResultMsg{disk: be.disk})
	if mm2 := model2.(Model); mm2.dashCursor != 2 {
		t.Errorf("startup cursor = %d, want 2 (the favourite c:3b)", mm2.dashCursor)
	}

	// Press f again on the favourite → unfavourite.
	mm.dashCursor = 1
	model3, _ := mm.updateDashboard(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")})
	if model3.(Model).opts.AppConfig.Favourite != "" {
		t.Error("pressing f on the favourite should clear it")
	}
}

func TestUnloadFromDashboard(t *testing.T) {
	be := &fakeBackend{reachable: true,
		disk:   []server.DiskModel{{Tag: "a:1b", Size: gib}},
		loaded: []server.LoadedModel{loadedModel("a:1b")},
	}
	m := New(testOpts(t, be, true))
	m.disk = be.disk
	m.loaded = be.loaded
	m.screen = screenDashboard
	m.dashCursor = 0

	_, cmd := m.updateDashboard(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("u")})
	if cmd == nil {
		t.Fatal("expected unload command")
	}
	drain(cmd)
	if len(be.unloadCalls) != 1 || be.unloadCalls[0] != "a:1b" {
		t.Errorf("unload calls = %v", be.unloadCalls)
	}
}

func TestDeviceModeTogglePersists(t *testing.T) {
	be := &fakeBackend{reachable: true}
	opts := testOpts(t, be, true)
	m := New(opts)
	m.screen = screenSettings
	m.settingsCursor = setDevice

	model, _ := m.updateSettings(tea.KeyMsg{Type: tea.KeyRight})
	m2 := model.(Model)
	if m2.opts.DeviceMode == hw.Auto {
		t.Errorf("device mode did not change from auto")
	}
	// Persisted to disk.
	got, err := opts.Store.LoadAppConfig()
	if err != nil {
		t.Fatal(err)
	}
	if got.DeviceMode == "auto" {
		t.Errorf("device mode not persisted: %q", got.DeviceMode)
	}
}

func TestLoadUnloadsOthersFirst(t *testing.T) {
	be := &fakeBackend{reachable: true,
		disk:   []server.DiskModel{{Tag: "c:3b", Size: 3 * gib}},
		loaded: []server.LoadedModel{loadedModel("a:1b"), loadedModel("b:2b")},
	}
	m := New(testOpts(t, be, true))
	m.disk = be.disk
	m.loaded = be.loaded
	m.screen = screenDashboard
	m.dashCursor = 0

	model, cmd := m.updateDashboard(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	mm := model.(Model)
	if mm.pending["a:1b"] != "unload" || mm.pending["b:2b"] != "unload" {
		t.Errorf("expected other models pending unload, got %v", mm.pending)
	}
	if mm.pending["c:3b"] != "load" {
		t.Errorf("expected c:3b pending load, got %q", mm.pending["c:3b"])
	}
	drain(cmd)
	if len(be.unloadCalls) != 2 {
		t.Errorf("expected 2 unloads (single-model residency), got %v", be.unloadCalls)
	}
	if len(be.loadCalls) != 1 || be.loadCalls[0].tag != "c:3b" {
		t.Errorf("expected load c:3b, got %v", be.loadCalls)
	}
}

func TestEscUnloadsSelected(t *testing.T) {
	be := &fakeBackend{reachable: true,
		disk:   []server.DiskModel{{Tag: "a:1b", Size: gib}},
		loaded: []server.LoadedModel{loadedModel("a:1b")},
	}
	m := New(testOpts(t, be, true))
	m.disk = be.disk
	m.loaded = be.loaded
	m.screen = screenDashboard

	_, cmd := m.updateDashboard(tea.KeyMsg{Type: tea.KeyEsc})
	drain(cmd)
	if len(be.unloadCalls) != 1 || be.unloadCalls[0] != "a:1b" {
		t.Errorf("esc should unload selected; calls=%v", be.unloadCalls)
	}
}

func TestDeviceCycleFromDashboard(t *testing.T) {
	be := &fakeBackend{reachable: true}
	m := New(testOpts(t, be, true))
	m.screen = screenDashboard
	model, _ := m.updateDashboard(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	if mm := model.(Model); mm.opts.DeviceMode == hw.Auto {
		t.Errorf("d should cycle device mode from auto, got %q", mm.opts.DeviceMode)
	}
}

func TestFittingModels(t *testing.T) {
	// 16GB GPU, 64GB system RAM. Models bigger than VRAM should still appear
	// (they run via a CPU/GPU split), only models bigger than RAM are excluded.
	det := hw.Detection{Mode: hw.Auto, RAM: hw.Memory{Source: "ram", Total: 64 * gib},
		GPU: &hw.Memory{Source: "gpu", Total: 16 * gib, Free: 16 * gib}, HasGPU: true}
	trending := []store.TrendingModel{
		{Tag: "llama3.2:1b", ParamsB: 1}, {Tag: "qwen3:8b", ParamsB: 8},
		{Tag: "qwen2.5-coder:32b", ParamsB: 32}, // ~17.6GB: > VRAM, < RAM → CPU split
		{Tag: "huge:200b", ParamsB: 200},        // > 64GB RAM → excluded
	}
	list := fittingModels(trending, det, trendingLimit)
	if len(list) == 0 || len(list) > trendingLimit {
		t.Fatalf("got %d models, want 1..%d", len(list), trendingLimit)
	}
	byTag := map[string]catalogEntry{}
	for _, e := range list {
		if !e.Fits {
			t.Errorf("%s included but does not fit RAM (est %.1f)", e.Tag, e.EstGB)
		}
		byTag[e.Tag] = e
	}
	// Largest-that-runs first.
	if list[0].ParamsB < list[len(list)-1].ParamsB {
		t.Errorf("not sorted largest-first: %.0f then %.0f", list[0].ParamsB, list[len(list)-1].ParamsB)
	}
	// A model that exceeds system RAM is excluded.
	if _, ok := byTag["huge:200b"]; ok {
		t.Error("200B model should not fit in 64GB RAM")
	}
	// A 32B (~17.6GB) exceeds the 16GB GPU but fits RAM → shown, flagged as a split.
	if e, ok := byTag["qwen2.5-coder:32b"]; !ok {
		t.Error("32B model should be shown (runs via CPU split)")
	} else if e.VRAMFit {
		t.Error("32B model should not fit entirely in 16GB VRAM")
	}
	if e, ok := byTag["llama3.2:1b"]; ok && !e.VRAMFit {
		t.Error("1B model should fit entirely in VRAM")
	}
}

// drain executes a tea.Cmd, recursing into batched children. Each command runs
// with a short timeout so the status-clear timer (a long sleep) is abandoned
// rather than blocking the test.
func drain(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	var msg tea.Msg
	select {
	case msg = <-done:
	case <-time.After(300 * time.Millisecond):
		return
	}
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			drain(c)
		}
	}
}
