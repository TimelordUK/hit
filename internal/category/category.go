// Package category labels history entries with the categories config.toml defines
// (DESIGN §17). A label is a view: it is worked out when history is read and never
// stored, so editing a rule relabels everything already recorded.
package category

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/TimelordUK/hit/internal/config"
)

// Rule is one compiled category.
type Rule struct {
	Name  string
	Color string // as configured; empty means the finder's quiet default
	Mark  string // one character, shown at the start of a row

	commands map[string]bool // lower-cased first words
	match    *regexp.Regexp
	cwd      []*regexp.Regexp
	scripts  []*regexp.Regexp // globs over the file the first word resolves to (C-040)
	paths    Paths            // how to resolve it: ~ and the separator
}

// Set is every usable rule in file order, which is priority order.
type Set struct {
	Rules []Rule
	// Problems are config problems plus anything found compiling; a rule with a problem
	// that would change what it matches is not in Rules.
	Problems []string
}

// Paths are what `~` and case folding depend on.
type Paths struct {
	Home    string
	Windows bool
}

// Compile turns config into rules. It never fails: whatever cannot be compiled is dropped
// and reported, because a broken config must not break Ctrl+R.
func Compile(c config.Config, p Paths) *Set {
	s := &Set{Problems: append([]string(nil), c.Problems...)}
	for _, cat := range c.Categories {
		r, problems, usable := compile(cat, p)
		for _, msg := range problems {
			s.Problems = append(s.Problems, fmt.Sprintf("category %s: %s", cat.Name, msg))
		}
		if usable {
			s.Rules = append(s.Rules, r)
		}
	}
	return s
}

var validName = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

var colors = map[string]bool{
	"black": true, "red": true, "green": true, "yellow": true, "blue": true,
	"magenta": true, "cyan": true, "white": true, "gray": true, "grey": true,
	"bright-black": true, "bright-red": true, "bright-green": true, "bright-yellow": true,
	"bright-blue": true, "bright-magenta": true, "bright-cyan": true, "bright-white": true,
}

var hexColor = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

func compile(cat config.Category, p Paths) (Rule, []string, bool) {
	r := Rule{Name: cat.Name}
	var problems []string
	usable := true

	if !validName.MatchString(cat.Name) {
		problems = append(problems, "name may use only letters, digits, - and _")
		usable = false
	}

	// Presentation problems keep the rule and fall back to the default: a wrong colour
	// is no reason to lose the category.
	switch c := strings.ToLower(cat.Color); {
	case c == "" || colors[c]:
		r.Color = c
	case hexColor.MatchString(c):
		r.Color = c
	default:
		problems = append(problems, fmt.Sprintf("color %q is not a colour name or #rrggbb; using the default", cat.Color))
	}
	switch utf8.RuneCountInString(cat.Mark) {
	case 0:
		first, _ := utf8.DecodeRuneInString(cat.Name)
		r.Mark = strings.ToUpper(string(first))
	case 1:
		r.Mark = cat.Mark
	default:
		first, _ := utf8.DecodeRuneInString(cat.Name)
		r.Mark = strings.ToUpper(string(first))
		problems = append(problems, fmt.Sprintf("mark %q must be one character; using %q", cat.Mark, r.Mark))
	}

	if len(cat.Commands) > 0 {
		r.commands = map[string]bool{}
		for _, w := range cat.Commands {
			if w = strings.TrimSpace(w); w != "" {
				r.commands[strings.ToLower(w)] = true
			}
		}
	}
	if cat.Match != "" {
		re, err := regexp.Compile(cat.Match)
		if err != nil {
			problems = append(problems, "match: "+err.Error())
			usable = false
		}
		r.match = re
	}
	for _, g := range cat.Cwd {
		re, err := globRegexp(g, p)
		if err != nil {
			problems = append(problems, fmt.Sprintf("cwd %q: %v", g, err))
			usable = false
			continue
		}
		r.cwd = append(r.cwd, re)
	}
	for _, g := range cat.Scripts {
		re, err := globRegexp(g, p)
		if err != nil {
			problems = append(problems, fmt.Sprintf("scripts %q: %v", g, err))
			usable = false
			continue
		}
		r.scripts = append(r.scripts, re)
	}
	r.paths = p
	if len(r.commands) == 0 && r.match == nil && len(r.cwd) == 0 && len(r.scripts) == 0 && usable {
		problems = append(problems, "has no commands, match, cwd or scripts, so it matches nothing")
		usable = false
	}
	return r, problems, usable
}

