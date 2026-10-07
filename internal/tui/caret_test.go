package tui

import (
	"strings"
	"testing"

	"github.com/TimelordUK/hit/internal/search"
	"github.com/TimelordUK/hit/internal/store"
)

// T-022: the filter text could only be typed or deleted at its end. Owner 2026-10-04:
// "we can't move the cursor anywhere other than the end". The caret moves now, and typing
// and Backspace work where it is.

func caretModel(t *testing.T, seed string) Model {
	t.Helper()
	return New(store.Build(nil), search.Query{Text: seed, Now: now})
}

func TestCaretStartsAtTheEndOfTheSeed(t *testing.T) {
	if m := caretModel(t, "git st"); m.Caret() != 6 {
		t.Errorf("caret %d, want 6", m.Caret())
	}
}

func TestArrowsMoveTheCaretAndTypingGoesThere(t *testing.T) {
	m := send(caretModel(t, "gt status"), "left", "left", "left", "left", "left", "left", "left", "left", "i")
	if got := m.Query().Text; got != "git status" {
		t.Errorf("text %q, want %q", got, "git status")
	}
	if m.Caret() != 2 {
		t.Errorf("caret %d, want 2 (just after what was typed)", m.Caret())
	}
}

func TestBackspaceDeletesBeforeTheCaret(t *testing.T) {
	m := send(caretModel(t, "giit status"), "ctrl+a", "right", "right", "backspace")
	if got := m.Query().Text; got != "git status" {
		t.Errorf("text %q", got)
	}
	if m.Caret() != 1 {
		t.Errorf("caret %d, want 1", m.Caret())
	}
}

func TestBackspaceAtTheStartDoesNothing(t *testing.T) {
	m := send(caretModel(t, "git"), "ctrl+a", "backspace")
	if m.Query().Text != "git" || m.Caret() != 0 {
		t.Errorf("text %q caret %d", m.Query().Text, m.Caret())
	}
}

func TestCaretStopsAtBothEnds(t *testing.T) {
	m := send(caretModel(t, "ab"), "right", "right")
	if m.Caret() != 2 {
		t.Errorf("past the end: %d", m.Caret())
	}
	m = send(m, "left", "left", "left", "left")
	if m.Caret() != 0 {
		t.Errorf("past the start: %d", m.Caret())
	}
}

func TestCtrlAAndCtrlEJumpToTheEnds(t *testing.T) {
	m := send(caretModel(t, "git status"), "ctrl+a")
	if m.Caret() != 0 {
		t.Errorf("ctrl+a: %d", m.Caret())
	}
	if m = send(m, "ctrl+e"); m.Caret() != 10 {
		t.Errorf("ctrl+e: %d", m.Caret())
	}
}

func TestCtrlArrowsMoveByWord(t *testing.T) {
	m := send(caretModel(t, "git log --oneline"), "ctrl+left")
	if m.Caret() != 8 {
		t.Errorf("ctrl+left from the end: %d, want 8 (start of --oneline)", m.Caret())
	}
	if m = send(m, "ctrl+left", "ctrl+left"); m.Caret() != 0 {
		t.Errorf("two more: %d, want 0", m.Caret())
	}
	if m = send(m, "ctrl+right"); m.Caret() != 3 {
		t.Errorf("ctrl+right: %d, want 3 (end of git)", m.Caret())
	}
}

// Ctrl+U deletes what is before the caret, which is the whole text when the caret is at
// the end, as it was before the caret could move.
func TestCtrlUDeletesBeforeTheCaret(t *testing.T) {
	m := send(caretModel(t, "git status"), "ctrl+u")
	if m.Query().Text != "" || m.Caret() != 0 {
		t.Errorf("at the end: text %q caret %d", m.Query().Text, m.Caret())
	}
	m = send(caretModel(t, "xx git"), "ctrl+left", "ctrl+u")
	if m.Query().Text != "git" || m.Caret() != 0 {
		t.Errorf("mid-text: text %q caret %d", m.Query().Text, m.Caret())
	}
}

// Characters, not bytes: the caret steps over a multi-byte rune in one press.
func TestCaretCountsCharacters(t *testing.T) {
	m := send(caretModel(t, "café"), "left", "backspace")
	if m.Query().Text != "caé" {
		t.Errorf("text %q", m.Query().Text)
	}
}

// Home and End keep moving through the list, which they did first.
func TestHomeAndEndStillMoveTheList(t *testing.T) {
	m := send(testModel(t), "end")
	if m.Cursor() != len(m.Results())-1 {
		t.Errorf("end: row %d", m.Cursor())
	}
	if m.Caret() != 0 {
		t.Errorf("end moved the caret: %d", m.Caret())
	}
}

func TestTheCaretIsDrawnWhereItIs(t *testing.T) {
	m := send(caretModel(t, "git"), "left")
	first := strings.SplitN(m.View(), "\n", 2)[0]
	if !strings.Contains(first, "gi▏t") {
		t.Errorf("query line %q should show the caret between i and t", first)
	}
}

// T-023: Alt+' takes the whole filter literal ⇄ fuzzy, so the quote the finder opens with
// comes out in one key, with the caret still on the same character.
func TestAltQuoteTogglesLiteral(t *testing.T) {
	m := sendAlt(caretModel(t, "'git checkout -b"), '\'')
	if got := m.Query().Text; got != "git checkout -b" {
		t.Errorf("off: text %q", got)
	}
	if m.Caret() != 15 {
		t.Errorf("off: caret %d, want 15", m.Caret())
	}
	m = sendAlt(send(m, "left", "left"), '\'')
	if got := m.Query().Text; got != "'git checkout -b" {
		t.Errorf("on: text %q", got)
	}
	if m.Caret() != 14 {
		t.Errorf("on: caret %d, want 14", m.Caret())
	}
	if !strings.Contains(view(m), "literal") {
		t.Error("header should name literal once the quote is back")
	}
}
