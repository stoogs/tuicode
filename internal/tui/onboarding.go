package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"tuicode/internal/server"
)

// starterModel is the small model offered on first run when no models exist.
// It must support tool-calling — OpenCode requires tools, so ultra-tiny models
// like gemma3:270m (no tool template) would fail at the OpenCode step.
const (
	starterModel = "llama3.2:1b"
	starterSize  = "~1.3GB"
)

// pull-screen tabs
const (
	tabTrending = iota
	tabRecommended
	tabManual
	tabCount
)

type onboardState struct {
	dismissed bool
	pulling   bool
	done      bool
	pullTag   string
	progress  server.PullProgress
	lastErr   string
	tab       int    // tabTrending | tabRecommended | tabManual
	cursor    int    // selected row in the active list
	manual    string // typed/pasted model tag on the MANUAL ADD tab
	ch        chan tea.Msg
}

const (
	trendingLimit    = 10
	recommendedLimit = 12
)

func newOnboardState() onboardState { return onboardState{} }

// pull messages
type pullProgressMsg struct{ p server.PullProgress }
type pullDoneMsg struct{ err error }

// pullLists returns the fitted entries for both tabs.
func (m Model) pullLists() ([]catalogEntry, []recEntry) {
	return fittingModels(m.opts.Trending, m.detection, trendingLimit),
		fittingRecommended(m.opts.Recommended, m.detection, recommendedLimit)
}

// activeLen is the row count of the currently selected tab.
func (m Model) activeLen() int {
	tr, rc := m.pullLists()
	if m.onboard.tab == tabRecommended {
		return len(rc)
	}
	return len(tr)
}

// selectedPullTag returns the tag to pull for the current tab + cursor.
func (m Model) selectedPullTag() string {
	tr, rc := m.pullLists()
	c := m.onboard.cursor
	if m.onboard.tab == tabRecommended {
		if c >= 0 && c < len(rc) {
			return rc[c].Tag
		}
		return starterModel
	}
	if c >= 0 && c < len(tr) {
		return tr[c].Tag
	}
	return starterModel
}

func (m Model) startPull(tag string) (Model, tea.Cmd) {
	ch := make(chan tea.Msg, 16)
	be := m.opts.Backend
	go func() {
		err := be.Pull(context.Background(), tag, func(p server.PullProgress) {
			ch <- pullProgressMsg{p: p}
		})
		ch <- pullDoneMsg{err: err}
	}()
	m.onboard.pulling = true
	m.onboard.done = false
	m.onboard.pullTag = tag
	m.onboard.lastErr = ""
	m.onboard.ch = ch
	return m, waitPull(ch)
}

func waitPull(ch chan tea.Msg) tea.Cmd {
	return func() tea.Msg { return <-ch }
}

// updateOnboarding handles pull progress/done messages.
func (m Model) updateOnboarding(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case pullProgressMsg:
		m.onboard.progress = msg.p
		if m.onboard.ch != nil {
			return m, waitPull(m.onboard.ch)
		}
		return m, nil
	case pullDoneMsg:
		m.onboard.pulling = false
		m.onboard.done = true
		m.onboard.ch = nil
		if msg.err != nil {
			m.onboard.lastErr = msg.err.Error()
			m.setError("pull failed: " + msg.err.Error())
			return m, nil
		}
		cmd := m.setStatus("pulled " + m.onboard.pullTag)
		return m, tea.Batch(cmd, m.listCmd(), m.pollCmd())
	}
	return m, nil
}

func (m Model) updateOnboardingKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.onboard.pulling {
		// Ignore input while a pull is running (ctrl+c handled globally).
		return m, nil
	}
	// On the post-pull success screen, Enter goes to the dashboard (handled here
	// so the MANUAL ADD input doesn't capture it).
	if m.onboard.done && m.onboard.lastErr == "" && msg.String() == "enter" {
		m.onboard.dismissed = true
		m.screen = screenDashboard
		return m, m.listCmd()
	}
	// Tab switching works on every tab (arrows / Tab). Done first so it can't be
	// captured as text by the MANUAL ADD input.
	switch msg.String() {
	case "left", "shift+tab":
		m.onboard.tab = wrap(m.onboard.tab-1, tabCount)
		m.onboard.cursor = 0
		return m, nil
	case "right", "tab":
		m.onboard.tab = wrap(m.onboard.tab+1, tabCount)
		m.onboard.cursor = 0
		return m, nil
	case "esc":
		m.onboard.dismissed = true
		m.screen = screenDashboard
		return m, m.listCmd()
	}
	// The MANUAL ADD tab is a text field: route printable keys into it so a tag
	// like "qwen3.6:35b-a3b" can be typed or pasted without 'q' quitting, etc.
	if m.onboard.tab == tabManual {
		return m.updateManualKey(msg)
	}
	switch msg.String() {
	case "up", "k":
		if m.onboard.cursor > 0 {
			m.onboard.cursor--
		}
		return m, nil
	case "down", "j":
		if m.onboard.cursor < m.activeLen()-1 {
			m.onboard.cursor++
		}
		return m, nil
	case "enter":
		if m.onboard.done && m.onboard.lastErr == "" {
			// Success acknowledged → go to dashboard.
			m.onboard.dismissed = true
			m.screen = screenDashboard
			return m, m.listCmd()
		}
		return m.startPull(m.selectedPullTag())
	case "y":
		// Quick: pull the small starter regardless of selection.
		return m.startPull(starterModel)
	case "n", "q":
		m.onboard.dismissed = true
		m.screen = screenDashboard
		return m, m.listCmd()
	}
	return m, nil
}

