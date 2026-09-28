package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// Alt chords belong to the shell, not to the finder: Alt+M toggles one line / many lines
// at the prompt (S-004, S-023). Inside the finder they were arriving as plain runes and
// being typed into the filter, so pressing Alt+M here silently narrowed the list to
// whatever matched "m" — the key looked like it did nothing, and quietly did the wrong
// thing instead.
func TestAltChordsAreNotTypedIntoTheFilter(t *testing.T) {
	for _, r := range []rune{'m', 'c', 'f', 'x'} {
		m := testModel(t)
		before := len(m.Results())
		tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}, Alt: true})
		got := tm.(Model)
		if got.Query().Text != "" {
			t.Errorf("alt+%c typed %q into the filter", r, got.Query().Text)
		}
		if len(got.Results()) != before {
			t.Errorf("alt+%c changed the results: %d → %d", r, before, len(got.Results()))
		}
	}
}

// On Windows a bare Ctrl press arrives as KeyRunes carrying NUL: bubbletea's console
// reader drops only a bare Shift. Typed into the filter, it matched nothing, so the list
// emptied the moment Ctrl went down and the Ctrl+Y that followed had nothing to yank —
// it closed as a cancel (T-016). No control character belongs in the filter.
func TestControlRunesAreNotTypedIntoTheFilter(t *testing.T) {
	for _, r := range []rune{0, 0x19, 0x1b, 0x7f} {
		m := testModel(t)
		before := len(m.Results())
		tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		got := tm.(Model)
		if got.Query().Text != "" {
			t.Errorf("%U typed %q into the filter", r, got.Query().Text)
		}
		if len(got.Results()) != before {
			t.Errorf("%U changed the results: %d → %d", r, before, len(got.Results()))
		}
	}
}

// The sequence as the Windows console delivers it: Ctrl down (NUL), then Ctrl+Y.
func TestBareCtrlThenCtrlYStillYanks(t *testing.T) {
	m := testModel(t)
	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{0}})
	tm, _ = tm.(Model).Update(tea.KeyMsg{Type: tea.KeyCtrlY})
	if c := tm.(Model).Choice; c == nil || c.Action != ActionYank || c.Cmd == "" {
		t.Fatalf("choice = %+v", c)
	}
}

// The plain rune must still type, or the finder stops filtering.
func TestPlainRunesStillType(t *testing.T) {
	m := testModel(t)
	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	if got := tm.(Model).Query().Text; got != "g" {
		t.Errorf("got %q, want %q", got, "g")
	}
}
