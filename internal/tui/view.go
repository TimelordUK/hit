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
	header, scope, match, selected, meta, marker, dim, sep, cursor lipgloss.Style
}

func newStyles() styles {
	if os.Getenv("NO_COLOR") != "" {
		plain := lipgloss.NewStyle()
		bold := lipgloss.NewStyle().Bold(true)
		return styles{header: bold, scope: plain, match: bold, selected: lipgloss.NewStyle().Reverse(true),
			meta: plain, marker: plain, dim: plain, sep: plain, cursor: lipgloss.NewStyle().Reverse(true)}
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
		cursor:   lipgloss.NewStyle().Foreground(lipgloss.Color("6")),
	}
}

func (m Model) View() string {
	if m.Choice != nil {
		return "" // done: leave the screen clean for the shell
	}
	// Marked before anything is rendered, so "paint" is the time to the frame you are
	// about to read the number on, not the time to render the number itself.
	if !m.Timing.Marked("paint") {
		m.Timing.Mark("paint")
	}
	s := newStyles()
	now := m.query.Now
	if now.IsZero() {
		now = time.Now()
	}

	var b strings.Builder
	// The search line: a prompt, what you've typed, a cursor, and how many commands match,
	// so it is obvious that typing filters.
	left := s.header.Render("hit ❯ ") + m.query.Text + s.cursor.Render("▏")
	right := fmt.Sprintf("%d", len(m.results))
	if len(m.results) == 1 {
		right += " match"
	} else {
		right += " matches"
	}
	right += "  " + string(m.query.Scope)
	if m.query.HideFailed {
		right += "  ok-only"
	}
	if len(m.order) > 0 {
		right += fmt.Sprintf("  %d deleted", len(m.order))
	}
	right = s.scope.Render(right)
	if gap := m.width - lipgloss.Width(left) - lipgloss.Width(right); gap > 1 {
		b.WriteString(left + strings.Repeat(" ", gap) + right)
	} else {
		b.WriteString(truncate(left, m.width))
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
	if line := m.timingLine(); line != "" {
		b.WriteString(s.meta.Render(truncate(line, m.width)) + "\n")
	}
	b.WriteString(s.dim.Render(m.statusLine()))
	out := b.String()
	if m.renders == 0 {
		m.logf("first render: %d bytes, %d results, %dx%d", len(out), len(m.results), m.width, m.height)
	}
	return out
}

// timingLine is the phase report, shown only with HIT_TIMING set. It is deliberately the
// raw numbers: it exists to be read off a screen and pasted into a bug report.
func (m Model) timingLine() string {
	if s := m.Timing.String(); s != "" {
		return "⏱ " + s
	}
	return ""
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

// rowLabel is the one-line form of a command for the list.
//
// A multi-line command is flattened — every run of whitespace becomes one space — because
// its first line is so often a bare opener (`& {`, `foreach ($x in $y) {`) that every
// command of that shape renders identically and the list becomes unreadable. A single-line
// command is returned exactly as it is: the view may reformat (principle 3), but there is
// nothing to gain by collapsing spaces inside a command that already fits on one line.
//
// The second return is, for each rune of the label, the index of the rune it came from in
// cmd, so the match highlighting still lands on the right characters — including matches
// on lines that the old first-line-only row could never show.
func rowLabel(cmd string) ([]rune, []int) {
	runes := []rune(cmd)
	if !strings.ContainsAny(cmd, "\n\r\t") {
		src := make([]int, len(runes))
		for i := range src {
			src[i] = i
		}
		return runes, src
	}
	out := make([]rune, 0, len(runes))
	src := make([]int, 0, len(runes))
	inGap := true // starts true, so leading whitespace is dropped rather than shown
	for i, r := range runes {
		if r == '\n' || r == '\r' || r == '\t' || r == ' ' {
			if !inGap {
				out = append(out, ' ')
				src = append(src, i)
				inGap = true
			}
			continue
		}
		out = append(out, r)
		src = append(src, i)
		inGap = false
	}
	if n := len(out); n > 0 && out[n-1] == ' ' {
		out, src = out[:n-1], src[:n-1]
	}
	return out, src
}

// renderRow shows a command on one line with the matched runes highlighted, and a ⏎ +N
// marker when it spans more (DESIGN §6).
func (m Model) renderRow(s styles, i int) string {
	r := m.results[i]
	label, src := rowLabel(r.Entry.Cmd)

	inMatch := map[int]bool{}
	for _, p := range r.Matched {
		inMatch[p] = true
	}

	prefix := "  "
	if i == m.cursor {
		prefix = "▸ "
	}
	// The markers are what the row is *for* once it no longer fits, so they are measured
	// first and the command gets the room that is left. Flattened labels are long enough
	// that appending these afterwards would run the row past the pane.
	var marker, count string
	if n := strings.Count(r.Entry.Cmd, "\n"); n > 0 {
		marker = " " + fmt.Sprintf("⏎ +%d", n)
	}
	if r.Count > 1 {
		count = fmt.Sprintf("  ×%d", r.Count)
	}
	width := max(10, m.width-lipgloss.Width(prefix)-lipgloss.Width(marker)-lipgloss.Width(count))

	// The ellipsis costs a column of its own, so it comes out of the label's room.
	clipped := false
	if len(label) > width {
		n := max(0, width-1)
		label, src, clipped = label[:n], src[:n], true
	}

	var b strings.Builder
	b.WriteString(prefix)
	for j, ru := range label {
		if inMatch[src[j]] {
			b.WriteString(s.match.Render(string(ru)))
		} else {
			b.WriteString(string(ru))
		}
	}
	if clipped {
		b.WriteString(s.dim.Render("…"))
	}
	if marker != "" {
		b.WriteString(s.marker.Render(marker))
	}
	if count != "" {
		b.WriteString(s.meta.Render(count))
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