// updateManualKey edits the MANUAL ADD text field. Enter pulls the typed tag;
// Backspace deletes; printable runes (incl. a pasted string) are appended.
func (m Model) updateManualKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		tag := strings.TrimSpace(m.onboard.manual)
		if tag == "" {
			return m, nil
		}
		return m.startPull(tag)
	case tea.KeyBackspace, tea.KeyDelete:
		if r := []rune(m.onboard.manual); len(r) > 0 {
			m.onboard.manual = string(r[:len(r)-1])
		}
		return m, nil
	case tea.KeyRunes, tea.KeySpace:
		m.onboard.manual += string(msg.Runes)
		return m, nil
	}
	return m, nil
}

func (m Model) viewOnboarding() string {
	w := m.contentWidth()
	var b strings.Builder
	b.WriteString(m.header())
	b.WriteString("\n\n")
	if len(m.disk) == 0 {
		b.WriteString(titleStyle.Render("Welcome to tuicode") + mutedStyle.Render("  — no models on disk yet"))
	} else {
		b.WriteString(titleStyle.Render("Pull a model"))
	}
	b.WriteString("\n\n")

	if m.onboard.pulling {
		b.WriteString(fmt.Sprintf("Pulling %s …\n\n", accentStyle.Render(m.onboard.pullTag)))
		p := m.onboard.progress
		b.WriteString("  " + mutedStyle.Render(p.Status))
		b.WriteString("\n")
		if p.Total > 0 {
			frac := float64(p.Completed) / float64(p.Total)
			b.WriteString("  " + progressBar(frac, w-24) +
				fmt.Sprintf("  %s / %s", fmtGB(p.Completed), fmtGB(p.Total)))
			b.WriteString("\n")
		}
		b.WriteString("\n" + mutedStyle.Render("Downloading… this can take a minute."))
		return boxWrap(b.String(), m)
	}

	if m.onboard.done && m.onboard.lastErr == "" {
		b.WriteString(goodStyle.Render("✓ Pulled " + m.onboard.pullTag))
		b.WriteString("\n\n")
		b.WriteString("You now have a working model. Press " + keyStyle.Render("[enter]") + " to open the dashboard,")
		b.WriteString("\nthen press " + keyStyle.Render("[⏎]") + " on its row to load it.")
		b.WriteString("\n\n")
		b.WriteString(footer([2]string{"enter", "dashboard"}))
		return boxWrap(b.String(), m)
	}

	if m.onboard.lastErr != "" {
		b.WriteString(badStyle.Render("✗ Pull failed: " + m.onboard.lastErr))
		b.WriteString("\n\n")
	}

	b.WriteString("Models are downloaded with " + accentStyle.Render("ollama pull <name>") + " — Ollama manages\n")
	b.WriteString("storage for you; you never place files by hand.\n\n")

	// --- tab heading: Trending ⇄ Recommended (←/→ to switch) ---
	b.WriteString(m.renderPullTabs())
	b.WriteString("\n")
	b.WriteString(divider(min(m.contentWidth(), 70)))
	b.WriteString("\n")

	switch m.onboard.tab {
	case tabRecommended:
		b.WriteString(m.renderRecommendedList())
	case tabManual:
		b.WriteString(m.renderManualAdd())
	default:
		b.WriteString(m.renderTrendingList())
	}
	b.WriteString("\n")

	if !m.daemon.Reachable {
		b.WriteString(badStyle.Render("Ollama daemon not reachable — start it first: ") +
			accentStyle.Render("sudo systemctl start ollama"))
		b.WriteString("\n\n")
	}
	if m.onboard.tab == tabManual {
		b.WriteString(footer(
			[2]string{"←→", "list"},
			[2]string{"type/paste", "model tag"},
			[2]string{"⏎", "pull"},
			[2]string{"esc", "skip"},
		))
	} else {
		b.WriteString(footer(
			[2]string{"←→", "list"},
			[2]string{"↑↓", "select"},
			[2]string{"⏎", "pull selected"},
			[2]string{"y", "pull " + starterModel},
			[2]string{"n", "skip"},
			[2]string{"q", "quit"},
		))
	}
	return boxWrap(b.String(), m)
}

