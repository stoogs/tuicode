package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// lipglossWidth returns the rendered cell width of s (ANSI-aware).
func lipglossWidth(s string) int { return lipgloss.Width(s) }

// padBetween places left and right on one line padded to width w.
func padBetween(left, right string, w int) string {
	gap := w - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

// boxWrap is a hook for an optional outer border. Kept border-free to avoid
// ANSI width math fighting the terminal; structure comes from headers/dividers.
func boxWrap(s string, m Model) string { return s }

const gib = 1024 * 1024 * 1024

// fmtGB formats bytes as a GB string, e.g. "9.9GB".
func fmtGB(bytes int64) string {
	gb := float64(bytes) / gib
	return fmt.Sprintf("%.1fGB", gb)
}

// fmtGBf formats a GB float, e.g. "9.9".
func fmtGBf(gb float64) string {
	return fmt.Sprintf("%.1f", gb)
}

// fmtCtx formats a context length compactly, e.g. 65536 → "65536".
func fmtCtx(n int) string {
	if n <= 0 {
		return "default"
	}
	return fmt.Sprintf("%d", n)
}
