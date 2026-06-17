// Package tui implements the tuicode terminal dashboard on Bubble Tea.
package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"tuicode/internal/deps"
	"tuicode/internal/hw"
	"tuicode/internal/ocfg"
	"tuicode/internal/server"
	"tuicode/internal/store"
)

// Options carries everything main wires up: resolved config, backend, paths.
type Options struct {
	Backend      server.Backend
	Store        *store.Store
	AppConfig    store.AppConfig
	Deps         deps.Report
	DeviceMode   hw.DeviceMode
	OpencodeJSON string // resolved target path
	WorkingDir   string // dir to launch OpenCode in
	OpencodeBin  string // path/name of opencode binary
	DryRun       bool
	Verbose      bool
	Logf         func(string)
}

type screenID int

const (
	screenPrereq screenID = iota
	screenOnboarding
	screenDashboard
	screenModels
	screenConfigure
	screenSettings
)

const pollInterval = 2 * time.Second

// Model is the root Bubble Tea model. It holds shared polled state that every
// screen reads, plus per-screen cursor/edit state.
type Model struct {
	opts   Options
	screen screenID
	width  int
	height int
	quit   bool

	// polled state
	daemon    server.DaemonStatus
	loaded    []server.LoadedModel
	disk      []server.DiskModel
	detection hw.Detection
	now       time.Time
	polling   bool // a poll is in flight (debounce)
	hasPolled bool // first poll has returned (suppresses startup banner flash)

	// transient UI
	status string // status/info line
	errMsg string // last error

	// per-screen cursors
	dashCursor     int // selected model row (index into disk)
	dashCol        int // selected editable column (colContext..colPreset)
	settingsCursor int

	// inline-edit state
	configs map[string]store.ModelConfig // per-tag config cache (edited inline)
	pending map[string]string            // base tag → "load"|"unload"|"delete" in flight

	// derived-model / launch tracking
	launchAfter  string // base tag to open OpenCode on after a (re)load
	confirm      string // pending delete confirmation (base tag), "" = none
	loadedPrompt string // just-loaded base tag awaiting "open in OpenCode?" prompt

	// loading-spinner animation
	spinFrame int
	spinning  bool

	quitting bool // graceful shutdown in progress (unload + prune)

	// configure screen state
	cfg configureState
	// onboarding / pull state
	onboard onboardState
}

// --- messages ---

type tickMsg struct{}
type pollResultMsg struct {
	daemon    server.DaemonStatus
	loaded    []server.LoadedModel
	detection hw.Detection
}
type listResultMsg struct {
	disk []server.DiskModel
	err  error
}
type actionResultMsg struct {
	action string
	tag    string
	err    error
}
type sessionDoneMsg struct{ err error }
type clearStatusMsg struct{ token int }
type spinnerTickMsg struct{}
type pruneResultMsg struct {
	count int
	err   error
}

// New builds the root model and decides the initial screen.
func New(opts Options) Model {
	m := Model{
		opts:    opts,
		now:     time.Now(),
		configs: map[string]store.ModelConfig{},
		pending: map[string]string{},
	}
	switch {
	case !opts.Deps.OK():
		m.screen = screenPrereq
	default:
		m.screen = screenDashboard
	}
	m.onboard = newOnboardState()
	return m
}

func (m Model) Init() tea.Cmd {
	if m.screen == screenPrereq {
		return nil
	}
	// Kick off discovery + polling.
	return tea.Batch(m.pollCmd(), m.listCmd(), tea.Tick(pollInterval, func(time.Time) tea.Msg { return tickMsg{} }))
}

// --- commands ---

func (m Model) pollCmd() tea.Cmd {
	be := m.opts.Backend
	mode := m.opts.DeviceMode
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		st := be.Status(ctx)
		var loaded []server.LoadedModel
		if st.Reachable {
			loaded, _ = be.Loaded(ctx)
		}
		det := hw.Detect(ctx, mode)
		return pollResultMsg{daemon: st, loaded: loaded, detection: det}
	}
}

func (m Model) listCmd() tea.Cmd {
	be := m.opts.Backend
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		disk, err := be.List(ctx)
		return listResultMsg{disk: disk, err: err}
	}
}

