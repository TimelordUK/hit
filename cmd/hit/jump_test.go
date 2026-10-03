package main

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/TimelordUK/hit/internal/paths"
	"github.com/TimelordUK/hit/internal/record"
	"github.com/TimelordUK/hit/internal/store"
)

const jumpConfig = `
[[category]]
name     = "navigation"
commands = ["cd", "Set-Location"]
jump     = true
`

// jumpHistory writes cd commands each followed by the cd record the prompt would write,
// in one session, on Windows paths whatever the test runs on.
func jumpHistory(t *testing.T, config string, dirs ...string) paths.Env {
	t.Helper()
	env := isolated(t, config)
	env.GOOS = "windows"
	hist, err := paths.History(env)
	if err != nil {
		t.Fatal(err)
	}
	var recs []*record.Record
	for i, d := range dirs {
		ts := "2026-10-03T09:00:" + string('0'+rune(i/10)) + string('0'+rune(i%10)) + ".000Z"
		recs = append(recs,
			&record.Record{K: record.KindCmd, ID: ulidN(i), TS: ts, Cmd: "cd " + d, Cwd: `C:\Users\owner`, Sh: "pwsh", Sid: "s1"},
			&record.Record{K: record.KindCd, TS: strings.Replace(ts, ".000Z", ".500Z", 1), Dir: d, Sh: "pwsh", Sid: "s1"})
	}
	if err := store.Append(hist, recs...); err != nil {
		t.Fatal(err)
	}
	return env
}

func TestCdPrintsTheTarget(t *testing.T) {
	env := jumpHistory(t, jumpConfig,
		`C:\Users\owner\dev\trd-platform`, `C:\Users\owner\dev\trd-platform-launch`, `C:\Users\owner\dev\trd-platform-launch`)
	out := runCmd(t, env, "cd", "--cwd", `C:\Users\owner`, "trd-platform")
	if strings.TrimSpace(out) != `C:\Users\owner\dev\trd-platform` {
		t.Errorf("exact name should win: %q", out)
	}
}

func TestCdJSONIsWhatTheServerSends(t *testing.T) {
	env := jumpHistory(t, jumpConfig, `\\devserv001\logs`)
	var res Response
	if err := json.Unmarshal([]byte(runCmd(t, env, "cd", "--json", "--cwd", `C:\Users\owner`, "devs", "logs")), &res); err != nil {
		t.Fatal(err)
	}
	want := Response{Action: "jump", Cwd: `\\devserv001\logs`, Step: "anywhere", Exact: true}
	if !reflect.DeepEqual(res, want) {
		t.Errorf("cold: %+v, want %+v", res, want)
	}
	s := &server{env: env}
	if got := s.jump(Request{Jump: []string{"devs", "logs"}, Cwd: `C:\Users\owner`}); !reflect.DeepEqual(got, want) {
		t.Errorf("served: %+v, want %+v", got, want)
	}
}

// Opt-in: without jump = true there is no jump, however much history there is.
func TestCdWithoutAJumpCategory(t *testing.T) {
	env := jumpHistory(t, "[[category]]\nname = \"navigation\"\ncommands = [\"cd\"]\n", `C:\Users\owner\dev\trd-platform`)
	var out, errb bytes.Buffer
	if code := run([]string{"cd", "--cwd", `C:\Users\owner`, "trd"}, env, &out, &errb); code != 1 || out.Len() != 0 {
		t.Errorf("exit %d, out %q", code, out.String())
	}
	if !strings.Contains(errb.String(), "jump = true") {
		t.Errorf("should say why: %q", errb.String())
	}
	if res := (&server{env: env}).jump(Request{Jump: []string{"trd"}, Cwd: `C:\Users\owner`}); res.Action != "none" {
		t.Errorf("served: %+v", res)
	}
}

func TestCdExplain(t *testing.T) {
	env := jumpHistory(t, jumpConfig,
		`C:\Users\owner\dev\trd-platform\data\logs`, `\\devserv001\logs`, `\\devserv001\logs`)
	out := runCmd(t, env, "cd", "--explain", "--cwd", `C:\Users\owner\dev\trd-platform\source\core`, "logs")
	local := strings.Index(out, `2 up     exact`)
	share := strings.Index(out, `anywhere exact`)
	if local < 0 || share < local || !strings.Contains(out, `data\logs  <- jumps here`) {
		t.Errorf("the project's logs should come first:\n%s", out)
	}
}
