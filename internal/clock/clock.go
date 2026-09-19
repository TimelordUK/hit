// Package clock provides the time source the core depends on, so tests can pin it
// (DESIGN §13.1).
package clock

import (
	"fmt"
	"time"
)

// Clock returns the current time.
type Clock func() time.Time

// System is the real clock.
func System() time.Time { return time.Now() }

// Fixed returns a clock that always reports t.
func Fixed(t time.Time) Clock { return func() time.Time { return t } }

// FromEnv returns a fixed clock when HIT_NOW is set (tests only), otherwise the real one.
// getenv is os.Getenv in production and a map lookup in tests.
func FromEnv(getenv func(string) string) (Clock, error) {
	v := getenv("HIT_NOW")
	if v == "" {
		return System, nil
	}
	t, err := time.Parse(time.RFC3339Nano, v)
	if err != nil {
		return nil, fmt.Errorf("HIT_NOW: %w", err)
	}
	return Fixed(t), nil
}
