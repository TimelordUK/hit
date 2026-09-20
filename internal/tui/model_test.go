package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/TimelordUK/hit/internal/record"
	"github.com/TimelordUK/hit/internal/search"
	"github.com/TimelordUK/hit/internal/store"
	tea "github.com/charmbracelet/bubbletea"
)

var now = time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)

func ts(mins int) string {
	return record.FormatTime(now.Add(-time.Duration(mins) * time.Minute))
}

func testModel(t *testing.T, extra ...record.Record) Model {
	t.Helper()
	recs := []record.Record{
		{K: record.KindCmd, ID: "A", TS: ts(50), Cmd: "git status", Cwd: `C:\dev\hit`, Sh: "pwsh", Sid: "s1"},
		{K: record.KindCmd, ID: "B", TS: ts(40), Cmd: "go test ./...", Cwd: `C:\dev\hit`, Sh: "pwsh", Sid: "s1"},
		{K: record.KindCmd, ID: "C", TS: ts(30), Cmd: "Invoke-RestMethod `\n    -Uri https://elastic-prod-1:9200/_search `\n    -Method Post", Cwd: `\\elastic-prod-1\logs`, Sh: "pwsh", Sid: "s2"},
		{K: record.KindCmd, ID: "D", TS: ts(20), Cmd: "git stash pop", Cwd: `C:\dev\hit`, Sh: "pwsh", Sid: "s1"},
	}
	one := 1
	recs = append(recs, record.Record{K: record.KindEnd, ID: "D", Exit: &one})
	recs = append(recs, extra...)
	return New(store.Build(recs), search.Query{Scope: search.ScopeAll, Cwd: `C:\dev\hit`, Session: "s1", Now: now})
}

// key builds the key message bubbletea would deliver.
func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	case "delete":
		return tea.KeyMsg{Type: tea.KeyDelete}
	case "space":
		return tea.KeyMsg{Type: tea.KeySpace}
	case "ctrl+r":
		return tea.KeyMsg{Type: tea.KeyCtrlR}
	case "ctrl+x":
		return tea.KeyMsg{Type: tea.KeyCtrlX}
	case "ctrl+z":
		return tea.KeyMsg{Type: tea.KeyCtrlZ}
	case "ctrl+u":
		return tea.KeyMsg{Type: tea.KeyCtrlU}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

// send feeds keys (runes go one at a time, as typing does).
func send(m Model, keys ...string) Model {
	for _, k := range keys {
		var next tea.Model = m
		if len(k) > 1 && key(k).Type == tea.KeyRunes {
			for _, r := range k {
				next, _ = next.(Model).Update(key(string(r)))
			}
		} else {
			next, _ = m.Update(key(k))
		}
		m = next.(Model)
	}
	return m
}

func cmds(m Model) []string {
	var out []string
	for _, r := range m.Results() {
		out = append(out, r.Entry.Cmd)
	}
	return out
}

func TestTypingFiltersAndBackspaceRestores(t *testing.T) {
	m := testModel(t)
	if len(m.Results()) != 4 {
		t.Fatalf("start: %v", cmds(m))
	}
	m = send(m, "git")
	if got := cmds(m); len(got) != 2 {
		t.Fatalf(`after "git": %v`, got)
	}
	m = send(m, "backspace", "backspace", "backspace")
	if len(m.Results()) != 4 {
		t.Fatalf("after backspace: %v", cmds(m))
	}
	m = send(m, "git", "space", "st")
	if m.Query().Text != "git st" {
		t.Fatalf("query = %q", m.Query().Text)
	}
	m = send(m, "ctrl+u")
	if m.Query().Text != "" || len(m.Results()) != 4 {
		t.Fatalf("ctrl+u: %q %v", m.Query().Text, cmds(m))
	}
}

func TestEnterInsertsSelectedCommandVerbatim(t *testing.T) {
	m := send(testModel(t), "rest")
	sel, ok := m.Selected()
	if !ok || !strings.HasPrefix(sel.Entry.Cmd, "Invoke-RestMethod") {
		t.Fatalf("selection = %+v", sel)
	}
	m = send(m, "enter")
	if m.Choice == nil || m.Choice.Action != ActionInsert {
		t.Fatalf("choice = %+v", m.Choice)
	}
	if m.Choice.Cmd != sel.Entry.Cmd || !strings.Contains(m.Choice.Cmd, "\n") {
		t.Errorf("multi-line command not returned verbatim: %q", m.Choice.Cmd)
	}
	if m.Choice.ID != "C" {
		t.Errorf("id = %q", m.Choice.ID)
	}
}

func TestTabEditsAndEscCancels(t *testing.T) {
	if m := send(testModel(t), "tab"); m.Choice == nil || m.Choice.Action != ActionEdit {
		t.Errorf("tab: %+v", m.Choice)
	}
	m := send(testModel(t), "esc")
	if m.Choice == nil || m.Choice.Action != ActionCancel || m.Choice.Cmd != "" {
		t.Errorf("esc: %+v", m.Choice)
	}
}

