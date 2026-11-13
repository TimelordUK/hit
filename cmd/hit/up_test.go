package main

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// Up at the prompt through the server (S-009): the walk's matches, newest first, and
// nothing drawn.
func TestServedUpIsTheRecallList(t *testing.T) {
	env := isolated(t, "", "git status", "ls", "git log", "git status")
	s := &server{env: env}
	text := "git"
	got := s.up(Request{Up: &text, Session: "s1", Shell: "pwsh"})
	want := Response{Action: "recall", Cmds: []string{"git status", "git log"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

// A blank line is a question too (plain stepping), and no match is an answer, not an
// error: the shell must be able to tell "nothing matches" from "not served".
func TestServedUpBlankAndNoMatch(t *testing.T) {
	env := isolated(t, "", "a", "b")
	s := &server{env: env}
	blank, none := "", "zzz"
	if got := s.up(Request{Up: &blank}); !reflect.DeepEqual(got.Cmds, []string{"b", "a"}) {
		t.Errorf("blank: got %+v", got)
	}
	if got := s.up(Request{Up: &none}); got.Action != "recall" || len(got.Cmds) != 0 || got.Error != "" {
		t.Errorf("no match: got %+v", got)
	}
}

// What the shell sends must decode as an Up request even when the line is blank:
// omitting it would turn the request into a finder, which draws.
func TestBlankUpDecodesAsUp(t *testing.T) {
	var r Request
	if err := json.Unmarshal([]byte(`{"up":"","session":"s1","shell":"pwsh"}`), &r); err != nil {
		t.Fatal(err)
	}
	if r.Up == nil || *r.Up != "" {
		t.Errorf("blank up lost: %+v", r)
	}
	if b, _ := json.Marshal(Request{Query: "x"}); strings.Contains(string(b), `"up"`) {
		t.Errorf("a finder request carries up: %s", b)
	}
}