// Field names which part of a rule matched, for --explain.
type Field string

const (
	FieldCommands Field = "commands"
	FieldMatch    Field = "match"
	FieldCwd      Field = "cwd"
	FieldScripts  Field = "scripts"
)

// Why is one rule's verdict on one command.
type Why struct {
	Rule  int
	Name  string
	Field Field
	What  string // the word, the matched text, or the pattern that matched
}

// Classify returns the indexes of every rule that matches, in priority order. A chain or
// a block matches nothing (DESIGN §17.1).
func (s *Set) Classify(cmd, cwd string) []int {
	if s == nil || len(s.Rules) == 0 || Compound(cmd) != "" {
		return nil
	}
	var out []int
	word := firstWord(cmd)
	for i := range s.Rules {
		if _, ok := s.Rules[i].test(cmd, cwd, word); ok {
			out = append(out, i)
		}
	}
	return out
}

// Explain says which rules matched and on what, or, when none could, why not.
func (s *Set) Explain(cmd, cwd string) (matches []Why, reason string) {
	if s == nil || len(s.Rules) == 0 {
		return nil, "no categories are configured"
	}
	if c := Compound(cmd); c != "" {
		return nil, "it is " + c + ", and those are never categorised"
	}
	word := firstWord(cmd)
	for i := range s.Rules {
		if w, ok := s.Rules[i].test(cmd, cwd, word); ok {
			w.Rule, w.Name = i, s.Rules[i].Name
			matches = append(matches, w)
		}
	}
	if len(matches) == 0 {
		return nil, fmt.Sprintf("no rule matched (first word %q)", word)
	}
	return matches, ""
}

func (r *Rule) test(cmd, cwd, word string) (Why, bool) {
	if r.commands != nil {
		for _, w := range wordForms(word) {
			if r.commands[w] {
				return Why{Field: FieldCommands, What: w}, true
			}
		}
	}
	if r.match != nil {
		if loc := r.match.FindStringIndex(cmd); loc != nil {
			return Why{Field: FieldMatch, What: cmd[loc[0]:loc[1]]}, true
		}
	}
	if cwd != "" {
		d := strings.TrimRight(cwd, `\/`)
		for _, re := range r.cwd {
			if re.MatchString(d) {
				return Why{Field: FieldCwd, What: re.String()}, true
			}
		}
	}
	if len(r.scripts) > 0 {
		if file, ok := resolveScript(word, cwd, r.paths); ok {
			for _, re := range r.scripts {
				if re.MatchString(file) {
					return Why{Field: FieldScripts, What: file}, true
				}
			}
		}
	}
	return Why{}, false
}

