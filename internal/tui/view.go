package tui

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/TimelordUK/hit/internal/search"
	"github.com/charmbracelet/lipgloss"
)

// Styles are resolved once. NO_COLOR (and a dumb terminal) fall back to plain text;
// lipgloss itself degrades to 16 colours where that's all there is.
type styles struct {
	header, scope, badge, dim, sep, cursor lipgloss.Style

	// row and selRow are the two sets a list row draws with. They exist as whole sets,
	// rather than as one set plus a highlight wrapped round the finished line, because
	// wrapping does not work: see rowStyles.
	row, selRow rowStyles

	// noColor is NO_COLOR: category marks are then plain letters, which is why a mark is
	// a letter and not only a colour.
	noColor bool
}

// rowStyles is everything one list row draws with.
//
// A row is built from several styles — the time column, the matched runes, the ⏎ marker —
// and each one ends with a full SGR reset. Wrapping the finished string in a "selected"
// style therefore painted the highlight only as far as the first nested style, which in
// practice was the "▸ " prefix: the one row you need to find with your eye was the one row
// with no highlight on it. So the selected row is drawn from its own set of styles, each
// already carrying the highlight, and nothing is wrapped round the outside (T-010).
type rowStyles struct {
	text, match, meta, marker, dim lipgloss.Style
}

func newStyles() styles {
	if os.Getenv("NO_COLOR") != "" {
		plain := lipgloss.NewStyle()
		bold := lipgloss.NewStyle().Bold(true)
		rev := lipgloss.NewStyle().Reverse(true)
		return styles{
			noColor: true,
			header:  bold, scope: plain, badge: bold, dim: plain, sep: plain, cursor: rev,
			row:    rowStyles{text: plain, match: bold, meta: plain, marker: plain, dim: plain},
			selRow: rowStyles{text: rev, match: rev.Bold(true), meta: rev, marker: rev, dim: rev},
		}
	}
	// The selected row is a bar of one background colour. Every style on it keeps its own
	// job — matches stay the brightest thing, the time stays quieter than the command —
	// but all of them are lifted a step, because a foreground chosen to read as "dim" on
	// the terminal's own background disappears entirely on a coloured one.
	const selBG = lipgloss.Color("24")
	return styles{
		header: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6")),
		scope:  lipgloss.NewStyle().Foreground(lipgloss.Color("5")),
		badge:  lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("0")).Background(lipgloss.Color("5")),
		dim:    lipgloss.NewStyle().Foreground(lipgloss.Color("8")),
		sep:    lipgloss.NewStyle().Foreground(lipgloss.Color("8")),
		cursor: lipgloss.NewStyle().Foreground(lipgloss.Color("6")),
		row: rowStyles{
			text:   lipgloss.NewStyle(),
			match:  lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("3")),
			meta:   lipgloss.NewStyle().Foreground(lipgloss.Color("8")),
			marker: lipgloss.NewStyle().Foreground(lipgloss.Color("4")),
			dim:    lipgloss.NewStyle().Foreground(lipgloss.Color("8")),
		},
		selRow: rowStyles{
			text:   lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15")).Background(selBG),
			match:  lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("11")).Background(selBG),
			meta:   lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Background(selBG),
			marker: lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Background(selBG),
			dim:    lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Background(selBG),
		},
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
	now := m.clock()

	var b strings.Builder
	// The search line: a prompt, what you've typed, a cursor, and how many commands match,
	// so it is obvious that typing filters.
	left := s.header.Render("hit ❯ ") + m.query.Text + s.cursor.Render("▏")
	count := fmt.Sprintf("%d match", len(m.results))
	if len(m.results) != 1 {
		count += "es"
	}
	right := s.scope.Render(count)
	for _, md := range m.modes() {
		// An active mode is a badge, not another word in a row of grey ones. Which mode
		// the finder is in has to be readable at a glance, because the alternative is
		// reading an empty list and concluding the finder is broken (T-008, T-009).
		switch {
		case md.category:
			// The category filter is its own colour, not a badge: the badge's black on
			// magenta was unreadable in schemes where black is itself a dark purple.
			right += "  " + s.categoryMode(md.color).Render(md.text)
		case md.active:
			right += " " + s.badge.Render(" "+md.text+" ")
		default:
			right += "  " + s.scope.Render(md.text)
		}
	}
	if gap := m.width - lipgloss.Width(left) - lipgloss.Width(right); gap > 1 {
		b.WriteString(left + strings.Repeat(" ", gap) + right)
	} else {
		b.WriteString(truncate(left, m.width))
	}
	b.WriteString("\n")

	rows := m.listRows()
	if len(m.results) == 0 {
		b.WriteString(s.dim.Render(truncate("  "+m.emptyHint(), m.width)) + "\n")
	}
	for i := m.top; i < len(m.results) && i < m.top+rows; i++ {
		b.WriteString(m.renderRow(s, i) + "\n")
	}

	if p := m.previewRows(); p > 0 {
		b.WriteString(s.sep.Render(strings.Repeat("─", max(1, m.width))) + "\n")
		b.WriteString(m.renderPreview(s, p, now))
	}
	if line := m.timingLine(); line != "" {
		b.WriteString(s.dim.Render(truncate(line, m.width)) + "\n")
	}
	if m.picking {
		b.WriteString(m.pickerLine(s))
	} else {
		b.WriteString(s.dim.Render(m.statusLine()))
	}
	out := b.String()
	if m.renders == 0 {
		m.logf("first render: %d bytes, %d results, %dx%d", len(out), len(m.results), m.width, m.height)
	}
	return out
}

