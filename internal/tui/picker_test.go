package tui

import (
	"strings"
	"testing"
)

// Alt+G then a group's mark letter jumps straight to it (T-019): the letters are the marks
// already shown at the start of each row, so there is nothing new to learn or bind.
func TestAltGThenALetterJumpsToThatGroup(t *testing.T) {
	for _, letter := range []string{"d", "D"} {
		m := send(sendAlt(categorised(t, ""), 'g'), letter)
		if m.picking {
			t.Errorf("%s: picking a group should close the picker", letter)
		}
		if got := strings.Join(cmds(m), ","); got != "ls" || m.Query().Text != "" {
			t.Errorf("%s: want devops only and nothing typed: %v %q", letter, got, m.Query().Text)
		}
	}
	m := send(sendAlt(categorised(t, ""), 'g'), "c")
	if got := strings.Join(cmds(m), ","); got != "curl https://x" {
		t.Errorf("c: %s", got)
	}
}

// The letter of the group you are already in goes back to everything.
func TestSameLetterAgainGoesBackToAll(t *testing.T) {
	m := send(sendAlt(categorised(t, "devops"), 'g'), "d")
	if m.catFilter != -1 || len(m.Results()) != 4 {
		t.Errorf("want all groups: filter=%d %v", m.catFilter, cmds(m))
	}
}

// Esc leaves the picker, not the finder, and keeps whatever group it was on.
func TestEscLeavesThePickerNotTheFinder(t *testing.T) {
	m := send(sendAlt(categorised(t, "content"), 'g'), "esc")
	if m.Choice != nil || m.picking {
		t.Fatalf("esc should only close the picker: choice=%+v picking=%v", m.Choice, m.picking)
	}
	if got := strings.Join(cmds(m), ","); got != "curl https://x" {
		t.Errorf("group should be unchanged: %s", got)
	}
	if m = send(m, "esc"); m.Choice == nil || m.Choice.Action != ActionCancel {
		t.Errorf("a second esc closes the finder as usual: %+v", m.Choice)
	}
}

// A key that is not a group's letter closes the picker and does its usual job, so typing
// straight after Alt+G is not lost.
func TestOtherKeysLeaveThePickerAndWorkAsUsual(t *testing.T) {
	m := send(sendAlt(categorised(t, ""), 'g'), "x")
	if m.picking || m.Query().Text != "x" {
		t.Errorf("x should be typed: picking=%v query=%q", m.picking, m.Query().Text)
	}
	m = send(sendAlt(categorised(t, ""), 'g'), "down")
	if m.picking || m.Cursor() != 1 {
		t.Errorf("down should move: picking=%v cursor=%d", m.picking, m.Cursor())
	}
}

// While picking, the status line lists each group with its letter.
func TestPickerListsTheLetters(t *testing.T) {
	m := sendAlt(categorised(t, ""), 'g')
	v := view(m)
	for _, want := range []string{"G git", "C content", "D devops", "esc"} {
		if !strings.Contains(v, want) {
			t.Errorf("picker should show %q:\n%s", want, v)
		}
	}
	if v = view(send(m, "esc")); strings.Contains(v, "D devops") {
		t.Errorf("picker should be gone after esc:\n%s", v)
	}
}
