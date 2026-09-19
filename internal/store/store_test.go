package store

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/TimelordUK/hit/internal/record"
)

func read(t *testing.T, s string) ([]record.Record, Stats) {
	t.Helper()
	recs, st, err := Read(strings.NewReader(s))
	if err != nil {
		t.Fatal(err)
	}
	return recs, st
}

func TestReadIsTolerant(t *testing.T) {
	in := "\xEF\xBB\xBF" + // BOM from an editor
		`{"k":"cmd","id":"1","cmd":"ls"}` + "\r\n" + // CRLF
		"\n   \n" + // blank lines
		`{"k":"cmd","id":"2","cmd":"garbage` + "\n" + // torn write
		"not json at all\n" +
		`{"k":"future-kind","id":"3"}` + "\n" + // unknown kind
		`{"k":"cmd","id":"4"}` + "\n" + // missing cmd
		`{"k":"cmd","id":"5","cmd":"dir","new_key":[1,2]}` + "\n" + // unknown key
		`{"k":"cmd","id":"6","cmd":"no trailing newline"}`
	recs, st := read(t, in)
	var ids []string
	for _, r := range recs {
		ids = append(ids, r.ID)
	}
	if strings.Join(ids, ",") != "1,5,6" {
		t.Fatalf("ids = %v", ids)
	}
	if st != (Stats{Lines: 7, Corrupt: 2, Unknown: 2}) {
		t.Fatalf("stats = %+v", st)
	}
}

func TestReadVeryLongLine(t *testing.T) {
	cmd := strings.Repeat("a", 1<<20)
	b, _ := record.Encode(&record.Record{K: record.KindCmd, ID: "1", Cmd: cmd})
	recs, _ := read(t, string(b)+string(b))
	if len(recs) != 2 || recs[1].Cmd != cmd {
		t.Fatalf("long lines lost: %d records", len(recs))
	}
}

type failingReader struct{ n int }

func (f *failingReader) Read(p []byte) (int, error) {
	if f.n == 0 {
		f.n++
		return copy(p, `{"k":"cmd","id":"1","cmd":"ls"}`+"\n"), nil
	}
	return 0, errors.New("disk on fire")
}

func TestReadReturnsIOErrorWithRecordsSoFar(t *testing.T) {
	recs, _, err := Read(&failingReader{})
	if err == nil || len(recs) != 1 {
		t.Fatalf("recs=%d err=%v", len(recs), err)
	}
}

func TestReadFileMissingIsEmpty(t *testing.T) {
	recs, st, err := ReadFile(filepath.Join(t.TempDir(), "nope.jsonl"))
	if err != nil || len(recs) != 0 || st != (Stats{}) {
		t.Fatalf("recs=%v st=%v err=%v", recs, st, err)
	}
}

func TestBuildMergesEndsAndTombstones(t *testing.T) {
	recs, _ := read(t, strings.Join([]string{
		`{"k":"end","id":"B","exit":1,"ms":20}`, // end before its cmd (other file read first)
		`{"k":"cmd","id":"A","ts":"2026-09-19T10:00:00.000Z","cmd":"one","cwd":"C:\\dev"}`,
		`{"k":"cmd","id":"B","cmd":"two"}`,
		`{"k":"end","id":"A","exit":0,"ms":5}`,
		`{"k":"cmd","id":"C","cmd":"three"}`,
		`{"k":"cmd","id":"D","cmd":"deleted"}`,
		`{"k":"end","id":"D","exit":0}`,
		`{"k":"del","id":"D"}`,
		`{"k":"del","id":"never-existed"}`,
		`{"k":"cmd","id":"A","cmd":"duplicate id"}`,
		`{"k":"end","id":"A","exit":9}`, // second end for A is ignored
		`{"k":"cd","ts":"2026-09-19T10:00:01.000Z","dir":"\\\\elastic-prod-1\\logs","sh":"pwsh"}`,
	}, "\n"))
	h := Build(recs)

	type row struct {
		id, cmd string
		exit    int // -1 = unknown
		ms      int64
	}
	var got []row
	for _, e := range h.Entries {
		r := row{e.ID, e.Cmd, -1, -1}
		if e.Exit != nil {
			r.exit = *e.Exit
		}
		if e.Ms != nil {
			r.ms = *e.Ms
		}
		got = append(got, r)
	}
	want := []row{{"A", "one", 0, 5}, {"B", "two", 1, 20}, {"C", "three", -1, -1}}
	if len(got) != len(want) {
		t.Fatalf("entries = %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("entry %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	if h.Entries[0].Cwd != `C:\dev` || h.Entries[0].Time.IsZero() {
		t.Errorf("entry A fields: %+v", h.Entries[0])
	}
	if len(h.Visits) != 1 || h.Visits[0].Dir != `\\elastic-prod-1\logs` {
		t.Errorf("visits = %+v", h.Visits)
	}
}

func TestAppendThenRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "history.jsonl")
	exit := 0
	cmd := "Invoke-RestMethod `\r\n    -Uri x"
	if err := Append(path,
		&record.Record{K: record.KindCmd, ID: "1", Cmd: cmd},
		&record.Record{K: record.KindEnd, ID: "1", Exit: &exit},
	); err != nil {
		t.Fatal(err)
	}
	if err := Append(path, &record.Record{K: record.KindDel, ID: "2"}); err != nil {
		t.Fatal(err)
	}
	recs, st, err := ReadFile(path)
	if err != nil || len(recs) != 3 || st.Lines != 3 {
		t.Fatalf("recs=%d st=%+v err=%v", len(recs), st, err)
	}
	if recs[0].Cmd != cmd {
		t.Fatalf("cmd changed: %q", recs[0].Cmd)
	}
}

func TestAppendRejectsInvalidAndWritesNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	err := Append(path,
		&record.Record{K: record.KindCmd, ID: "1", Cmd: "ok"},
		&record.Record{K: record.KindCmd, ID: "2"},
	)
	if err == nil {
		t.Fatal("want error")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("file written despite invalid record: %v", err)
	}
}

// A damaged history file must never crash the reader.
func FuzzRead(f *testing.F) {
	f.Add([]byte(`{"k":"cmd","id":"1","cmd":"ls"}` + "\n" + `{"k":"end","id":"1","exit":0}`))
	f.Add([]byte(`{"k":"cmd","id":"1","cmd":"l`))
	f.Add([]byte("\xEF\xBB\xBF\r\n\x00{}[]\"\\"))
	f.Add([]byte(`{"k":"end","id":"1","exit":"zero","ms":1e400}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		recs, st, err := Read(strings.NewReader(string(data)))
		if err != nil {
			t.Fatal(err)
		}
		if len(recs)+st.Corrupt+st.Unknown != st.Lines {
			t.Fatalf("lines unaccounted for: %d recs, %+v", len(recs), st)
		}
		Build(recs)
	})
}
