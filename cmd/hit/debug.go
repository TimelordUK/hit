package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/TimelordUK/hit/internal/paths"
	"github.com/TimelordUK/hit/internal/timing"
)

// newTimeline returns a timeline when HIT_TIMING is set, and nil otherwise — nil being a
// working no-op, so the normal run carries no checks and no cost (DESIGN §15).
//
// startedMs is the shell's clock when it spawned us. With it, the timeline starts there
// and the first span is process creation; without it, the timeline starts at our own
// package initialisation and that span is simply not visible.
func newTimeline(env paths.Env, startedMs int64) *timing.Timeline {
	if env.Getenv == nil || env.Getenv("HIT_TIMING") == "" {
		return nil
	}
	if startedMs <= 0 {
		return timing.NewVia(processStart, "spawn")
	}
	tl := timing.NewVia(time.UnixMilli(startedMs), "spawn")
	tl.MarkAt("spawn", processStart)
	return tl
}

// newServeTimeline times one request drawn by a resident server. It starts when the
// request arrived, and reports "pipe" so the report says how the finder was reached:
// a served run has no spawn phase at all, and an absence reads too easily as a fast one.
func newServeTimeline(env paths.Env) *timing.Timeline {
	if env.Getenv == nil || env.Getenv("HIT_TIMING") == "" {
		return nil
	}
	return timing.NewVia(time.Now(), "pipe")
}

// debugf appends a line to $TEMP/hit-debug.log when HIT_DEBUG is set, matching the pwsh
// side's Write-HitDebug. The finder runs inside a key handler where nothing is visible,
// so this is how a failure gets reported. It never fails the command.
func debugf(env paths.Env, format string, args ...any) {
	if env.Getenv == nil || env.Getenv("HIT_DEBUG") == "" {
		return
	}
	appendLog(format, args...)
}

// timingf is the same log under the other switch: HIT_TIMING is for one number and
// should not oblige you to wade through every key press to find it.
func timingf(env paths.Env, format string, args ...any) {
	if env.Getenv == nil || env.Getenv("HIT_TIMING") == "" {
		return
	}
	appendLog(format, args...)
}

func appendLog(format string, args ...any) {
	f, err := os.OpenFile(filepath.Join(os.TempDir(), "hit-debug.log"),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s  hit: %s\n", time.Now().Format("15:04:05.000"), fmt.Sprintf(format, args...))
}
