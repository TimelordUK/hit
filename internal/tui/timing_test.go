package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/TimelordUK/hit/internal/timing"
	tea "github.com/charmbracelet/bubbletea"
)

// Without HIT_TIMING the finder has no timeline, and nothing about the finder changes.
func TestNoTimelineDrawsNoTimingLine(t *testing.T) {
	m := testModel(t)
	if got := m.timingLine(); got != "" {
		t.Errorf("got %q, want empty", got)
	}
	if v := m.View(); strings.Contains(v, "⏱") {
		t.Errorf("timing line drawn without a timeline:\n%s", v)
	}
}

func TestTimingLineReportsThePhases(t *testing.T) {
	m := testModel(t)
	start := time.Date(2026, 9, 21, 10, 0, 0, 0, time.UTC)
	tl := timing.New(start)
	tl.MarkAt("spawn", start.Add(2800*time.Millisecond))
	tl.MarkAt("read", start.Add(2802*time.Millisecond))
	m.Timing = tl

	v := m.View()
	if !strings.Contains(v, "spawn 2800") {
		t.Errorf("spawn phase not shown:\n%s", v)
	}
	// View marks the paint itself, so the report gains a phase by being drawn.
	if !tl.Marked("paint") {
		t.Error("View did not mark the first paint")
	}
	if !strings.Contains(v, "paint") {
		t.Errorf("paint phase not shown:\n%s", v)
	}
}

// The paint mark is the time to the *first* frame. Redrawing on every keystroke must
// not keep moving it, or the number stops meaning anything.
func TestPaintIsMarkedOnceOnly(t *testing.T) {
	m := testModel(t)
	tl := timing.New(time.Now())
	m.Timing = tl
	_ = m.View()
	first := tl.Total()
	time.Sleep(2 * time.Millisecond)
	_ = m.View()
	if tl.Total() != first {
		t.Errorf("paint re-marked on redraw: %v then %v", first, tl.Total())
	}
}

// The report takes a row from the list, not from the screen: with timing on, the finder
// must still fit its pane.
func TestViewWithTimingFitsSmallPanes(t *testing.T) {
	for _, size := range []struct{ w, h int }{{80, 24}, {40, 12}, {30, 8}, {20, 5}} {
		m := testModel(t)
		tl := timing.New(time.Now())
		tl.MarkAt("spawn", time.Now().Add(3*time.Second))
		m.Timing = tl
		var tm tea.Model = m
		tm, _ = tm.Update(tea.WindowSizeMsg{Width: size.w, Height: size.h})
		v := tm.(Model).View()
		lines := strings.Split(strings.TrimRight(v, "\n"), "\n")
		if len(lines) > size.h {
			t.Errorf("%dx%d: %d lines, too tall:\n%s", size.w, size.h, len(lines), v)
		}
		for i, l := range lines {
			if w := lipglossWidth(l); w > size.w {
				t.Errorf("%dx%d: line %d is %d wide", size.w, size.h, i, w)
			}
		}
	}
}
