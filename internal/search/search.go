// Package search ranks history entries for the finder (DESIGN §6).
//
// Ranking is match quality × recency × frequency, with bonuses for the same directory,
// the same session and a successful exit. The numbers here are a starting point to be
// tuned by daily use; golden tests make any change visible.
package search

import (
	"math"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/TimelordUK/hit/internal/store"
)

// Scope narrows which entries are considered.
type Scope string

const (
	ScopeAll     Scope = "all"     // everything hit has ever seen
	ScopeHost    Scope = "host"    // this machine
	ScopeSession Scope = "session" // this shell
	ScopeDir     Scope = "dir"     // commands run in this directory
)

// Scopes in the order the finder cycles through them.
var Scopes = []Scope{ScopeDir, ScopeSession, ScopeHost, ScopeAll}

// Query is what the finder asks for.
type Query struct {
	Text       string // the filter the user has typed
	Scope      Scope
	Cwd        string    // the shell's current directory
	Session    string    // the shell's session id
	Host       string    // this machine
	Shell      string    // current shell family; empty means don't filter
	HideFailed bool      // drop commands with a non-zero exit
	Now        time.Time // for ranking and tests
	Limit      int       // 0 means no limit
}

// Result is one line in the finder: a command, with the run it was last seen in.
type Result struct {
	Entry     store.Entry // the most recent run
	Count     int         // how many times this exact command was run in scope
	Matched   []int       // indexes into Entry.Cmd of the matched runes, for highlighting
	Score     float64
	InCwd     bool // some run of it happened in the query's directory
	InSession bool // some run of it happened in this shell session
}

// Search filters, collapses duplicates and ranks.
func Search(h *store.History, q Query) []Result {
	if q.Now.IsZero() {
		q.Now = time.Now()
	}
	type agg struct {
		res   Result
		index int // keeps equal scores in a stable, most-recent-first order
	}
	byCmd := map[string]*agg{}
	var order []*agg

	for i := range h.Entries {
		e := h.Entries[i]
		if !inScope(e, q) {
			continue
		}
		if q.HideFailed && e.Exit != nil && *e.Exit != 0 {
			continue
		}
		a, seen := byCmd[e.Cmd]
		if !seen {
			a = &agg{index: len(order)}
			byCmd[e.Cmd] = a
			order = append(order, a)
		}
		a.res.Count++
		// Duplicates collapse across directories and sessions, so the bonuses look at
		// every run, not only the most recent one.
		if q.Cwd != "" && samePath(e.Cwd, q.Cwd) {
			a.res.InCwd = true
		}
		if q.Session != "" && e.Session == q.Session {
			a.res.InSession = true
		}
		// Entries arrive in file order, so the last one wins as "most recent". An entry
		// with no timestamp (damaged record) never displaces one that has one.
		if a.res.Entry.ID == "" || !e.Time.Before(a.res.Entry.Time) {
			a.res.Entry = e
		}
	}

	results := make([]Result, 0, len(order))
	for _, a := range order {
		score, matched, ok := Match(a.res.Entry.Cmd, q.Text)
		if !ok {
			continue
		}
		a.res.Matched = matched
		a.res.Score = score * frequencyWeight(a.res.Count) * recencyWeight(q.Now, a.res.Entry.Time) * bonus(a.res)
		results = append(results, a.res)
	}

	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		return results[i].Entry.Time.After(results[j].Entry.Time)
	})
	if q.Limit > 0 && len(results) > q.Limit {
		results = results[:q.Limit]
	}
	return results
}

func inScope(e store.Entry, q Query) bool {
	if q.Shell != "" && e.Shell != "" && e.Shell != q.Shell {
		return false
	}
	switch q.Scope {
	case ScopeDir:
		return q.Cwd != "" && samePath(e.Cwd, q.Cwd) && sameHost(e.Host, q.Host)
	case ScopeSession:
		return q.Session != "" && e.Session == q.Session
	case ScopeHost:
		return sameHost(e.Host, q.Host)
	}
	return true
}

func sameHost(a, b string) bool {
	return b == "" || strings.EqualFold(a, b)
}

// samePath compares directories the way the platforms do: Windows paths are
// case-insensitive and / and \ are the same separator; a trailing separator is ignored.
// Remote paths are never stat'd (DESIGN §8).
func samePath(a, b string) bool {
	return strings.EqualFold(normalizePath(a), normalizePath(b))
}

func normalizePath(p string) string {
	p = strings.ReplaceAll(p, "/", `\`)
	if len(p) > 1 {
		p = strings.TrimRight(p, `\`)
	}
	return p
}

// frequencyWeight grows with the run count but flattens, so a command run 500 times
// doesn't bury a good match run twice.
func frequencyWeight(count int) float64 {
	return 1 + math.Log1p(float64(count))
}

// recencyWeight halves every 7 days, with a floor so old commands stay findable.
func recencyWeight(now, t time.Time) float64 {
	if t.IsZero() {
		return 0.5
	}
	days := now.Sub(t).Hours() / 24
	if days < 0 {
		days = 0
	}
	return 0.5 + math.Exp2(-days/7)
}

func bonus(r Result) float64 {
	b := 1.0
	if r.InCwd {
		b *= 1.5 // run here before: usually what you're looking for
	}
	if r.InSession {
		b *= 1.2
	}
	if r.Entry.Exit != nil && *r.Entry.Exit != 0 {
		b *= 0.7 // it failed last time; still findable, just lower
	}
	return b
}

// Match scores pattern against text and returns the rune indexes that matched.
// Matching is a subsequence match, smart-case (an upper-case rune in the pattern makes
// that match case-sensitive), scoring contiguous runs and matches at word boundaries
// higher. An empty pattern matches everything with a neutral score.
func Match(text, pattern string) (score float64, matched []int, ok bool) {
	if pattern == "" {
		return 1, nil, true
	}
	runes := []rune(text)
	pat := []rune(pattern)

	matched = make([]int, 0, len(pat))
	pi := 0
	var (
		run       float64 // length of the current contiguous run
		total     float64
		firstAt   = -1
		prevIndex = -2
	)
	for i := 0; i < len(runes) && pi < len(pat); i++ {
		if !runeMatch(runes[i], pat[pi]) {
			continue
		}
		if firstAt < 0 {
			firstAt = i
		}
		if i == prevIndex+1 {
			run++
		} else {
			run = 1
		}
		s := 1.0 + run // contiguous matches are worth more
		if i == 0 || isBoundary(runes[i-1]) {
			s += 2 // start of a word: "irm" in "… | irm" beats "… iRM …"
		}
		if runes[i] == pat[pi] {
			s += 0.5 // exact case
		}
		total += s
		matched = append(matched, i)
		prevIndex = i
		pi++
	}
	if pi < len(pat) {
		return 0, nil, false
	}

	// Normalise so long commands aren't penalised for their length, then reward matches
	// that start early and span a short stretch of the text.
	span := float64(matched[len(matched)-1]-matched[0]) + 1
	density := float64(len(pat)) / span
	early := 1.0 / (1.0 + float64(firstAt)/40)
	score = (total / (float64(len(pat)) * 3.5)) * (0.5 + density) * early
	return score, matched, true
}

func runeMatch(text, pat rune) bool {
	if unicode.IsUpper(pat) {
		return text == pat // smart-case: an upper-case pattern rune must match exactly
	}
	return unicode.ToLower(text) == unicode.ToLower(pat)
}

func isBoundary(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '\r', '-', '_', '.', '/', '\\', ':', '|', '=', ',', ';', '(', ')', '[', ']', '{', '}', '\'', '"', '$', '&', '`':
		return true
	}
	return false
}
