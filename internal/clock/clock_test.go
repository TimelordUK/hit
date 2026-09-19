package clock

import (
	"testing"
	"time"
)

func TestFromEnv(t *testing.T) {
	c, err := FromEnv(func(string) string { return "2026-09-19T10:12:03.412Z" })
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2026, 9, 19, 10, 12, 3, 412e6, time.UTC); !c().Equal(want) {
		t.Fatalf("got %v", c())
	}
	if _, err := FromEnv(func(string) string { return "monday" }); err == nil {
		t.Fatal("want error for bad HIT_NOW")
	}
	c, _ = FromEnv(func(string) string { return "" })
	if time.Since(c()) > time.Minute {
		t.Fatal("unset HIT_NOW should give the real clock")
	}
}
