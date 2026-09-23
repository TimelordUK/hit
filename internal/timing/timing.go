// Package timing records how long each phase of one run took, so a slow recall can be
// attributed to a phase instead of guessed at (DESIGN §15).
//
// The interesting number is usually not inside hit at all: it is the gap between the
// shell calling Process.Start and Go's main() running, which is process creation —
// loader, runtime init, and on a managed machine whatever scans the binary first. The
// shell passes the moment it spawned us (`--started-at`) so that gap can be measured;
// nothing else can see it, because neither side is running for the whole of it.
package timing

import (
	"fmt"
	"strings"
	"time"
)

// Span is one phase: from the previous mark to this one.
type Span struct {
	Name string
	D    time.Duration
}

// Timeline is the marks taken during one run. A nil *Timeline is a working no-op, so
// the uninstrumented path costs nothing and callers need no checks.
type Timeline struct {
	start time.Time
	via   string
	names []string
	ats   []time.Time
}

// New starts a timeline at t.
func New(t time.Time) *Timeline { return &Timeline{start: t} }

// NewVia starts a timeline at t and records how the finder was reached — "pipe" when a
// resident server drew it, "spawn" when a process was started for it. That is the one
// thing the phases cannot say for themselves: a served run simply has no spawn phase,
// and an absence is easy to misread as a fast one.
func NewVia(t time.Time, via string) *Timeline { return &Timeline{start: t, via: via} }

// Via is how the finder was reached, or "" when nobody said.
func (t *Timeline) Via() string {
	if t == nil {
		return ""
	}
	return t.via
}

// Mark records that the named phase finished now.
func (t *Timeline) Mark(name string) { t.MarkAt(name, time.Now()) }

// MarkAt records that the named phase finished at at. Marks are kept in call order:
// the caller decides the sequence, not the clock, so a clock that steps backwards
// cannot reorder the report.
func (t *Timeline) MarkAt(name string, at time.Time) {
	if t == nil {
		return
	}
	t.names = append(t.names, name)
	t.ats = append(t.ats, at)
}

// Marked reports whether a phase of that name has been recorded. The first paint is
// marked from View, which runs on every redraw, so it needs to ask.
func (t *Timeline) Marked(name string) bool {
	if t == nil {
		return false
	}
	for _, n := range t.names {
		if n == name {
			return true
		}
	}
	return false
}

// Spans returns one span per mark, each measured from the mark before it.
func (t *Timeline) Spans() []Span {
	if t == nil {
		return nil
	}
	spans := make([]Span, 0, len(t.names))
	prev := t.start
	for i, n := range t.names {
		spans = append(spans, Span{Name: n, D: t.ats[i].Sub(prev)})
		prev = t.ats[i]
	}
	return spans
}

// Total is from the start to the last mark.
func (t *Timeline) Total() time.Duration {
	if t == nil || len(t.ats) == 0 {
		return 0
	}
	return t.ats[len(t.ats)-1].Sub(t.start)
}

// String is the one-line report: "via spawn · spawn 2841 · read 2 · total 2843 ms".
func (t *Timeline) String() string {
	if t == nil || len(t.names) == 0 {
		return ""
	}
	parts := make([]string, 0, len(t.names)+2)
	if t.via != "" {
		parts = append(parts, "via "+t.via)
	}
	for _, s := range t.Spans() {
		parts = append(parts, fmt.Sprintf("%s %d", s.Name, Millis(s.D)))
	}
	parts = append(parts, fmt.Sprintf("total %d", Millis(t.Total())))
	return strings.Join(parts, " · ") + " ms"
}

// Millis rounds a span to whole milliseconds. A span measured across two processes is
// a difference of two wall-clock readings, so a clock adjustment between them can make
// it negative; report that as 0 rather than a negative phase, which would only ever be
// read as a bug in the report.
func Millis(d time.Duration) int64 {
	if d < 0 {
		return 0
	}
	return d.Round(time.Millisecond).Milliseconds()
}
