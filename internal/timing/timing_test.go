package timing

import (
	"testing"
	"time"
)

var epoch = time.Date(2026, 9, 21, 10, 0, 0, 0, time.UTC)

func at(ms int) time.Time { return epoch.Add(time.Duration(ms) * time.Millisecond) }

func TestSpansMeasureFromThePreviousMark(t *testing.T) {
	tl := New(epoch)
	tl.MarkAt("spawn", at(2800))
	tl.MarkAt("read", at(2805))
	tl.MarkAt("paint", at(2810))

	spans := tl.Spans()
	want := []Span{
		{"spawn", 2800 * time.Millisecond},
		{"read", 5 * time.Millisecond},
		{"paint", 5 * time.Millisecond},
	}
	if len(spans) != len(want) {
		t.Fatalf("got %d spans, want %d", len(spans), len(want))
	}
	for i, w := range want {
		if spans[i] != w {
			t.Errorf("span %d: got %v, want %v", i, spans[i], w)
		}
	}
	if got := tl.Total(); got != 2810*time.Millisecond {
		t.Errorf("total: got %v, want 2810ms", got)
	}
}

// The report is what gets pasted into a bug report, so its shape is worth pinning.
func TestStringIsTheOneLineReport(t *testing.T) {
	tl := New(epoch)
	tl.MarkAt("spawn", at(2841))
	tl.MarkAt("read", at(2843))

	want := "spawn 2841 · read 2 · total 2843 ms"
	if got := tl.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// A nil timeline is the "timing off" path: every method must work and report nothing,
// so the normal run needs no `if enabled` checks around it.
func TestNilTimelineIsANoOp(t *testing.T) {
	var tl *Timeline
	tl.Mark("spawn")
	tl.MarkAt("read", at(1))
	if got := tl.String(); got != "" {
		t.Errorf("String: got %q, want empty", got)
	}
	if got := tl.Spans(); got != nil {
		t.Errorf("Spans: got %v, want nil", got)
	}
	if got := tl.Total(); got != 0 {
		t.Errorf("Total: got %v, want 0", got)
	}
	if tl.Marked("paint") {
		t.Error("Marked: got true, want false")
	}
}

func TestEmptyTimelineReportsNothing(t *testing.T) {
	tl := New(epoch)
	if got := tl.String(); got != "" {
		t.Errorf("got %q, want empty", got)
	}
	if got := tl.Total(); got != 0 {
		t.Errorf("total: got %v, want 0", got)
	}
}

func TestMarkedFindsAPhase(t *testing.T) {
	tl := New(epoch)
	tl.MarkAt("spawn", at(1))
	if !tl.Marked("spawn") {
		t.Error("spawn should be marked")
	}
	if tl.Marked("paint") {
		t.Error("paint should not be marked")
	}
}

// The spawn span is a difference between two processes' wall clocks. If the clock is
// adjusted in between it can come out negative, and a negative phase in the report
// would read as a bug in hit rather than as the clock moving.
func TestNegativeSpansReportAsZero(t *testing.T) {
	if got := Millis(-5 * time.Millisecond); got != 0 {
		t.Errorf("got %d, want 0", got)
	}
	tl := New(at(100))
	tl.MarkAt("spawn", at(90))
	if got := tl.String(); got != "spawn 0 · total 0 ms" {
		t.Errorf("got %q", got)
	}
}

// Sub-millisecond phases are the normal case for everything except spawn, and they
// must not vanish into an empty report.
func TestSubMillisecondPhasesRoundToZeroButStillAppear(t *testing.T) {
	tl := New(epoch)
	tl.MarkAt("read", epoch.Add(200*time.Microsecond))
	if got := tl.String(); got != "read 0 · total 0 ms" {
		t.Errorf("got %q", got)
	}
}

// Whether the finder was reached over the pipe or by starting a process is the thing the
// phases cannot say for themselves: a served run simply has no spawn phase, and an
// absence reads too easily as a fast one.
func TestViaIsNamedInTheReport(t *testing.T) {
	tl := NewVia(epoch, "pipe")
	tl.MarkAt("read", at(2))
	if got, want := tl.String(), "via pipe · read 2 · total 2 ms"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if tl.Via() != "pipe" {
		t.Errorf("Via: got %q", tl.Via())
	}
}

func TestViaIsOmittedWhenNobodySaid(t *testing.T) {
	tl := New(epoch)
	tl.MarkAt("read", at(2))
	if got := tl.String(); got != "read 2 · total 2 ms" {
		t.Errorf("got %q", got)
	}
	var nilTL *Timeline
	if nilTL.Via() != "" {
		t.Error("a nil timeline should report no route")
	}
}