// markStyle draws a category's mark (T-012). In the list it is the letter in the
// category's colour and nothing more, so a screen of rows stays quiet. On the selected row
// it becomes a chip, the colour behind the letter, because the bar has a background of its
// own and a blue letter on the blue bar would simply vanish.
func (s styles) markStyle(color string, selected bool) lipgloss.Style {
	if s.noColor {
		if selected {
			return lipgloss.NewStyle().Reverse(true).Bold(true)
		}
		return lipgloss.NewStyle()
	}
	c := ansiColor(color)
	if selected {
		return lipgloss.NewStyle().Bold(true).Background(c).Foreground(chipText(color))
	}
	return lipgloss.NewStyle().Foreground(c)
}

// categoryMode draws the active category filter in the header: its name, bold, in the
// category's colour, on no background. A category with no colour keeps the terminal's
// own text colour, which still stands out from the grey words around it.
func (s styles) categoryMode(color string) lipgloss.Style {
	st := lipgloss.NewStyle().Bold(true)
	if s.noColor || color == "" {
		return st
	}
	return st.Foreground(ansiColor(color))
}

// ansiColor maps a configured colour to a terminal one. Names are the 16 ANSI colours, so
// they follow the terminal's own palette; #rrggbb is passed through for lipgloss to fit to
// the terminal. Empty is the quiet default, the same grey as the time column.
func ansiColor(name string) lipgloss.Color {
	if strings.HasPrefix(name, "#") {
		return lipgloss.Color(name)
	}
	if i, ok := ansiIndex[name]; ok {
		return lipgloss.Color(i)
	}
	return lipgloss.Color("8")
}

var ansiIndex = map[string]string{
	"black": "0", "red": "1", "green": "2", "yellow": "3", "blue": "4", "magenta": "5",
	"cyan": "6", "white": "7", "gray": "8", "grey": "8", "bright-black": "8",
	"bright-red": "9", "bright-green": "10", "bright-yellow": "11", "bright-blue": "12",
	"bright-magenta": "13", "bright-cyan": "14", "bright-white": "15",
}

// chipText is the letter's colour on a chip: white on the dark colours, black on the
// light ones, so the letter reads on any of them.
func chipText(name string) lipgloss.Color {
	if strings.HasPrefix(name, "#") && len(name) == 7 {
		var r, g, b int
		fmt.Sscanf(name[1:], "%02x%02x%02x", &r, &g, &b)
		if (299*r+587*g+114*b)/1000 > 140 {
			return lipgloss.Color("0")
		}
		return lipgloss.Color("15")
	}
	switch name {
	case "", "black", "red", "blue", "magenta", "gray", "grey", "bright-black":
		return lipgloss.Color("15")
	}
	return lipgloss.Color("0")
}

// timingLine is the phase report, shown only with HIT_TIMING set. It is deliberately the
// raw numbers: it exists to be read off a screen and pasted into a bug report.
func (m Model) timingLine() string {
	if s := m.Timing.String(); s != "" {
		return "⏱ " + s
	}
	return ""
}

// mode is one indicator in the header: what the finder is currently doing to the list.
// active marks the ones that are narrowing or reordering it, so they can be drawn as
// badges rather than as more grey words.
type mode struct {
	text   string
	active bool
	// category marks the active category filter, drawn in color rather than as a badge.
	category bool
	color    string
}

