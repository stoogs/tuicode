package tui

import "github.com/charmbracelet/lipgloss"

var (
	colAccent = lipgloss.Color("12") // blue
	colGood   = lipgloss.Color("10") // green
	colWarn   = lipgloss.Color("11") // yellow
	colBad    = lipgloss.Color("9")  // red
	colMuted  = lipgloss.Color("8")  // grey
	colFg     = lipgloss.Color("15")

	titleStyle   = lipgloss.NewStyle().Bold(true).Foreground(colAccent)
	headerStyle  = lipgloss.NewStyle().Foreground(colMuted)
	sectionStyle = lipgloss.NewStyle().Bold(true).Foreground(colFg)
	mutedStyle   = lipgloss.NewStyle().Foreground(colMuted)
	goodStyle    = lipgloss.NewStyle().Foreground(colGood)
	warnStyle    = lipgloss.NewStyle().Foreground(colWarn)
	badStyle     = lipgloss.NewStyle().Foreground(colBad)
	accentStyle  = lipgloss.NewStyle().Foreground(colAccent)

	keyStyle  = lipgloss.NewStyle().Bold(true).Foreground(colAccent)
	helpStyle = lipgloss.NewStyle().Foreground(colMuted)

	selectedStyle = lipgloss.NewStyle().Bold(true).Foreground(colFg).Background(lipgloss.Color("236"))

	dividerStyle = lipgloss.NewStyle().Foreground(colMuted)
)

// dot returns a colored status dot.
func dot(up bool) string {
	if up {
		return goodStyle.Render("●")
	}
	return mutedStyle.Render("○")
}

// divider renders a horizontal rule of the given width.
func divider(width int) string {
	if width < 1 {
		width = 1
	}
	s := make([]byte, width)
	for i := range s {
		s[i] = '-'
	}
	return dividerStyle.Render(string(s))
}

// progressBar renders a [▓▓░░] style bar of the given cell width for frac [0,1].
func progressBar(frac float64, width int) string {
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}
	if width < 1 {
		width = 1
	}
	filled := int(frac*float64(width) + 0.5)
	if filled > width {
		filled = width
	}
	var b []rune
	for i := 0; i < width; i++ {
		if i < filled {
			b = append(b, '▓')
		} else {
			b = append(b, '░')
		}
	}
	style := goodStyle
	if frac > 0.85 {
		style = badStyle
	} else if frac > 0.7 {
		style = warnStyle
	}
	return style.Render(string(b))
}

var usedBlockStyle = lipgloss.NewStyle().Foreground(colFg) // white = already used

// stackedBar renders a memory bar: white ▓ for the already-used fraction, then
// (optionally) a projected fraction in ▒ (accent, or red if it would overflow),
// then dim ░ for free space.
func stackedBar(usedFrac, projFrac float64, width int) string {
	clamp01 := func(f float64) float64 {
		if f < 0 {
			return 0
		}
		if f > 1 {
			return 1
		}
		return f
	}
	if width < 1 {
		width = 1
	}
	usedN := int(clamp01(usedFrac)*float64(width) + 0.5)
	if usedN > width {
		usedN = width
	}
	projN := int(projFrac*float64(width) + 0.5)
	overflow := false
	if usedN+projN > width {
		projN = width - usedN
		overflow = true
	}
	if projN < 0 {
		projN = 0
	}
	freeN := width - usedN - projN

	rep := func(n int, r rune) string {
		out := make([]rune, n)
		for i := range out {
			out[i] = r
		}
		return string(out)
	}
	projStyle := accentStyle
	if overflow {
		projStyle = badStyle
	}
	return usedBlockStyle.Render(rep(usedN, '▓')) +
		projStyle.Render(rep(projN, '▒')) +
		dividerStyle.Render(rep(freeN, '░'))
}
