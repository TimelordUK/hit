package search

import (
	"strings"
	"unicode"

	"github.com/TimelordUK/hit/history/store"
)

// RecallQuery is what Up at the prompt asks for (S-009).
type RecallQuery struct {
	Text    string // what is on the line; blank recalls everything
	Session string // this shell's session: its commands come first
	Shell   string // only commands from this shell; empty means any
	Case    Case   // empty means CaseIgnore
	Limit   int    // 0 means no limit
}

// Recall is the commands Up walks through, as zsh-history-substring-search walks them:
// those containing Text, newest first, each once, leaving out Text itself since bringing
// it back changes nothing. The session's own commands come first, then everyone else's,
// as PSReadLine's Up does (its in-memory list only gains this session's lines), so a
// command run in another pane never cuts in front of the one just run here.
//
// No ranking: Up answers "what did I run", which is the question frecency fights (C-032).
func Recall(h *store.History, q RecallQuery) []string {
	text := strings.TrimSpace(q.Text)
	fold := q.Case != CaseSmart || !strings.ContainsFunc(text, unicode.IsUpper)
	if fold {
		text = strings.ToLower(text)
	}
	var out []string
	seen := map[string]bool{}
	pass := func(mine bool) {
		for i := len(h.Entries) - 1; i >= 0; i-- {
			e := h.Entries[i]
			if q.Limit > 0 && len(out) >= q.Limit {
				return
			}
			if (q.Session != "" && e.Session == q.Session) != mine || seen[e.Cmd] {
				continue
			}
			if q.Shell != "" && e.Shell != "" && e.Shell != q.Shell {
				continue
			}
			if strings.TrimSpace(e.Cmd) == "" {
				continue
			}
			c := e.Cmd
			if fold {
				c = strings.ToLower(c)
			}
			if strings.TrimSpace(c) == text || !strings.Contains(c, text) {
				continue
			}
			seen[e.Cmd] = true
			out = append(out, e.Cmd)
		}
	}
	pass(true)
	pass(false)
	return out
}
