// Package jump resolves `cd <terms>` to a directory you have been to (F-030, DESIGN §8.1).
// Nothing here touches the disk except Options.Exists, which only step 0 calls, and only
// for a path relative to where you stand.
package jump

import (
	"sort"
	"strings"
	"time"

	"github.com/TimelordUK/hit/internal/store"
)

// Step is the rule that chose a target. The steps are tried in this order and the first
// that finds anything wins.
type Step string

const (
	Real     Step = "real path" // what Set-Location would do anyway; never searched
	Below    Step = "below"     // a visited directory under the current one
	Nearest  Step = "nearest"   // under the closest parent whose visited subtree matches
	Anywhere Step = "anywhere"  // another drive or share: zoxide's behaviour, last
)

// Candidate is a directory a jump-category command took you to.
type Candidate struct {
	Dir   string  // as last recorded
	Score float64 // frecency: only the order matters
}

// Result is where `cd` should go. For Real, Dir is the query as typed, for the shell to
// hand to Set-Location unchanged.
type Result struct {
	Dir   string
	Step  Step
	Exact bool // the last term is the whole last segment (C-035)
}

// Options are what path handling depends on.
type Options struct {
	Windows bool // `\` and `/` alike, case ignored
	// Exists reports whether a directory exists. Called at most once per Resolve, for a
	// path relative to the current directory; nil means nothing exists.
	Exists func(dir string) bool
}

// Resolve picks the target for `cd <terms>` from cwd. ok is false when there is nothing to
// jump to, and cd should be plain Set-Location.
func Resolve(cands []Candidate, cwd string, terms []string, o Options) (r Result, ok bool) {
	if len(terms) == 0 {
		return Result{}, false
	}
	if len(terms) == 1 && o.real(cwd, terms[0]) {
		return Result{Dir: terms[0], Step: Real}, true
	}
	var split []string
	for _, t := range terms {
		split = append(split, o.segments(t)...)
	}
	if len(split) == 0 {
		return Result{}, false
	}

	here := o.segments(cwd)
	best, bestRing := -1, 0
	var bestExact bool
	for i, c := range cands {
		segs := o.segments(c.Dir)
		exact, match := matches(segs, split)
		if !match || equal(segs, here) {
			continue
		}
		ring := distance(segs, here)
		if best >= 0 && !better(ring, exact, c, bestRing, bestExact, cands[best]) {
			continue
		}
		best, bestRing, bestExact = i, ring, exact
	}
	if best < 0 {
		return Result{}, false
	}
	step := Nearest
	switch {
	case bestRing == 0:
		step = Below
	case bestRing > len(here):
		step = Anywhere
	}
	return Result{Dir: cands[best].Dir, Step: step, Exact: bestExact}, true
}

// better orders matches: nearer, then exact, then frecency, then the path so ties are
// stable from one run to the next.
func better(ring int, exact bool, c Candidate, bRing int, bExact bool, b Candidate) bool {
	if ring != bRing {
		return ring < bRing
	}
	if exact != bExact {
		return exact
	}
	if c.Score != b.Score {
		return c.Score > b.Score
	}
	return c.Dir < b.Dir
}

// distance is how many parents up from cwd the candidate's subtree starts: 0 under cwd,
// len(here) and below on the same drive or share, and beyond that anywhere else.
func distance(segs, here []string) int {
	n := 0
	for n < len(segs) && n < len(here) && segs[n] == here[n] {
		n++
	}
	if n == 0 {
		return len(here) + 1
	}
	return len(here) - n
}

