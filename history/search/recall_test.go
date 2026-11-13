package search

import (
	"reflect"
	"testing"

	"github.com/TimelordUK/hit/history/record"
	"github.com/TimelordUK/hit/history/store"
)

// Up at the prompt walks these (S-009), the way zsh-history-substring-search does: what is
// typed is a substring, newest first, each command once. This shell's own commands come
// first, as PSReadLine's do (its in-memory list only gains this session's lines), then
// everything else, so another pane's command never cuts in front of the one just run here.
func recallFixture() *store.History {
	return store.Build([]record.Record{
		{K: record.KindCmd, ID: "1", TS: "2026-11-13T09:00:00.000Z", Cmd: "git status", Sh: "pwsh", Sid: "old"},
		{K: record.KindCmd, ID: "2", TS: "2026-11-13T09:01:00.000Z", Cmd: "jq '.[] | def'", Sh: "pwsh", Sid: "old"},
		{K: record.KindCmd, ID: "3", TS: "2026-11-13T09:02:00.000Z", Cmd: "git log --oneline", Sh: "pwsh", Sid: "me"},
		{K: record.KindCmd, ID: "4", TS: "2026-11-13T09:03:00.000Z", Cmd: "ls -la", Sh: "zsh", Sid: "wsl"},
		{K: record.KindCmd, ID: "5", TS: "2026-11-13T09:04:00.000Z", Cmd: "Get-ChildItem", Sh: "pwsh", Sid: "other"},
		{K: record.KindCmd, ID: "6", TS: "2026-11-13T09:05:00.000Z", Cmd: "git status", Sh: "pwsh", Sid: "me"},
		{K: record.KindCmd, ID: "7", TS: "2026-11-13T09:06:00.000Z", Cmd: "Git-Thing\nsecond line", Sh: "pwsh", Sid: "other"},
	})
}

func TestRecallIsSubstringNewestFirstThisSessionFirst(t *testing.T) {
	got := Recall(recallFixture(), RecallQuery{Text: "git", Session: "me", Shell: "pwsh"})
	want := []string{"git status", "git log --oneline", "Git-Thing\nsecond line"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

// Blank recalls everything, which is plain Up stepping.
func TestRecallBlankIsEverything(t *testing.T) {
	got := Recall(recallFixture(), RecallQuery{Text: "  ", Session: "me", Shell: "pwsh"})
	want := []string{"git status", "git log --oneline", "Git-Thing\nsecond line", "Get-ChildItem", "jq '.[] | def'"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

// Substring, not a prefix and not fuzzy: `def` finds `… | def`, and `gs` finds nothing.
func TestRecallMatchesInsideTheCommandOnly(t *testing.T) {
	h := recallFixture()
	if got := Recall(h, RecallQuery{Text: "def", Shell: "pwsh"}); !reflect.DeepEqual(got, []string{"jq '.[] | def'"}) {
		t.Errorf("def: got %q", got)
	}
	if got := Recall(h, RecallQuery{Text: "gs", Shell: "pwsh"}); len(got) != 0 {
		t.Errorf("gs is not a substring of anything: got %q", got)
	}
}

// The command already on the line is left out: bringing it back changes nothing.
func TestRecallLeavesOutWhatIsTyped(t *testing.T) {
	got := Recall(recallFixture(), RecallQuery{Text: "git status ", Session: "me", Shell: "pwsh"})
	if len(got) != 0 {
		t.Errorf("got %q", got)
	}
}

func TestRecallCase(t *testing.T) {
	h := recallFixture()
	if got := Recall(h, RecallQuery{Text: "GIT", Shell: "pwsh"}); len(got) != 3 {
		t.Errorf("ignore: got %q", got)
	}
	if got := Recall(h, RecallQuery{Text: "Git", Shell: "pwsh", Case: CaseSmart}); !reflect.DeepEqual(got, []string{"Git-Thing\nsecond line"}) {
		t.Errorf("smart: got %q", got)
	}
}

func TestRecallOtherShellsAndLimit(t *testing.T) {
	h := recallFixture()
	if got := Recall(h, RecallQuery{Text: "ls", Shell: "pwsh"}); len(got) != 0 {
		t.Errorf("a zsh command came back in pwsh: %q", got)
	}
	if got := Recall(h, RecallQuery{Text: "ls"}); !reflect.DeepEqual(got, []string{"ls -la"}) {
		t.Errorf("no shell given: got %q", got)
	}
	if got := Recall(h, RecallQuery{Session: "me", Limit: 2}); !reflect.DeepEqual(got, []string{"git status", "git log --oneline"}) {
		t.Errorf("limit: got %q", got)
	}
}
