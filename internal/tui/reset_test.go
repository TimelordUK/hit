package tui

import (
	"testing"

	"github.com/TimelordUK/hit/internal/search"
)

// Alt+R puts every mode back to how the finder opens — scope all, sort rank, every group,
// failed commands shown — in one key, instead of undoing each with its own (T-020). The
// typed text stays: "restart, but everywhere" is the usual reason to reset, and Ctrl+U
// already clears the text.
func TestAltRResetsEveryModeButKeepsTheText(t *testing.T) {
	m := categorised(t, "")
	m = send(m, "g")
	m = sendAlt(m, 'd')            // this folder
	m = sendAlt(m, 's')            // sort: recent → rank, or rank → recent
	m = send(sendAlt(m, 'g'), "c") // a group
	m = send(m, "ctrl+x")          // hide failed
	m = sendAlt(m, 'g')            // leave the picker open, too
	m.cursor = 1

	m = sendAlt(m, 'r')
	q := m.Query()
	if q.Scope != search.ScopeAll || q.Sort != search.SortRank || q.HideFailed {
		t.Errorf("modes not reset: scope=%s sort=%s hideFailed=%v", q.Scope, q.Sort, q.HideFailed)
	}
	if m.catFilter != -1 || m.picking {
		t.Errorf("group not reset: filter=%d picking=%v", m.catFilter, m.picking)
	}
	if q.Text != "g" {
		t.Errorf("the typed text should stay: %q", q.Text)
	}
	if m.Cursor() != 0 {
		t.Errorf("cursor should go back to the top: %d", m.Cursor())
	}
	fresh := send(New(m.history, search.Query{Scope: search.ScopeAll, Now: now}), "g")
	if len(m.Results()) != len(fresh.Results()) {
		t.Errorf("reset list should match a fresh finder with the same text: %v vs %v", cmds(m), cmds(fresh))
	}
}

// After Alt+R, Alt+D narrows to the folder and back out to all, not to whatever scope
// was in force before the reset.
func TestAltRForgetsTheScopeAltDWouldRestore(t *testing.T) {
	m := sendAlt(send(categorised(t, ""), "ctrl+r"), 'd') // host, then this folder
	m = sendAlt(m, 'r')
	m = sendAlt(sendAlt(m, 'd'), 'd')
	if m.Query().Scope != search.ScopeAll {
		t.Errorf("alt+d after a reset should return to all: %s", m.Query().Scope)
	}
}
