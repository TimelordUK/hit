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

// The plain rune must still type, or the finder stops filtering.
func TestPlainRunesStillType(t *testing.T) {
	m := testModel(t)
	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	if got := tm.(Model).Query().Text; got != "g" {
		t.Errorf("got %q, want %q", got, "g")
	}
}
