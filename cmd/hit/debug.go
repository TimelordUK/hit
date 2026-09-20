package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/TimelordUK/hit/internal/paths"
)

// debugf appends a line to $TEMP/hit-debug.log when HIT_DEBUG is set, matching the pwsh
// side's Write-HitDebug. The finder runs inside a key handler where nothing is visible,
// so this is how a failure gets reported. It never fails the command.
func debugf(env paths.Env, format string, args ...any) {
	if env.Getenv == nil || env.Getenv("HIT_DEBUG") == "" {
		return
	}
	f, err := os.OpenFile(filepath.Join(os.TempDir(), "hit-debug.log"),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s  hit: %s\n", time.Now().Format("15:04:05.000"), fmt.Sprintf(format, args...))
}