// loadServeCmd creates the derived model (if needed) then warm-loads `serve`.
// The result is keyed by the base tag so the UI/launch chain can react.
func (m Model) loadServeCmd(base, serve string, params map[string]any, keepAlive string) tea.Cmd {
	be := m.opts.Backend
	create := serve != base
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
		defer cancel()
		if create {
			if err := be.Create(ctx, serve, base, params); err != nil {
				return actionResultMsg{action: "load", tag: base, err: fmt.Errorf("create %s: %w", serve, err)}
			}
		}
		err := be.Load(ctx, serve, keepAlive)
		return actionResultMsg{action: "load", tag: base, err: err}
	}
}

func (m Model) deleteCmd(tag string) tea.Cmd {
	be := m.opts.Backend
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := be.Delete(ctx, tag)
		return actionResultMsg{action: "delete", tag: tag, err: err}
	}
}

// pruneCmd removes unused derived models and reports the count.
func (m Model) pruneCmd() tea.Cmd {
	be := m.opts.Backend
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		n, err := pruneDerived(ctx, be)
		return pruneResultMsg{count: n, err: err}
	}
}

// beginShutdown starts a graceful quit: unload all resident models and prune
// derived models, then exit. A second quit/ctrl+c forces an immediate exit.
func (m Model) beginShutdown() (tea.Model, tea.Cmd) {
	if m.quitting {
		m.quit = true
		return m, tea.Quit
	}
	m.quitting = true
	m.status = "unloading models & quitting…  (press again to force)"
	m.errMsg = ""
	return m, m.shutdownCmd()
}

// shutdownCmd unloads every resident model, prunes derived models, then quits.
// Best-effort: daemon errors still quit cleanly.
func (m Model) shutdownCmd() tea.Cmd {
	be := m.opts.Backend
	loaded := append([]server.LoadedModel(nil), m.loaded...)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		for _, lm := range loaded {
			_ = be.Unload(ctx, lm.Tag)
		}
		_, _ = pruneDerived(ctx, be)
		return tea.QuitMsg{}
	}
}

func (m Model) unloadCmd(tag string) tea.Cmd {
	be := m.opts.Backend
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := be.Unload(ctx, tag)
		return actionResultMsg{action: "unload", tag: tag, err: err}
	}
}

// statusToken increments so a delayed clearStatus only clears its own message.
var statusToken int

func (m *Model) setStatus(s string) tea.Cmd {
	m.status = s
	m.errMsg = ""
	statusToken++
	tok := statusToken
	return func() tea.Msg {
		time.Sleep(6 * time.Second)
		return clearStatusMsg{token: tok}
	}
}

func (m *Model) setError(s string) {
	m.errMsg = s
	m.status = ""
}

// scheduleTick re-arms the poll timer.
func scheduleTick() tea.Cmd {
	return tea.Tick(pollInterval, func(time.Time) tea.Msg { return tickMsg{} })
}

// spinnerCmd drives the load animation (faster than the poll tick).
func spinnerCmd() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg { return spinnerTickMsg{} })
}

// hasPendingLoad reports whether any model is currently loading.
func (m Model) hasPendingLoad() bool {
	for _, v := range m.pending {
		if v == "load" {
			return true
		}
	}
	return false
}

