// Package record defines hit's on-disk record: one JSON object per line (DESIGN §4.2).
package record

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/oklog/ulid/v2"
)

// Record kinds. Readers ignore any other value of "k" so new kinds can be added later.
const (
	KindCmd = "cmd" // command started (written before execution)
	KindEnd = "end" // command finished: exit code and duration, merged into its cmd by id
	KindCd  = "cd"  // directory visited
	KindDel = "del" // tombstone: hide the cmd with this id
	KindEnv = "env" // the captured variables changed: the state from here on in its session (C-037)
)

// TimeLayout is the timestamp format writers use: UTC, millisecond precision.
// Readers accept any RFC 3339 timestamp.
const TimeLayout = "2006-01-02T15:04:05.000Z"

// Record is one line of the history file. Short keys keep the file compact.
// Field order is the JSON key order, so "k" comes first and lines stay greppable.
type Record struct {
	K    string `json:"k"`
	ID   string `json:"id,omitempty"`
	TS   string `json:"ts,omitempty"`
	Cmd  string `json:"cmd,omitempty"`
	Cwd  string `json:"cwd,omitempty"`
	Dir  string `json:"dir,omitempty"`
	Sh   string `json:"sh,omitempty"`
	Host string `json:"host,omitempty"`
	Sid  string `json:"sid,omitempty"`
	Src  string `json:"src,omitempty"`
	Exit *int   `json:"exit,omitempty"`
	Ms   *int64 `json:"ms,omitempty"`
	// Vars is an env record's full state: every listed variable, nil when unset.
	// omitzero, not omitempty: an env record where nothing is set still writes {}.
	Vars map[string]*EnvValue `json:"vars,omitzero"`
}

// EnvValue is one captured variable (DESIGN §18). A secret-looking name is Hidden: only
// that it was set is stored, written as {"set":true}; otherwise the value, as a string.
type EnvValue struct {
	Value  string
	Hidden bool
}

func (v EnvValue) MarshalJSON() ([]byte, error) {
	if v.Hidden {
		return []byte(`{"set":true}`), nil
	}
	return json.Marshal(v.Value)
}

func (v *EnvValue) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '"' {
		*v = EnvValue{}
		return json.Unmarshal(b, &v.Value)
	}
	var h struct {
		Set bool `json:"set"`
	}
	if err := json.Unmarshal(b, &h); err != nil {
		return err
	}
	if !h.Set {
		return errors.New(`record: env value must be a string, null or {"set":true}`)
	}
	*v = EnvValue{Hidden: true}
	return nil
}

// Time parses TS. A missing or malformed timestamp gives the zero time rather than an
// error: a command with a bad timestamp is still a command worth keeping.
func (r *Record) Time() time.Time {
	t, err := time.Parse(time.RFC3339Nano, r.TS)
	if err != nil {
		return time.Time{}
	}
	return t
}

// Valid reports whether r has the fields its kind needs. Readers skip invalid records.
func (r *Record) Valid() bool {
	switch r.K {
	case KindCmd:
		return r.ID != "" && r.Cmd != ""
	case KindEnd, KindDel:
		return r.ID != ""
	case KindCd:
		return r.Dir != ""
	case KindEnv:
		return r.TS != "" && r.Vars != nil
	}
	return false
}

// ErrInvalid is returned when encoding a record that fails Valid.
var ErrInvalid = errors.New("record: missing required fields")

// Encode returns r as a single JSON line including the trailing newline. JSON escapes
// every control character, so multi-line commands can never split the line.
// HTML escaping is off so commands with <, > and & stay readable in the raw file.
func Encode(r *Record) ([]byte, error) {
	if !r.Valid() {
		return nil, ErrInvalid
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(r); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Decode parses one line (without or with its trailing newline / CRLF).
func Decode(line []byte) (Record, error) {
	var r Record
	err := json.Unmarshal(line, &r)
	return r, err
}

// FormatTime formats t the way writers store it.
func FormatTime(t time.Time) string {
	return t.UTC().Format(TimeLayout)
}

// NewID returns a ULID for time t: lexically sortable, unique across hosts without
// coordination.
func NewID(t time.Time, entropy io.Reader) string {
	return ulid.MustNew(ulid.Timestamp(t), entropy).String()
}