// resolveScript is the file a first word runs, when the word is a path: `scripts\x.ps1`,
// `.\x.ps1` and `..\x.ps1` against the directory it ran in, `~\…` against home, and an
// absolute path as it is. A bare word is not resolved, because pwsh looks those up on
// PATH and finding out where would mean touching the disk. Pure string work: nothing is
// stat'ed, so it is safe on the prompt path (C-040).
func resolveScript(word, cwd string, p Paths) (string, bool) {
	sep := "/"
	if p.Windows {
		sep = `\`
		word = strings.ReplaceAll(word, "/", `\`)
		cwd = strings.ReplaceAll(cwd, "/", `\`)
	}
	if word == "" || (!strings.Contains(word, sep) && word != "~") {
		return "", false
	}
	var full string
	switch {
	case word == "~" || strings.HasPrefix(word, "~"+sep):
		if p.Home == "" {
			return "", false
		}
		full = strings.TrimRight(p.Home, `\/`) + word[1:]
	case isAbs(word, p.Windows):
		full = word
	case p.Windows && strings.HasPrefix(word, `\`) && len(cwd) >= 2 && cwd[1] == ':':
		full = cwd[:2] + word // \x.ps1 is the root of the current drive
	default:
		if cwd == "" {
			return "", false
		}
		full = strings.TrimRight(cwd, sep) + sep + word
	}
	return cleanPath(full, sep), true
}

func isAbs(path string, windows bool) bool {
	if !windows {
		return strings.HasPrefix(path, "/")
	}
	return strings.HasPrefix(path, `\\`) || (len(path) >= 3 && path[1] == ':' && path[2] == '\\')
}

// cleanPath resolves `.` and `..` and repeated separators, keeping the root: a drive
// (`C:`), a UNC share's leading `\\`, or `/`. `..` never climbs above the root.
func cleanPath(path, sep string) string {
	root := ""
	switch {
	case sep == `\` && strings.HasPrefix(path, `\\`):
		root, path = `\\`, path[2:]
	case sep == `\` && len(path) >= 2 && path[1] == ':':
		root, path = path[:2]+`\`, path[2:]
	case sep == "/" && strings.HasPrefix(path, "/"):
		root = "/"
	}
	var parts []string
	for _, part := range strings.Split(path, sep) {
		switch part {
		case "", ".":
		case "..":
			if len(parts) > 0 {
				parts = parts[:len(parts)-1]
			}
		default:
			parts = append(parts, part)
		}
	}
	return root + strings.Join(parts, sep)
}

// firstWord is the command's first word as a shell would run it: after a call or
// dot-source operator, and without quotes.
func firstWord(cmd string) string {
	s := strings.TrimLeft(cmd, " \t\r\n")
	// `& 'C:\tools\x.exe'` and `. .\setup.ps1`: the operator is not the command. A path
	// that starts with `.\` is, so the operator must be followed by space.
	if len(s) > 1 && (s[0] == '&' || s[0] == '.') && (s[1] == ' ' || s[1] == '\t') {
		s = strings.TrimLeft(s[1:], " \t")
	}
	if s == "" {
		return ""
	}
	if q := s[0]; q == '\'' || q == '"' {
		if end := strings.IndexByte(s[1:], q); end >= 0 {
			return s[1 : end+1]
		}
		return s[1:]
	}
	if end := strings.IndexAny(s, " \t\r\n|;"); end >= 0 {
		return s[:end]
	}
	return s
}

// wordForms are the lower-cased names a first word can be listed under: as written, its
// file name, and its file name without extension. So `.\scripts\Get-EsTree.ps1` is found
// by `Get-EsTree`, and `git.exe` by `git`.
func wordForms(word string) []string {
	w := strings.ToLower(word)
	forms := []string{w}
	base := w
	if i := strings.LastIndexAny(w, `\/`); i >= 0 {
		base = w[i+1:]
		forms = append(forms, base)
	}
	if ext := filepath.Ext(base); ext != "" && ext != base {
		forms = append(forms, strings.TrimSuffix(base, ext))
	}
	return forms
}

// Compound says whether a command is more than one simple statement or pipeline: "a
// chain" for statements joined by `;`, `&&`, `||` or a new line, "a block" for a
// command that is a script block. Empty means it can be categorised.
//
// This is a scan, not a parse: Go has no PowerShell parser. It tracks quotes and
// brackets, so a `;` inside a string or a `{ … }` does not count, and it knows the ways
// a line continues (a trailing backtick or pipe, a leading pipe). The shell will record
// the parser's own answer beside each command (S-034); until then this is close enough
// for a label.
func Compound(cmd string) string {
	s := strings.TrimSpace(cmd)
	if strings.HasPrefix(s, "{") || strings.HasPrefix(s, "& {") || strings.HasPrefix(s, "&{") ||
		strings.HasPrefix(s, ". {") {
		return "a block"
	}
	depth := 0
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			switch {
			case c == '`' && quote == '"':
				i++ // escaped character inside a double-quoted string
			case c == quote:
				quote = 0
			}
			continue
		}
		switch c {
		case '`':
			i++ // escaped character, including a line continuation
		case '\'', '"':
			quote = c
		case '#':
			// A comment runs to the end of the line; `<#` block comments are rare in
			// typed commands and not worth the code.
			if i == 0 || s[i-1] == ' ' || s[i-1] == '\t' {
				for i < len(s) && s[i] != '\n' {
					i++
				}
				i--
			}
		case '(', '{', '[':
			depth++
		case ')', '}', ']':
			if depth > 0 {
				depth--
			}
		case ';':
			if depth == 0 {
				return "a chain"
			}
		case '&', '|':
			if depth == 0 && i+1 < len(s) && s[i+1] == c {
				return "a chain"
			}
		case '\n':
			if depth == 0 && !continues(s, i) {
				return "a chain"
			}
		}
	}
	return ""
}

