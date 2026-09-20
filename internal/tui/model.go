// Package tui is the finder (DESIGN §6). Update(msg) → model is pure, so tests drive it
// with key messages and no terminal; rendering is checked separately.
package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/TimelordUK/hit/internal/search"
	"github.com/TimelordUK/hit/internal/store"
	tea "github.com/charmbracelet/bubbletea"
)

// Action is what the shell should do with the result (the finder's half of the handoff).
type Action string

const (
	ActionInsert Action = "insert" // put the command in the prompt, don't run it
	ActionEdit   Action = "edit"   // put it in the prompt and keep the finder's text
	ActionCancel Action = "cancel" // leave the prompt untouched
)

// Choice is what the key handler reads back.
type Choice struct {
	Action  Action   `json:"action"`
	Cmd     string   `json:"cmd,omitempty"`
	ID      string   `json:"id,omitempty"`
	Deleted []string `json:"deleted,omitempty"` // ids to tombstone
}

// Model is the finder's state.
type Model struct {
	history *store.History
	query   search.Query
	results []search.Result
	cursor  int
	top     int // first visible row
	deleted map[string]bool
	order   []string // deletion order, so undo removes the last one

	width, height int
	Choice        *Choice // set when the finder is done

	// Log, when set, records what the finder receives and draws. The finder runs inside
	// a key handler where nothing is visible, so this is the only way to see it work.
	Log func(format string, args ...any)

	renders int
}

func (m *Model) logf(format string, args ...any) {
	if m.Log != nil {
		m.Log(format, args...)
	}
}

// New builds a finder over h. q carries the seed text, scope and context.
func New(h *store.History, q search.Query) Model {
	m := Model{history: h, query: q, deleted: map[string]bool{}, width: 80, height: 24}
	m.refresh()
	return m
}

func (m Model) Init() tea.Cmd { return nil }

// Query returns the current query (tests and the view read it).
func (m Model) Query() search.Query { return m.query }

// Results returns the current, visible results.
func (m Model) Results() []search.Result { return m.results }

// Cursor returns the selected row.
func (m Model) Cursor() int { return m.cursor }

// Selected returns the highlighted result, if there is one.
func (m Model) Selected() (search.Result, bool) {
	if m.cursor < 0 || m.cursor >= len(m.results) {
		return search.Result{}, false
	}
	return m.results[m.cursor], true
}

func (m *Model) refresh() {
	all := search.Search(m.history, m.query)
	m.results = all[:0:0]
	for _, r := range all {
		if !m.deleted[r.Entry.ID] {
			m.results = append(m.results, r)
		}
	}
	m.clampCursor()
}