// modes is what the header says about the current view, left to right.
//
// The scope and the sort order are always shown, default or not. An indicator that only
// appears when it is on teaches you nothing about the key that turns it off, and the
// question these answer — "why am I not seeing what I expected?" — is asked precisely when
// you have forgotten which mode you are in.
func (m Model) modes() []mode {
	scope := string(m.query.Scope)
	if m.query.Scope == search.ScopeDir {
		// Naming the folder, not just the word "dir": in a filter that shows one
		// directory's commands, which directory is the whole of the information.
		if leaf := pathLeaf(m.query.Cwd); leaf != "" {
			scope = "dir " + leaf
		}
	}
	out := []mode{
		{text: scope, active: m.query.Scope != search.ScopeAll},
		{text: string(m.query.Sort), active: m.query.Sort != search.SortRank},
	}
	if strings.HasPrefix(m.query.Text, "'") {
		out = append(out, mode{text: "literal", active: true}) // named, not inferred (T-005)
	}
	if m.query.HideFailed {
		out = append(out, mode{text: "ok-only", active: true})
	}
	if m.hasCategories() {
		// Named permanently once categories exist, like scope and sort; absent when none
		// are configured, because then there is no key to teach.
		if m.catFilter >= 0 {
			r := m.cats.Set.Rules[m.catFilter]
			out = append(out, mode{text: r.Name, active: true, category: true, color: r.Color})
		} else {
			out = append(out, mode{text: "any group"})
		}
	}
	if m.cats != nil && m.cats.Set != nil && len(m.cats.Set.Problems) > 0 {
		// Quiet, but there: a rule that silently stopped working looks like a category
		// that has nothing in it. `hit categories` says what is wrong.
		out = append(out, mode{text: "config ⚠"})
	}
	if n := len(m.order); n > 0 {
		out = append(out, mode{text: fmt.Sprintf("%d deleted", n)})
	}
	return out
}

