package search

import "testing"

// The complaint from a real day's history: subsequence matching is loose enough that
// "hit" finds Get-History, Get-ChildItem and Push-Location. A leading quote asks for the
// literal thing, following fzf, where 'wild is an exact-substring term.
func TestLiteralRejectsWhatFuzzyAccepts(t *testing.T) {
	noise := []string{
		"Get-History | Select-Object -Last 20",
		"Get-ChildItem -Path C:/temp -Recurse",
		"Push-Location ../other",
		"$h = Invoke-RestMethod -Uri https://api.example.com/status",
	}
	for _, text := range noise {
		if _, _, ok := Match(text, "hit"); !ok {
			t.Errorf("fuzzy should still match %q (that is the behaviour being narrowed)", text)
		}
		if _, _, ok := Match(text, "'hit"); ok {
			t.Errorf("literal should not match %q", text)
		}
	}
	for _, text := range []string{"hit search --print", "hit version", "run hit now"} {
		if _, _, ok := Match(text, "'hit"); !ok {
			t.Errorf("literal should match %q", text)
		}
	}
}

func TestLiteralMatchesAreContiguous(t *testing.T) {
	_, matched, ok := Match("run hit now", "'hit")
	if !ok {
		t.Fatal("should match")
	}
	want := []int{4, 5, 6}
	if len(matched) != len(want) {
		t.Fatalf("got %v, want %v", matched, want)
	}
	for i := range want {
		if matched[i] != want[i] {
			t.Fatalf("got %v, want %v", matched, want)
		}
	}
}

// Smart-case is the same promise in both modes: a lower-case pattern is case-insensitive,
// an upper-case rune demands an exact match.
func TestLiteralIsSmartCase(t *testing.T) {
	if _, _, ok := Match("Get-ChildItem", "'childitem"); !ok {
		t.Error("lower-case literal should match case-insensitively")
	}
	if _, _, ok := Match("Get-childitem", "'ChildItem"); ok {
		t.Error("upper-case literal should demand an exact match")
	}
}

// A lone quote is what you have typed the instant before the term arrives; it must not
// blank the list.
func TestLoneQuoteMatchesEverything(t *testing.T) {
	if _, _, ok := Match("anything at all", "'"); !ok {
		t.Error("a lone quote should match everything")
	}
}

// A quote anywhere but the front is ordinary text: commands are full of them.
func TestQuoteInsideThePatternIsNotSpecial(t *testing.T) {
	if _, _, ok := Match(`Write-Host 'done'`, "host 'done"); !ok {
		t.Error("an embedded quote should be matched as a character")
	}
}

// The two modes score on one scale, so a literal hit is not artificially buried or
// floated against a fuzzy one.
func TestLiteralAndFuzzyScoreTheSameWhenTheyMatchTheSameRunes(t *testing.T) {
	fuzzy, _, ok1 := Match("hit version", "hit")
	literal, _, ok2 := Match("hit version", "'hit")
	if !ok1 || !ok2 {
		t.Fatal("both should match")
	}
	if fuzzy != literal {
		t.Errorf("same runes matched but scored differently: fuzzy %v, literal %v", fuzzy, literal)
	}
}

func TestLiteralPrefersTheEarliestOccurrence(t *testing.T) {
	_, matched, ok := Match("hit and hit again", "'hit")
	if !ok {
		t.Fatal("should match")
	}
	if matched[0] != 0 {
		t.Errorf("matched at %d, want the first occurrence at 0", matched[0])
	}
}
