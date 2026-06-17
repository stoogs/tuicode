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

type onboardState struct {
	dismissed bool
	pulling   bool
	done      bool
	pullTag   string
	progress  server.PullProgress
	lastErr   string
	cursor    int // selected row in the trending list
	ch        chan tea.Msg
}

const trendingLimit = 10

func newOnboardState() onboardState { return onboardState{} }

// pull messages
type pullProgressMsg struct{ p server.PullProgress }
type pullDoneMsg struct{ err error }

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
	list := fittingModels(m.detection, trendingLimit)
	switch msg.String() {
	case "up", "k":
		if m.onboard.cursor > 0 {
			m.onboard.cursor--
		}
		return m, nil
	case "down", "j":
		if m.onboard.cursor < len(list)-1 {
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
		// Pull the highlighted trending model (or the starter if list empty).
		tag := starterModel
		if m.onboard.cursor >= 0 && m.onboard.cursor < len(list) {
			tag = list[m.onboard.cursor].Tag
		}
		return m.startPull(tag)
	case "y":
		// Quick: pull the small starter regardless of selection.
		return m.startPull(starterModel)
	case "n", "esc", "q":
		m.onboard.dismissed = true
		m.screen = screenDashboard
		return m, m.listCmd()
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

	// Trending, tool-capable models that fit in the detected memory at Q4_K_M.
	mem := m.detection.Authoritative()
	memNote := "memory unknown"
	if mem.Total > 0 {
		memNote = fmt.Sprintf("fit in %.0fGB %s at Q4_K_M", float64(mem.Total)/gib, strings.ToUpper(mem.Source))
	}
	b.WriteString(sectionStyle.Render("TRENDING MODELS") + mutedStyle.Render("  (top "+fmt.Sprint(trendingLimit)+" that "+memNote+")"))
	b.WriteString("\n")
	b.WriteString(divider(min(m.contentWidth(), 70)))
	b.WriteString("\n")

	list := fittingModels(m.detection, trendingLimit)
	if len(list) == 0 {
		b.WriteString(mutedStyle.Render("  (none fit — try " + starterModel + ")\n"))
	}
	for i, e := range list {
		cursor := "  "
		line := fmt.Sprintf("%-22s %6.1fGB  %s", e.Tag, e.EstGB, e.Note)
		if i == m.onboard.cursor {
			cursor = accentStyle.Render("▸ ")
			line = accentStyle.Render(line)
		} else {
			line = mutedStyle.Render(line)
		}
		b.WriteString(cursor + line + "\n")
	}
	b.WriteString("\n")
	b.WriteString(mutedStyle.Render("OpenCode needs tool-calling; these all support it (gemma3:270m does not)."))
	b.WriteString("\n\n")

	if !m.daemon.Reachable {
		b.WriteString(badStyle.Render("Ollama daemon not reachable — start it first: ") +
			accentStyle.Render("sudo systemctl start ollama"))
		b.WriteString("\n\n")
	}
	b.WriteString(footer(
		[2]string{"↑↓", "select"},
		[2]string{"⏎", "pull selected"},
		[2]string{"y", "pull " + starterModel},
		[2]string{"n", "skip"},
		[2]string{"q", "quit"},
	))
	return boxWrap(b.String(), m)
}