// continues reports whether the new line at s[i] carries on the same statement: the line
// before it ends in a pipe, or the line after it starts with one (PowerShell 7).
func continues(s string, i int) bool {
	before := strings.TrimRight(s[:i], " \t\r")
	if strings.HasSuffix(before, "|") {
		return true
	}
	after := strings.TrimLeft(s[i+1:], " \t\r\n")
	return strings.HasPrefix(after, "|") && !strings.HasPrefix(after, "||")
}

// globRegexp compiles a cwd glob. `**` crosses directories, `*` and `?` stay within one,
// `~` at the start is the home directory, and a trailing `\**` also matches the directory
// itself, so `~\dev\**` includes `~\dev`. On Windows `/` and `\` are the same and case is
// ignored, as the file system does.
func globRegexp(glob string, p Paths) (*regexp.Regexp, error) {
	g := strings.TrimSpace(glob)
	if g == "" {
		return nil, fmt.Errorf("empty")
	}
	if g == "~" || strings.HasPrefix(g, `~\`) || strings.HasPrefix(g, "~/") {
		if p.Home == "" {
			return nil, fmt.Errorf("~ used but the home directory is unknown")
		}
		g = strings.TrimRight(p.Home, `\/`) + g[1:]
	}
	sep, notSep := "/", `[^/]`
	if p.Windows {
		g = strings.ReplaceAll(g, "/", `\`)
		sep, notSep = `\`, `[^\\]`
	}
	var b strings.Builder
	if p.Windows {
		b.WriteString("(?i)")
	}
	b.WriteString("^")
	for i := 0; i < len(g); i++ {
		switch c := g[i]; {
		case c == '*' && i+1 < len(g) && g[i+1] == '*':
			// `\**` at the very end also matches the directory itself.
			if i+2 == len(g) && i > 0 && g[i-1] == sep[0] {
				str := b.String()
				b.Reset()
				b.WriteString(str[:len(str)-len(regexp.QuoteMeta(sep))])
				b.WriteString("(" + regexp.QuoteMeta(sep) + ".*)?")
			} else {
				b.WriteString(".*")
			}
			i++
		case c == '*':
			b.WriteString(notSep + "*")
		case c == '?':
			b.WriteString(notSep)
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString("$")
	return regexp.Compile(b.String())
}

// Labeler remembers what each command was classified as, so the finder classifies a
// command once per session rather than once per keystroke. The key includes the
// directory, because a cwd rule can label the same command differently by where it ran.
// Not safe for concurrent use; the finder draws on one goroutine.
type Labeler struct {
	Set   *Set
	cache map[string][]int
}

// NewLabeler wraps s. A nil or empty set labels nothing.
func NewLabeler(s *Set) *Labeler {
	return &Labeler{Set: s, cache: map[string][]int{}}
}

// Labels is Classify, remembered.
func (l *Labeler) Labels(cmd, cwd string) []int {
	if l == nil || l.Set == nil || len(l.Set.Rules) == 0 {
		return nil
	}
	key := cwd + "\x00" + cmd
	if v, ok := l.cache[key]; ok {
		return v
	}
	v := l.Set.Classify(cmd, cwd)
	l.cache[key] = v
	return v
}

// Has reports whether rule i is among the labels.
func (l *Labeler) Has(cmd, cwd string, i int) bool {
	for _, j := range l.Labels(cmd, cwd) {
		if j == i {
			return true
		}
	}
	return false
}

// Index is the position of the named rule, or -1.
func (s *Set) Index(name string) int {
	if s == nil {
		return -1
	}
	for i, r := range s.Rules {
		if strings.EqualFold(r.Name, name) {
			return i
		}
	}
	return -1
}