// --- update ---

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case tickMsg:
		m.now = time.Now()
		if m.polling {
			// Skip this tick; a poll is still running. Re-arm.
			return m, scheduleTick()
		}
		m.polling = true
		return m, m.pollCmd()

	case pollResultMsg:
		m.polling = false
		m.hasPolled = true
		m.daemon = msg.daemon
		m.loaded = msg.loaded
		m.detection = msg.detection
		m.now = time.Now()
		m.reconcilePending()
		m.clampCursors()
		return m, scheduleTick()

	case listResultMsg:
		if msg.err == nil {
			// Hide tuicode-managed derived models from the table; they're an
			// implementation detail of the base model's context/GPU settings.
			disk := msg.disk[:0:0]
			for _, dm := range msg.disk {
				if !isDerived(dm.Tag) {
					disk = append(disk, dm)
				}
			}
			m.disk = disk
		}
		m.clampCursors()
		// First-run onboarding: no models on disk → onboarding screen.
		if msg.err == nil && len(m.disk) == 0 && m.screen == screenDashboard && !m.onboard.dismissed {
			m.screen = screenOnboarding
		}
		return m, nil

	case actionResultMsg:
		delete(m.pending, msg.tag)
		if msg.err != nil {
			m.launchAfter = ""
			m.setError(fmt.Sprintf("%s %s failed: %v", msg.action, msg.tag, msg.err))
			return m, nil
		}
		switch msg.action {
		case "load":
			// Chained "load → open OpenCode" once the (re)load completes.
			if m.launchAfter == msg.tag {
				m.launchAfter = ""
				cmd := m.setStatus("loaded " + msg.tag)
				mm, exec := m.execOpenCode(msg.tag)
				return mm, tea.Batch(cmd, m.pollCmd(), exec)
			}
			// Otherwise prompt the user to continue into OpenCode.
			m.loadedPrompt = msg.tag
			m.status = ""
			m.errMsg = ""
			return m, m.pollCmd()
		case "unload":
			cmd := m.setStatus("unloaded " + msg.tag)
			return m, tea.Batch(cmd, m.pollCmd())
		case "delete":
			cmd := m.setStatus("deleted " + msg.tag)
			delete(m.configs, msg.tag)
			return m, tea.Batch(cmd, m.listCmd(), m.pollCmd())
		}
		return m, nil

	case clearStatusMsg:
		if msg.token == statusToken {
			m.status = ""
		}
		return m, nil

	case spinnerTickMsg:
		m.spinFrame++
		if m.hasPendingLoad() {
			return m, spinnerCmd()
		}
		m.spinning = false
		return m, nil

	case pruneResultMsg:
		if msg.err != nil {
			m.setError("prune failed: " + msg.err.Error())
			return m, nil
		}
		cmd := m.setStatus(fmt.Sprintf("pruned %d unused derived model(s)", msg.count))
		return m, tea.Batch(cmd, m.listCmd())

	case sessionDoneMsg:
		// Resumed from OpenCode. Repoll and report.
		var note string
		if msg.err != nil {
			note = fmt.Sprintf("OpenCode exited: %v", msg.err)
		} else {
			note = "Session closed."
		}
		cmd := m.setStatus(note)
		return m, tea.Batch(cmd, m.pollCmd())

	case pullProgressMsg, pullDoneMsg:
		return m.updateOnboarding(msg)

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

// handleKey routes key input to the active screen.
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Global quit. On the live screens this unloads models first; on the prereq
	// screen there's nothing to unload.
	if msg.String() == "ctrl+c" {
		if m.screen == screenPrereq {
			m.quit = true
			return m, tea.Quit
		}
		return m.beginShutdown()
	}
	switch m.screen {
	case screenPrereq:
		return m.updatePrereq(msg)
	case screenOnboarding:
		return m.updateOnboardingKey(msg)
	case screenDashboard:
		return m.updateDashboard(msg)
	case screenConfigure:
		return m.updateConfigure(msg)
	case screenSettings:
		return m.updateSettings(msg)
	}
	return m, nil
}

func (m *Model) clampCursors() {
	if m.dashCursor >= len(m.disk) {
		m.dashCursor = len(m.disk) - 1
	}
	if m.dashCursor < 0 {
		m.dashCursor = 0
	}
	m.dashCol = clamp(m.dashCol, 0, colCount-1)
}

// reconcilePending clears in-flight markers once the poll reflects the change.
func (m *Model) reconcilePending() {
	for tag, action := range m.pending {
		_, _, loaded := m.loadedFor(tag)
		if action == "load" && loaded {
			delete(m.pending, tag)
		}
		if action == "unload" && !loaded {
			delete(m.pending, tag)
		}
	}
}

// loadedFor finds the resident instance for a base model — its current serve
// tag, the base tag, or any other tuicode-derived variant of it. Returns the
// loaded model, the actual loaded tag, and whether one was found.
func (m Model) loadedFor(base string) (server.LoadedModel, string, bool) {
	cfg := m.ensureConfig(base)
	serve := serveTag(base, cfg, m.opts.DeviceMode)
	if lm, ok := m.loadedByTag(serve); ok {
		return lm, serve, true
	}
	if lm, ok := m.loadedByTag(base); ok {
		return lm, base, true
	}
	prefix := derivedPrefixFor(base)
	for _, lm := range m.loaded {
		if strings.HasPrefix(lm.Tag, prefix) {
			return lm, lm.Tag, true
		}
	}
	return server.LoadedModel{}, "", false
}

