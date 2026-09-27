package record

import (
	"bytes"
	"math/rand"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/oklog/ulid/v2"
)

func TestEncodeIsOneLineWithKindFirst(t *testing.T) {
	r := &Record{K: KindCmd, ID: "01J8Z0000000000000000000AA", Cmd: "a\nb\r\nc\td", Cwd: `\\elastic-prod-1\logs`}
	b, err := Encode(r)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(b, []byte("\n")) || bytes.Count(b, []byte("\n")) != 1 || bytes.ContainsRune(b, '\r') {
		t.Fatalf("not a single line: %q", b)
	}
	if !bytes.HasPrefix(b, []byte(`{"k":"cmd",`)) {
		t.Fatalf("k is not first: %s", b)
	}
}

func TestEncodeKeepsShellCharactersReadable(t *testing.T) {
	b, _ := Encode(&Record{K: KindCmd, ID: "x", Cmd: `a && b > c < d`})
	if !bytes.Contains(b, []byte(`a && b > c < d`)) {
		t.Fatalf("HTML-escaped: %s", b)
	}
}

func TestEncodeRejectsInvalid(t *testing.T) {
	for _, r := range []*Record{
		{K: KindCmd, ID: "x"},
		{K: KindCmd, Cmd: "ls"},
		{K: KindEnd},
		{K: KindDel},
		{K: KindCd},
		{K: KindEnv, Vars: map[string]*EnvValue{}},
		{K: KindEnv, TS: "2026-09-27T10:00:00.000Z"},
		{K: "nope", ID: "x", Cmd: "ls"},
	} {
		if _, err := Encode(r); err != ErrInvalid {
			t.Errorf("%+v: want ErrInvalid, got %v", r, err)
		}
	}
}

func TestExitZeroIsKept(t *testing.T) {
	zero, ms := 0, int64(0)
	b, _ := Encode(&Record{K: KindEnd, ID: "x", Exit: &zero, Ms: &ms})
	if !bytes.Contains(b, []byte(`"exit":0`)) || !bytes.Contains(b, []byte(`"ms":0`)) {
		t.Fatalf("exit/ms 0 dropped: %s", b)
	}
	got, _ := Decode(b)
	if got.Exit == nil || *got.Exit != 0 {
		t.Fatalf("exit not decoded: %+v", got)
	}
}

func TestTime(t *testing.T) {
	r := Record{TS: "2026-09-19T10:12:03.412Z"}
	want := time.Date(2026, 9, 19, 10, 12, 3, 412e6, time.UTC)
	if !r.Time().Equal(want) {
		t.Fatalf("got %v", r.Time())
	}
	if !(&Record{TS: "yesterday"}).Time().IsZero() {
		t.Fatal("bad ts should give zero time")
	}
	if got := FormatTime(want.In(time.FixedZone("x", 3600))); got != "2026-09-19T10:12:03.412Z" {
		t.Fatalf("FormatTime = %s", got)
	}
}

func TestNewIDSortsByTime(t *testing.T) {
	src := rand.New(rand.NewSource(1))
	t0 := time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC)
	a := NewID(t0, src)
	b := NewID(t0.Add(time.Millisecond), src)
	if len(a) != 26 || a >= b {
		t.Fatalf("ids not sortable: %s %s", a, b)
	}
	id, err := ulid.Parse(a)
	if err != nil || !ulid.Time(id.Time()).Equal(t0) {
		t.Fatalf("id time: %v %v", ulid.Time(id.Time()), err)
	}
}

// Any valid UTF-8 command comes back byte-exact, on exactly one line (DESIGN §2.3).
// Invalid UTF-8 can't be represented in JSON; the shells never produce it.
func FuzzCommandRoundTrip(f *testing.F) {
	for _, s := range []string{
		"ls",
		"Invoke-RestMethod `\n    -Uri https://elastic-prod-1:9200/_search `\n    -Method Post",
		"a\r\nb", "tab\there", `C:\dev\ "quoted" \\srv\share`, "emoji 🦀 ✓", "\u2028\u2029", "\x00\x1b[31m",
		strings.Repeat("x", 100_000),
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, cmd string) {
		if cmd == "" || !utf8.ValidString(cmd) {
			t.Skip()
		}
		line, err := Encode(&Record{K: KindCmd, ID: "x", Cmd: cmd})
		if err != nil {
			t.Fatal(err)
		}
		if i := bytes.IndexAny(line, "\r\n"); i != len(line)-1 {
			t.Fatalf("line break inside encoded record at %d", i)
		}
		got, err := Decode(line)
		if err != nil {
			t.Fatal(err)
		}
		if got.Cmd != cmd {
			t.Fatalf("round trip changed command:\n in: %q\nout: %q", cmd, got.Cmd)
		}
	})
}

func TestEnvRoundTrip(t *testing.T) {
	prod := "prod"
	r := &Record{K: KindEnv, TS: "2026-09-27T10:00:00.000Z", Sh: "pwsh", Sid: "s", Vars: map[string]*EnvValue{
		"ELASTIC_ENV": {Value: prod},
		"ES_PASSWORD": {Hidden: true},
		"AWS_PROFILE": nil,
	}}
	b, err := Encode(r)
	if err != nil {
		t.Fatal(err)
	}
	want := `"vars":{"AWS_PROFILE":null,"ELASTIC_ENV":"prod","ES_PASSWORD":{"set":true}}`
	if !bytes.Contains(b, []byte(want)) {
		t.Fatalf("got %s, want it to contain %s", b, want)
	}
	got, err := Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Valid() || got.Vars["AWS_PROFILE"] != nil || *got.Vars["ELASTIC_ENV"] != (EnvValue{Value: prod}) ||
		!got.Vars["ES_PASSWORD"].Hidden {
		t.Fatalf("round trip: %+v", got.Vars)
	}
}

// Nothing listed is set: still a snapshot, so the empty map is written, not dropped.
func TestEnvWithNoVarsIsKept(t *testing.T) {
	b, err := Encode(&Record{K: KindEnv, TS: "2026-09-27T10:00:00.000Z", Vars: map[string]*EnvValue{}})
	if err != nil || !bytes.Contains(b, []byte(`"vars":{}`)) {
		t.Fatalf("%s %v", b, err)
	}
	if b, _ := Encode(&Record{K: KindCd, Dir: "x"}); bytes.Contains(b, []byte("vars")) {
		t.Fatalf("vars on a cd record: %s", b)
	}
}

func TestEnvRejectsAHiddenValueThatIsNotSet(t *testing.T) {
	if _, err := Decode([]byte(`{"k":"env","ts":"2026-09-27T10:00:00.000Z","vars":{"X":{"set":false}}}`)); err == nil {
		t.Fatal("want error: an unset variable is null")
	}
}