// matches: each term is a substring of a segment, in order, and the last term matches the
// last segment. Never fuzzy: fuzzy matching on directories finds too many odd ones.
func matches(segs, terms []string) (exact, ok bool) {
	if len(segs) == 0 {
		return false, false
	}
	last := segs[len(segs)-1]
	lt := terms[len(terms)-1]
	if !strings.Contains(last, lt) {
		return false, false
	}
	j := 0
	for _, t := range terms[:len(terms)-1] {
		for j < len(segs)-1 && !strings.Contains(segs[j], t) {
			j++
		}
		if j == len(segs)-1 {
			return false, false
		}
		j++
	}
	return last == lt, true
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// real is step 0: a query Set-Location takes as it is. Only a plain relative path is
// checked on disk; everything else is taken on sight, so an absolute path to a share is
// never stat'ed here — Set-Location reports it if it is not there.
func (o Options) real(cwd, q string) bool {
	switch q {
	case "-", "+", ".", "..", "~":
		return true
	}
	seps := "/"
	if o.Windows {
		seps = `\/`
	}
	for _, p := range []string{"~", ".", ".."} {
		if len(q) > len(p) && strings.HasPrefix(q, p) && strings.ContainsRune(seps, rune(q[len(p)])) {
			return true
		}
	}
	if strings.ContainsRune(seps, rune(q[0])) || (o.Windows && len(q) >= 2 && q[1] == ':') {
		return true
	}
	if o.Exists == nil {
		return false
	}
	sep := "/"
	if o.Windows {
		sep = `\`
	}
	return o.Exists(strings.TrimRight(cwd, seps) + sep + q)
}

// segments splits a path or a term on separators, dropping empty parts, so `\\host\share`
// is [host share] and `C:\Users` is [c: users]. Case is folded on Windows.
func (o Options) segments(p string) []string {
	if o.Windows {
		p = strings.ToLower(strings.ReplaceAll(p, "/", `\`))
		return fields(p, '\\')
	}
	return fields(p, '/')
}

func fields(p string, sep rune) []string {
	return strings.FieldsFunc(p, func(r rune) bool { return r == sep })
}

// Candidates gathers the directories jump-category commands took you to. The prompt writes
// a cd record after any command that changed directory, so a visit is a candidate when the
// command just before it in the same session is one isJump accepts. Directories reached any
// other way — a script's Set-Location, Pop-Location, the shell starting up — are not
// places you chose to go.
func Candidates(h *store.History, isJump func(cmd, cwd string) bool, o Options, now time.Time) []Candidate {
	bySession := map[string][]int{}
	for i, e := range h.Entries {
		if e.Session != "" {
			bySession[e.Session] = append(bySession[e.Session], i)
		}
	}
	for _, idx := range bySession {
		sort.SliceStable(idx, func(a, b int) bool { return h.Entries[idx[a]].Time.Before(h.Entries[idx[b]].Time) })
	}

	type agg struct {
		Candidate
		last time.Time
	}
	var out []*agg
	byKey := map[string]*agg{}
	used := map[int]bool{}
	jump := map[int]bool{}
	for _, v := range h.Visits {
		idx := bySession[v.Session]
		k := sort.Search(len(idx), func(k int) bool { return h.Entries[idx[k]].Time.After(v.Time) }) - 1
		if v.Session == "" || k < 0 || used[idx[k]] {
			continue
		}
		e := idx[k]
		used[e] = true
		if _, seen := jump[e]; !seen {
			jump[e] = isJump(h.Entries[e].Cmd, h.Entries[e].Cwd)
		}
		if !jump[e] {
			continue
		}
		key := strings.Join(o.segments(v.Dir), "\x00")
		a := byKey[key]
		if a == nil {
			a = &agg{Candidate: Candidate{Dir: v.Dir}, last: v.Time}
			byKey[key] = a
			out = append(out, a)
		}
		a.Score += score(v.Time, now)
		if !v.Time.Before(a.last) {
			a.Dir, a.last = v.Dir, v.Time
		}
	}
	cands := make([]Candidate, len(out))
	for i, a := range out {
		cands[i] = a.Candidate
	}
	return cands
}

// score is one visit's weight: zoxide's ageing, applied per visit so a burst of visits
// long ago fades while today's count fully.
func score(t, now time.Time) float64 {
	switch age := now.Sub(t); {
	case age < time.Hour:
		return 4
	case age < 24*time.Hour:
		return 2
	case age < 7*24*time.Hour:
		return 0.5
	default:
		return 0.25
	}
}