func (m *Model) clampCursor() {
	if m.cursor >= len(m.results) {
		m.cursor = len(m.results) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	if rows := m.listRows(); rows > 0 {
		if m.cursor < m.top {
			m.top = m.cursor
		}
		if m.cursor >= m.top+rows {
			m.top = m.cursor - rows + 1
		}
		if m.top < 0 {
			m.top = 0
		}
	}
}

func (m *Model) finish(a Action) {
	c := &Choice{Action: a, Deleted: m.order}
	if a != ActionCancel {
		if r, ok := m.Selected(); ok {
			c.Cmd, c.ID = r.Entry.Cmd, r.Entry.ID
		} else {
			c.Action = ActionCancel
		}
	}
	m.Choice = c
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.logf("size %dx%d", msg.Width, msg.Height)
		m.width, m.height = msg.Width, msg.Height
		m.clampCursor()
		return m, nil
	case tea.KeyMsg:
		m.logf("key %q", msg.String())
		return m.handleKey(msg)
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c":
		m.finish(ActionCancel)
		return m, tea.Quit
	case "enter":
		m.finish(ActionInsert)
		return m, tea.Quit
	case "tab":
		m.finish(ActionEdit)
		return m, tea.Quit
	case "up":
		m.cursor--
		m.clampCursor()
	case "down":
		m.cursor++
		m.clampCursor()
	case "pgup":
		m.cursor -= m.listRows()
		m.clampCursor()
	case "pgdown":
		m.cursor += m.listRows()
		m.clampCursor()
	case "home":
		m.cursor = 0
		m.clampCursor()
	case "end":
		m.cursor = len(m.results) - 1
		m.clampCursor()
	case "ctrl+r": // cycle scope: dir → session → host → all
		m.query.Scope = nextScope(m.query.Scope)
		m.cursor, m.top = 0, 0
		m.refresh()
	case "ctrl+x": // hide/show failed commands
		m.query.HideFailed = !m.query.HideFailed
		m.refresh()
	case "delete": // tombstone, undone with ctrl+z while the finder is open
		if r, ok := m.Selected(); ok && r.Entry.ID != "" {
			m.deleted[r.Entry.ID] = true
			m.order = append(m.order, r.Entry.ID)
			m.refresh()
		}
	case "ctrl+z":
		if n := len(m.order); n > 0 {
			delete(m.deleted, m.order[n-1])
			m.order = m.order[:n-1]
			m.refresh()
		}
	case "backspace":
		if r := []rune(m.query.Text); len(r) > 0 {
			m.query.Text = string(r[:len(r)-1])
			m.cursor, m.top = 0, 0
			m.refresh()
		}
	case "ctrl+u":
		m.query.Text = ""
		m.cursor, m.top = 0, 0
		m.refresh()
	case "space", " ":
		m.query.Text += " "
		m.cursor, m.top = 0, 0
		m.refresh()
	default:
		if msg.Type == tea.KeyRunes && len(msg.Runes) > 0 {
			m.query.Text += string(msg.Runes)
			m.cursor, m.top = 0, 0
			m.refresh()
		}
	}
	return m, nil
}

func nextScope(s search.Scope) search.Scope {
	for i, v := range search.Scopes {
		if v == s {
			return search.Scopes[(i+1)%len(search.Scopes)]
		}
	}
	return search.Scopes[0]
}

// Layout: one header line, the list, a separator, the preview, the status line. The
// preview is capped by the pane height and sized to the selected command, and it
// disappears when the pane is too small for it (F-020). Nothing is padded with blanks.
func (m Model) maxPreviewRows() int {
	switch {
	case m.height < 10:
		return 0
	case m.height < 16:
		return 3
	}
	return 6
}

// previewRows is how many rows the preview actually takes: one metadata line plus the
// command's lines, capped.
func (m Model) previewRows() int {
	limit := m.maxPreviewRows()
	if limit == 0 {
		return 0
	}
	r, ok := m.Selected()
	if !ok {
		return 0
	}
	n := 1 + strings.Count(r.Entry.Cmd, "\n") + 1
	if n > limit {
		return limit
	}
	return n
}

func (m Model) listRows() int {
	rows := m.height - 2 // header + status line
	if p := m.previewRows(); p > 0 {
		rows -= p + 1 // preview plus its separator
	}
	if rows < 1 {
		return 1
	}
	return rows
}

// describe is the metadata line for a result: how often, when, where, how it ended.
func describe(r search.Result, now time.Time) string {
	parts := []string{}
	if r.Count > 1 {
		parts = append(parts, fmt.Sprintf("×%d", r.Count))
	}
	if !r.Entry.Time.IsZero() {
		parts = append(parts, humanAge(now.Sub(r.Entry.Time)))
	}
	if r.Entry.Exit != nil && *r.Entry.Exit != 0 {
		parts = append(parts, fmt.Sprintf("exit %d", *r.Entry.Exit))
	}
	if r.Entry.Ms != nil && *r.Entry.Ms >= 1000 {
		parts = append(parts, fmt.Sprintf("%.1fs", float64(*r.Entry.Ms)/1000))
	}
	if r.Entry.Cwd != "" {
		parts = append(parts, r.Entry.Cwd)
	}
	return strings.Join(parts, "  ")
}

func humanAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	default:
		return fmt.Sprintf("%dw ago", int(d.Hours()/24/7))
	}
}