// ensureConfig returns the cached per-model config, loading it on first access.
func (m *Model) ensureConfig(tag string) store.ModelConfig {
	if c, ok := m.configs[tag]; ok {
		return c
	}
	c, _ := m.opts.Store.GetOrDefaultModelConfig(tag, m.opts.AppConfig.DefaultResidency)
	m.configs[tag] = c
	return c
}

// saveConfig persists a per-model config and updates the cache.
func (m *Model) saveConfig(cfg store.ModelConfig) {
	m.configs[cfg.ModelTag] = cfg
	if m.opts.DryRun {
		return
	}
	if err := m.opts.Store.SaveModelConfig(cfg); err != nil {
		m.setError("save config failed: " + err.Error())
	}
}

// selectedDiskTag returns the tag of the model under the dashboard cursor.
func (m Model) selectedDiskTag() (string, bool) {
	if m.dashCursor >= 0 && m.dashCursor < len(m.disk) {
		return m.disk[m.dashCursor].Tag, true
	}
	return "", false
}

// --- view ---

func (m Model) View() string {
	if m.quit {
		return ""
	}
	switch m.screen {
	case screenPrereq:
		return m.viewPrereq()
	case screenOnboarding:
		return m.viewOnboarding()
	case screenConfigure:
		return m.viewConfigure()
	case screenSettings:
		return m.viewSettings()
	default:
		return m.viewDashboard()
	}
}

// contentWidth is the usable inner width for screen bodies.
func (m Model) contentWidth() int {
	w := m.width
	if w <= 0 {
		w = 80
	}
	if w > 100 {
		w = 100
	}
	return w - 2
}

// --- shared helpers ---

// loadedByTag returns the loaded model with the given tag, if resident.
func (m Model) loadedByTag(tag string) (server.LoadedModel, bool) {
	for _, lm := range m.loaded {
		if lm.Tag == tag {
			return lm, true
		}
	}
	return server.LoadedModel{}, false
}

// launchOpenCode opens OpenCode on a base model. If the resident instance does
// not match the current config (context/GPU/residency changed, or it isn't
// loaded), it (re)loads first and chains the launch via launchAfter; otherwise
// it execs OpenCode immediately.
func (m Model) launchOpenCode(base string) (tea.Model, tea.Cmd) {
	cfg := m.ensureConfig(base)
	serve := serveTag(base, cfg, m.opts.DeviceMode)

	// The serve tag fully encodes the bakeable params (context, GPU layers), so
	// the resident instance is correct iff it's loaded *as* the serve tag.
	// Otherwise reload with the current config, then chain the launch.
	_, actual, loaded := m.loadedFor(base)
	if !loaded || actual != serve {
		m.launchAfter = base
		return m.doLoad(base)
	}
	return m.execOpenCode(base)
}

// execOpenCode writes opencode.json to point at the model's serve tag, then
// suspends the TUI and runs OpenCode as a foreground child.
func (m Model) execOpenCode(base string) (tea.Model, tea.Cmd) {
	cfg := m.ensureConfig(base)
	serve := serveTag(base, cfg, m.opts.DeviceMode)

	name := cfg.DisplayName
	if serve != base {
		name = fmt.Sprintf("%s (ctx %s)", cfg.DisplayName, contextShort(cfg.ContextLength))
	}
	w := &ocfg.Writer{
		BackupDir:    m.opts.Store.BackupsDir(),
		Keep:         10,
		DryRun:       m.opts.DryRun,
		DefaultModel: serve, // OpenCode opens with this model selected
		Log:          m.opts.Logf,
	}
	entry := ocfg.ModelEntry{Tag: serve, DisplayName: name}
	if _, err := w.Write(m.opts.OpencodeJSON, []ocfg.ModelEntry{entry}); err != nil {
		m.setError(fmt.Sprintf("opencode.json write failed: %v", err))
		return m, nil
	}

	bin := m.opts.OpencodeBin
	if bin == "" {
		bin = "opencode"
	}
	cmd := exec.Command(bin)
	cmd.Dir = m.opts.WorkingDir
	cmd.Env = os.Environ()
	// tea.ExecProcess suspends the TUI (leaves alt-screen, restores cooked mode),
	// runs OpenCode as a foreground child, and resumes on exit.
	return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
		return sessionDoneMsg{err: err}
	})
}