// pathLeaf is the last segment of a directory, for naming the dir filter. A drive or share
// root has no leaf worth showing, so it keeps the whole path.
func pathLeaf(p string) string {
	p = strings.TrimRight(strings.ReplaceAll(p, "/", `\`), `\`)
	if i := strings.LastIndex(p, `\`); i >= 0 && i < len(p)-1 {
		return p[i+1:]
	}
	return p
}

// emptyHint says why the list is empty and which key widens it.
//
// An empty list with no explanation reads as a broken finder, and the scopes that most
// often come up empty — this directory, this session — are the ones a single keystroke
// lands you in. Saying which key gets you back out is the difference between a filter and
// a dead end.
func (m Model) emptyHint() string {
	if m.catFilter >= 0 && m.hasCategories() {
		return "nothing in " + m.cats.Set.Rules[m.catFilter].Name + " matches — alt+g for the next group"
	}
	switch {
	case m.query.Scope == search.ScopeDir:
		where := pathLeaf(m.query.Cwd)
		if where == "" {
			where = "this folder"
		}
		return "nothing recorded in " + where + " — alt+d searches everywhere"
	case m.query.Scope == search.ScopeSession:
		return "nothing in this shell session yet — ^r widens the scope"
	case m.query.Scope == search.ScopeHost:
		return "nothing on this machine matches — ^r widens the scope"
	case m.query.Text != "":
		return "no matches"
	}
	return "no history yet"
}

// statusLine drops hints from the right as the pane narrows, keeping the position.
func (m Model) statusLine() string {
	pos := "0/0"
	if len(m.results) > 0 {
		pos = fmt.Sprintf("%d/%d", m.cursor+1, len(m.results))
	}
	hints := []string{"↵ insert", "alt+d dir", "alt+s sort", "^r scope", "^x failed",
		"^y yank", "alt+w run there", "tab edit", "del remove", "^z undo", "esc cancel"}
	if m.hasCategories() {
		hints = append(hints[:3], append([]string{"alt+g group"}, hints[3:]...)...)
	}
	for len(hints) > 0 {
		line := pos + "  " + strings.Join(hints, " · ")
		if lipgloss.Width(line) <= m.width {
			return line
		}
		hints = hints[:len(hints)-1]
	}
	return truncate(pos, m.width)
}

// pickerLine replaces the status line while the group picker is open (T-019): each group
// as its mark letter, in its colour as in the rows, then its name. Groups that do not fit
// the width are dropped from the right, as hints are; Alt+G still steps to them.
func (m Model) pickerLine(s styles) string {
	line := s.dim.Render("group ")
	tail := s.dim.Render("  alt+g next · esc done")
	for i, r := range m.cats.Set.Rules {
		name := s.dim.Render(r.Name)
		if i == m.catFilter {
			name = s.categoryMode(r.Color).Render(r.Name) // the one you are on
		}
		entry := s.markStyle(r.Color, false).Render(r.Mark) + " " + name
		if i > 0 {
			entry = s.dim.Render(" · ") + entry
		}
		if lipgloss.Width(line+entry+tail) > m.width {
			break
		}
		line += entry
	}
	return line + tail
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

// timeColWidth is the width of the time column, and of the blank that stands in for an
// entry with no usable timestamp so the commands still line up. timeColMinWidth is the
// pane width below which the column is dropped rather than squeezing the command.
const (
	timeColWidth    = 6
	timeColMinWidth = 40
)

// timeColumn is when a command was run, in one fixed-width column (T-001).
//
// Today shows the clock, because the job is to place a run against the working day — "did
// I run this before or after the deploy?" — and a relative age ("3h ago") answers that
// worse the longer the day goes on. Any other day shows the date instead: the exact minute
// stopped mattering, and which day it was started to.
func timeColumn(t, now time.Time) string {
	if t.IsZero() {
		return strings.Repeat(" ", timeColWidth)
	}
	t = t.In(now.Location())
	layout := "02 Jan"
	if sameDay(t, now) {
		layout = "15:04"
	}
	return fmt.Sprintf("%*s", timeColWidth, t.Format(layout))
}

// clock is "now" for the view: pinned by the query in tests, the real clock otherwise.
// The row and the preview take it from here so they can never disagree about the day.
func (m Model) clock() time.Time {
	if !m.query.Now.IsZero() {
		return m.query.Now
	}
	return time.Now()
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

// renderRow shows a command on one line with the matched runes highlighted, when it was
// run, and a ⏎ +N marker when it spans more lines (DESIGN §6).
func (m Model) renderRow(s styles, i int) string {
	r := m.results[i]
	label, src := rowLabel(r.Entry.Cmd)

	inMatch := map[int]bool{}
	for _, p := range r.Matched {
		inMatch[p] = true
	}

	selected := i == m.cursor
	rs := s.row
	prefix := "  "
	if selected {
		rs, prefix = s.selRow, "▸ "
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
	// The time is the first thing dropped when the pane narrows: it is context, and the
	// command is the thing you came for (T-001).
	stamp := ""
	if m.width >= timeColMinWidth {
		stamp = timeColumn(r.Entry.Time, m.clock()) + "  "
	}
	// The category mark is a column of its own between the time and the command (T-012):
	// one cell, blank on an uncategorised row so every command starts in the same place.
	// It only exists when categories are configured, so without them the row is unchanged.
	var mark string
	var markStyle lipgloss.Style
	if m.hasCategories() {
		mark = " "
		if rule, ok := m.categoryMark(r); ok {
			mark, markStyle = rule.Mark, s.markStyle(rule.Color, selected)
		}
		if stamp != "" {
			stamp = strings.TrimSuffix(stamp, " ") // the mark's own spacing replaces one
		}
	}
	markWidth := 0
	if mark != "" {
		markWidth = 2
	}
	width := max(10, m.width-lipgloss.Width(prefix)-lipgloss.Width(stamp)-markWidth-
		lipgloss.Width(marker)-lipgloss.Width(count))

	// The ellipsis costs a column of its own, so it comes out of the label's room.
	clipped := false
	if len(label) > width {
		n := max(0, width-1)
		label, src, clipped = label[:n], src[:n], true
	}

	var b strings.Builder
	b.WriteString(rs.text.Render(prefix))
	if stamp != "" {
		b.WriteString(rs.meta.Render(stamp))
	}
	if mark != "" {
		if mark == " " {
			b.WriteString(rs.text.Render("  "))
		} else {
			b.WriteString(markStyle.Render(mark) + rs.text.Render(" "))
		}
	}
	// Runs of matched and unmatched runes are rendered a run at a time, not a rune at a
	// time: one escape per run instead of per character, and the command's own text stays
	// contiguous in the output where nothing matched.
	for j := 0; j < len(label); {
		k, on := j, inMatch[src[j]]
		for k < len(label) && inMatch[src[k]] == on {
			k++
		}
		text := string(label[j:k])
		if on {
			b.WriteString(rs.match.Render(text))
		} else {
			b.WriteString(rs.text.Render(text))
		}
		j = k
	}
	if clipped {
		b.WriteString(rs.dim.Render("…"))
	}
	if marker != "" {
		b.WriteString(rs.marker.Render(marker))
	}
	if count != "" {
		b.WriteString(rs.meta.Render(count))
	}
	line := b.String()
	if selected {
		// The bar runs to the edge of the pane, so the selection is a band across the
		// list rather than a highlight that stops wherever the command happens to end.
		if n := m.width - lipgloss.Width(line); n > 0 {
			line += rs.text.Render(strings.Repeat(" ", n))
		}
	}
	return line
}

func (m Model) renderPreview(s styles, rows int, now time.Time) string {
	r, ok := m.Selected()
	if !ok {
		return strings.Repeat("\n", rows)
	}
	var b strings.Builder
	b.WriteString(s.dim.Render(truncate(describe(r, now), m.width)) + "\n")
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
