// Package contract checks the hand-off between the shell writers and the Go reader
// (DESIGN §13.2). Every writer turns the shared cases in testdata/contract/cases.json into
// records; each record must match schema/record.schema.json and read back unchanged.
//
// Set HIT_REQUIRE_SHELLS=1 (CI does) to fail instead of skip when a shell is missing.
package contract

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/TimelordUK/hit/internal/record"
	"github.com/TimelordUK/hit/internal/store"
	"github.com/oklog/ulid/v2"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

var root = filepath.Join("..", "..")

type testCase struct {
	Name     string        `json:"name"`
	Generate bool          `json:"generate"`
	Record   record.Record `json:"record"`
}

func loadCases(t *testing.T) []testCase {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, "testdata", "contract", "cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cases []testCase
	if err := json.Unmarshal(b, &cases); err != nil {
		t.Fatal(err)
	}
	return cases
}

func loadSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	path, err := filepath.Abs(filepath.Join(root, "schema", "record.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	s, err := jsonschema.NewCompiler().Compile(path)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func validate(t *testing.T, s *jsonschema.Schema, name string, line []byte) {
	t.Helper()
	v, err := jsonschema.UnmarshalJSON(bytes.NewReader(line))
	if err != nil {
		t.Errorf("%s: not JSON: %v\n%s", name, err, line)
		return
	}
	if err := s.Validate(v); err != nil {
		t.Errorf("%s: violates schema: %v\n%s", name, err, line)
	}
}

// The Go writer, and the fixtures themselves, match the schema.
func TestGoWriter(t *testing.T) {
	s := loadSchema(t)
	for _, c := range loadCases(t) {
		if c.Generate {
			continue
		}
		line, err := record.Encode(&c.Record)
		if err != nil {
			t.Errorf("%s: %v", c.Name, err)
			continue
		}
		validate(t, s, c.Name, line)
	}
}

func TestSchemaRejectsBadRecords(t *testing.T) {
	s := loadSchema(t)
	for _, line := range []string{
		`{"k":"cmd","id":"01K5HQ8ZJ2A7Q3M8V4W6X9Y0ZC","ts":"2026-09-19T10:12:03.412Z","sh":"pwsh"}`,                  // no cmd
		`{"k":"cmd","id":"not-a-ulid","ts":"2026-09-19T10:12:03.412Z","cmd":"ls","sh":"pwsh"}`,                       // bad id
		`{"k":"cmd","id":"01K5HQ8ZJ2A7Q3M8V4W6X9Y0ZC","ts":"2026-09-19 10:12","cmd":"ls","sh":"pwsh"}`,               // bad ts
		`{"k":"cmd","id":"01K5HQ8ZJ2A7Q3M8V4W6X9Y0ZC","ts":"2026-09-19T10:12:03.412Z","cmd":"ls","sh":"pwsh","x":1}`, // unknown key
		`{"k":"end","id":"01K5HQ8ZJ2A7Q3M8V4W6X9Y0ZC","exit":"0"}`,                                                   // exit not a number
		`{"k":"end","id":"01K5HQ8ZJ2A7Q3M8V4W6X9Y0ZC","cmd":"ls"}`,                                                   // key from another kind
		`{"k":"nope"}`,
	} {
		v, _ := jsonschema.UnmarshalJSON(bytes.NewReader([]byte(line)))
		if s.Validate(v) == nil {
			t.Errorf("schema accepted %s", line)
		}
	}
}

// writer runs a shell's emit script, which appends one line per case to out.
type writer struct {
	name  string
	shell string
	args  func(cases, out string) []string
}

var writers = []writer{
	{"pwsh", "pwsh", func(cases, out string) []string {
		return []string{"-NoProfile", "-NonInteractive", "-File",
			filepath.Join(root, "tests", "contract", "emit.ps1"), "-CasesPath", cases, "-OutPath", out}
	}},
}

func TestShellWriters(t *testing.T) {
	const now = "2026-09-19T10:12:03.412Z"
	nowT, _ := time.Parse(time.RFC3339Nano, now)
	s := loadSchema(t)
	cases := loadCases(t)
	casesPath := filepath.Join(root, "testdata", "contract", "cases.json")

	for _, w := range writers {
		t.Run(w.name, func(t *testing.T) {
			exe, err := exec.LookPath(w.shell)
			if err != nil {
				if os.Getenv("HIT_REQUIRE_SHELLS") != "" {
					t.Fatalf("%s not found and HIT_REQUIRE_SHELLS is set", w.shell)
				}
				t.Skipf("%s not found", w.shell)
			}
			dir := t.TempDir()
			out := filepath.Join(dir, "data", "history.jsonl") // data dir doesn't exist yet
			cmd := exec.Command(exe, w.args(casesPath, out)...)
			cmd.Env = append(os.Environ(), "HIT_NOW="+now, "HIT_DATA_DIR="+dir, "HIT_CONFIG="+filepath.Join(dir, "config.toml"))
			if b, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%s failed: %v\n%s", w.name, err, b)
			}

			raw, err := os.ReadFile(out)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.HasPrefix(raw, []byte{0xEF, 0xBB, 0xBF}) {
				t.Error("file starts with a UTF-8 BOM")
			}
			if bytes.Contains(raw, []byte("\r")) {
				t.Error("raw CR in file: lines must end in LF and CRs inside commands must be escaped")
			}
			lines := bytes.Split(bytes.TrimSuffix(raw, []byte("\n")), []byte("\n"))
			if len(lines) != len(cases) {
				t.Fatalf("got %d lines for %d cases", len(lines), len(cases))
			}

			recs, st, err := store.Read(bytes.NewReader(raw))
			if err != nil || st.Corrupt+st.Unknown != 0 || len(recs) != len(cases) {
				t.Fatalf("reader: %d records, %+v, %v", len(recs), st, err)
			}

			for i, c := range cases {
				validate(t, s, c.Name, lines[i])
				got, want := recs[i], c.Record
				if c.Generate {
					id, err := ulid.ParseStrict(got.ID)
					if err != nil || !ulid.Time(id.Time()).Equal(nowT) {
						t.Errorf("%s: id %q does not encode HIT_NOW (%v)", c.Name, got.ID, err)
					}
					if got.TS != now {
						t.Errorf("%s: ts = %q, want %q", c.Name, got.TS, now)
					}
					want.ID, want.TS = got.ID, got.TS
				}
				if !reflect.DeepEqual(got, want) {
					g, _ := json.Marshal(got)
					wa, _ := json.Marshal(want)
					t.Errorf("%s: record changed crossing the boundary\n got: %s\nwant: %s", c.Name, g, wa)
				}
			}
		})
	}
}