// renderPullTabs renders the two-tab heading with the active tab highlighted.
func (m Model) renderPullTabs() string {
	tab := func(i int, label string) string {
		if m.onboard.tab == i {
			return selectedStyle.Render(" " + label + " ")
		}
		return mutedStyle.Render(" " + label + " ")
	}
	return tab(tabTrending, "TRENDING") + "  " + tab(tabRecommended, "RECOMMENDED") + "  " +
		tab(tabManual, "MANUAL ADD") + mutedStyle.Render("    ←→ switch")
}

// renderManualAdd renders the manual model-tag entry field.
func (m Model) renderManualAdd() string {
	var b strings.Builder
	b.WriteString(mutedStyle.Render("paste any Ollama model tag to pull it directly") + "\n\n")
	// Boxed input line with a block cursor, padded so the box has some width.
	shown := m.onboard.manual + "▏"
	if w := 24 - len([]rune(m.onboard.manual)); w > 0 {
		shown += strings.Repeat(" ", w)
	}
	b.WriteString("  " + accentStyle.Render("ollama pull ") + selectedStyle.Render(" "+shown+" ") + "\n")
	if m.onboard.manual == "" {
		b.WriteString("  " + mutedStyle.Render("e.g. qwen3.6:35b-a3b — find tags at ") +
			hyperlink("https://ollama.com/library", accentStyle.Render("ollama.com/library")) + "\n")
	} else {
		b.WriteString("  " + mutedStyle.Render("press ") + keyStyle.Render("[⏎]") + mutedStyle.Render(" to pull") + "\n")
	}
	return b.String()
}

func (m Model) memNote() string {
	ram := runCeilingGB(m.detection)
	if ram <= 0 {
		return "memory unknown"
	}
	s := fmt.Sprintf("run in %.0fGB RAM", ram)
	if v := vramGB(m.detection); v > 0 {
		s += fmt.Sprintf(" · %.0fGB VRAM", v)
	}
	return s
}

// renderTrendingList renders the trending tab: popular tool-capable models that
// fit, estimated at Q4_K_M.
func (m Model) renderTrendingList() string {
	var b strings.Builder
	b.WriteString(mutedStyle.Render("top "+fmt.Sprint(trendingLimit)+" tool-capable models that "+m.memNote()+" at Q4_K_M") + "\n")
	list, _ := m.pullLists()
	if len(list) == 0 {
		b.WriteString(mutedStyle.Render("  (none fit — try " + starterModel + ")\n"))
	}
	for i, e := range list {
		note := e.Note
		if !e.VRAMFit {
			note = strings.TrimSpace(note + " · CPU split (spills past VRAM)")
		}
		line := fmt.Sprintf("%-22s %6.1fGB  %s", e.Tag, e.EstGB, note)
		b.WriteString(rowCursor(i == m.onboard.cursor, line))
	}
	b.WriteString("\n")
	b.WriteString(mutedStyle.Render("OpenCode needs tool-calling; these all support it (gemma3:270m does not)."))
	b.WriteString("\n")
	return b.String()
}

// renderRecommendedList renders the recommended tab from the benchmark
// reference: footprint + CPU/GPU split, fastest (most-on-GPU) first.
func (m Model) renderRecommendedList() string {
	var b strings.Builder
	ref := ""
	if g := m.opts.Recommended.RefGPUGB; g > 0 {
		ref = fmt.Sprintf(" · split measured on a %.0fGB GPU", g)
	}
	b.WriteString(mutedStyle.Render("benchmark picks that "+m.memNote()+ref) + "\n")
	_, list := m.pullLists()
	if len(list) == 0 {
		b.WriteString(mutedStyle.Render("  (none fit your memory — see the Trending tab)\n"))
	}
	for i, e := range list {
		line := fmt.Sprintf("%-22s %5.0fGB  %-15s %s", e.Tag, e.MemGB, splitLabel(e.GPUPercent), e.Note)
		b.WriteString(rowCursor(i == m.onboard.cursor, line))
	}
	b.WriteString("\n")
	b.WriteString(mutedStyle.Render("Edit ~/.config/tuicode/recommended.json to curate this list."))
	b.WriteString("\n")
	return b.String()
}

// rowCursor renders one selectable list row with a cursor marker.
func rowCursor(selected bool, line string) string {
	if selected {
		return accentStyle.Render("▸ ") + accentStyle.Render(line) + "\n"
	}
	return "  " + mutedStyle.Render(line) + "\n"
}