func TestEnterWithNoMatchesCancels(t *testing.T) {
	m := send(testModel(t), "zzzz", "enter")
	if m.Choice == nil || m.Choice.Action != ActionCancel {
		t.Fatalf("choice = %+v", m.Choice)
	}
}

func TestCursorMovementStaysInRange(t *testing.T) {
	m := testModel(t)
	m = send(m, "up") // already at the top
	if m.Cursor() != 0 {
		t.Errorf("cursor = %d", m.Cursor())
	}
	for i := 0; i < 10; i++ {
		m = send(m, "down")
	}
	if m.Cursor() != len(m.Results())-1 {
		t.Errorf("cursor = %d of %d", m.Cursor(), len(m.Results()))
	}
	m = send(m, "git") // filtering resets to the top
	if m.Cursor() != 0 {
		t.Errorf("cursor after filter = %d", m.Cursor())
	}
}

func TestScopeCycleAndFailedToggle(t *testing.T) {
	m := testModel(t)
	m.query.Scope = search.ScopeDir
	m.refresh()
	for _, want := range []search.Scope{search.ScopeSession, search.ScopeHost, search.ScopeAll, search.ScopeDir} {
		m = send(m, "ctrl+r")
		if m.Query().Scope != want {
			t.Fatalf("scope = %q, want %q", m.Query().Scope, want)
		}
	}
	m = send(m, "ctrl+x")
	if !m.Query().HideFailed {
		t.Fatal("ctrl+x did not hide failed commands")
	}
	for _, c := range cmds(m) {
		if c == "git stash pop" {
			t.Error("failed command still listed")
		}
	}
	if m = send(m, "ctrl+x"); m.Query().HideFailed {
		t.Error("ctrl+x did not toggle back")
	}
}

func TestDeleteAndUndo(t *testing.T) {
	m := send(testModel(t), "git")
	first, _ := m.Selected()
	m = send(m, "delete")
	for _, c := range cmds(m) {
		if c == first.Entry.Cmd {
			t.Fatal("deleted command still listed")
		}
	}
	m = send(m, "ctrl+z")
	if len(m.Results()) != 2 {
		t.Fatalf("undo did not restore: %v", cmds(m))
	}
	m = send(m, "delete", "enter")
	if len(m.Choice.Deleted) != 1 || m.Choice.Deleted[0] != first.Entry.ID {
		t.Fatalf("deleted ids = %v", m.Choice.Deleted)
	}
}

func TestViewShowsMultiLineMarkerAndPreview(t *testing.T) {
	m := send(testModel(t), "rest")
	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	v := tm.(Model).View()
	if !strings.Contains(v, "⏎ +2") {
		t.Errorf("missing multi-line marker:\n%s", v)
	}
	if !strings.Contains(v, "-Method Post") {
		t.Errorf("preview does not show the rest of the command:\n%s", v)
	}
	if !strings.Contains(v, `\\elastic-prod-1\logs`) {
		t.Errorf("preview does not show the directory:\n%s", v)
	}
	if strings.Contains(v, "\n\n\n") {
		t.Errorf("view has empty gaps:\n%s", v)
	}
}

// The search line has to make it obvious that typing filters.
func TestViewShowsTheQueryAndLiveMatchCount(t *testing.T) {
	m := testModel(t)
	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = tm.(Model)

	if v := m.View(); !strings.Contains(v, "hit ❯ ") || !strings.Contains(v, "4 matches") {
		t.Errorf("empty query:\n%s", v)
	}
	m = send(m, "git")
	v := m.View()
	if !strings.Contains(v, "hit ❯ git") {
		t.Errorf("typed text not shown:\n%s", v)
	}
	if !strings.Contains(v, "2 matches") {
		t.Errorf("match count did not follow the filter:\n%s", v)
	}
	if m = send(m, " stash"); !strings.Contains(m.View(), "1 match ") {
		t.Errorf("singular match count:\n%s", m.View())
	}
}

func TestViewFitsSmallPanes(t *testing.T) {
	for _, size := range []struct{ w, h int }{{80, 24}, {40, 12}, {30, 8}, {20, 5}} {
		m := testModel(t)
		var tm tea.Model = m
		tm, _ = tm.Update(tea.WindowSizeMsg{Width: size.w, Height: size.h})
		v := tm.(Model).View()
		lines := strings.Split(strings.TrimRight(v, "\n"), "\n")
		if len(lines) > size.h {
			t.Errorf("%dx%d: %d lines, too tall", size.w, size.h, len(lines))
		}
		for i, l := range lines {
			if w := lipglossWidth(l); w > size.w {
				t.Errorf("%dx%d: line %d is %d wide", size.w, size.h, i, w)
			}
		}
	}
}
