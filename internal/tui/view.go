package tui

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// Styles are resolved once. NO_COLOR (and a dumb terminal) fall back to plain text;
// lipgloss itself degrades to 16 colours where that's all there is.
type styles struct {
	header, scope, match, selected, meta, marker, dim, sep lipgloss.Style
}

func newStyles() styles {
	if os.Getenv("NO_COLOR") != "" {
		plain := lipgloss.NewStyle()
		bold := lipgloss.NewStyle().Bold(true)
		return styles{header: bold, scope: plain, match: bold, selected: lipgloss.NewStyle().Reverse(true),
			meta: plain, marker: plain, dim: plain, sep: plain}
	}
	return styles{
		header:   lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6")),
		scope:    lipgloss.NewStyle().Foreground(lipgloss.Color("5")),
		match:    lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("3")),
		selected: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15")).Background(lipgloss.Color("24")),
		meta:     lipgloss.NewStyle().Foreground(lipgloss.Color("8")),
		marker:   lipgloss.NewStyle().Foreground(lipgloss.Color("4")),
		dim:      lipgloss.NewStyle().Foreground(lipgloss.Color("8")),
		sep:      lipgloss.NewStyle().Foreground(lipgloss.Color("8")),
	}
}

func (m Model) View() string {
	if m.Choice != nil {
		return "" // done: leave the screen clean for the shell
	}
	s := newStyles()
	now := m.query.Now
	if now.IsZero() {
		now = time.Now()
	}

	var b strings.Builder
	b.WriteString(s.header.Render("hit") + " " + m.query.Text + s.dim.Render("▏") + "  " +
		s.scope.Render("["+string(m.query.Scope)+"]"))
	if m.query.HideFailed {
		b.WriteString(s.dim.Render("  ok-only"))
	}
	if len(m.order) > 0 {
		b.WriteString(s.dim.Render(fmt.Sprintf("  %d deleted", len(m.order))))
	}
	b.WriteString("\n")

	rows := m.listRows()
	if len(m.results) == 0 {
		b.WriteString(s.dim.Render("  no matches") + "\n")
	}
	for i := m.top; i < len(m.results) && i < m.top+rows; i++ {
		b.WriteString(m.renderRow(s, i) + "\n")
	}

	if p := m.previewRows(); p > 0 {
		b.WriteString(s.sep.Render(strings.Repeat("─", max(1, m.width))) + "\n")
		b.WriteString(m.renderPreview(s, p, now))
	}
	b.WriteString(s.dim.Render(m.statusLine()))
	out := b.String()
	if m.renders == 0 {
		m.logf("first render: %d bytes, %d results, %dx%d", len(out), len(m.results), m.width, m.height)
	}
	return out
}

// statusLine drops hints from the right as the pane narrows, keeping the position.
func (m Model) statusLine() string {
	pos := "0/0"
	if len(m.results) > 0 {
		pos = fmt.Sprintf("%d/%d", m.cursor+1, len(m.results))
	}
	hints := []string{"↵ insert", "tab edit", "^r scope", "^x failed", "del remove", "^z undo", "esc cancel"}
	for len(hints) > 0 {
		line := pos + "  " + strings.Join(hints, " · ")
		if lipgloss.Width(line) <= m.width {
			return line
		}
		hints = hints[:len(hints)-1]
	}
	return truncate(pos, m.width)
}

// renderRow shows the first line of a command with the matched runes highlighted,
// and a ⏎ +N marker when there are more lines (DESIGN §6).
func (m Model) renderRow(s styles, i int) string {
	r := m.results[i]
	lines := strings.Split(r.Entry.Cmd, "\n")
	first := []rune(strings.TrimRight(lines[0], "\r"))

	inMatch := map[int]bool{}
	for _, p := range r.Matched {
		inMatch[p] = true
	}

	prefix := "  "
	if i == m.cursor {
		prefix = "▸ "
	}
	width := max(10, m.width-4)
	var b strings.Builder
	b.WriteString(prefix)
	for j, ru := range first {
		if j >= width {
			b.WriteString(s.dim.Render("…"))
			break
		}
		if inMatch[j] {
			b.WriteString(s.match.Render(string(ru)))
		} else {
			b.WriteString(string(ru))
		}
	}
	if n := len(lines) - 1; n > 0 {
		b.WriteString(" " + s.marker.Render(fmt.Sprintf("⏎ +%d", n)))
	}
	if r.Count > 1 {
		b.WriteString(s.meta.Render(fmt.Sprintf("  ×%d", r.Count)))
	}
	line := b.String()
	if i == m.cursor {
		return s.selected.Render(pad(line, m.width))
	}
	return line
}

func (m Model) renderPreview(s styles, rows int, now time.Time) string {
	r, ok := m.Selected()
	if !ok {
		return strings.Repeat("\n", rows)
	}
	var b strings.Builder
	b.WriteString(s.meta.Render(truncate(describe(r, now), m.width)) + "\n")
	lines := strings.Split(r.Entry.Cmd, "\n")
	shown := rows - 1
	for i := 0; i < shown && i < len(lines); i++ {
		if i == shown-1 && len(lines) > shown {
			b.WriteString(truncate(strings.TrimRight(lines[i], "\r"), m.width-6) +
				s.dim.Render(fmt.Sprintf(" … +%d", len(lines)-shown)))
		} else {
			b.WriteString(truncate(strings.TrimRight(lines[i], "\r"), m.width))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func pad(s string, width int) string {
	if n := width - lipgloss.Width(s); n > 0 {
		return s + strings.Repeat(" ", n)
	}
	return s
}

func truncate(s string, width int) string {
	if width < 1 {
		return ""
	}
	r := []rune(s)
	if len(r) <= width {
		return s
	}
	return string(r[:width-1]) + "…"
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// lipglossWidth is the rendered width of a line, ignoring ANSI escapes (used by tests).
func lipglossWidth(s string) int { return lipgloss.Width(s) }
