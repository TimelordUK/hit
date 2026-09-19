// Package store reads, merges and appends hit's JSONL history (DESIGN §4.2–4.3).
package store

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"

	"github.com/TimelordUK/hit/internal/record"
)

// Stats counts what the reader saw. Skipped lines are never fatal, but
// `hit doctor` can report them.
type Stats struct {
	Lines   int // non-blank lines read
	Corrupt int // lines that are not valid JSON (torn writes, garbage)
	Unknown int // valid JSON but an unknown kind or missing required fields
}

var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// Read parses every line of r. It never fails on content: torn or corrupt lines,
// unknown kinds and incomplete records are skipped and counted. Only an I/O error
// from r is returned, together with the records read before it.
// Lines have no length limit (a pasted JSON body can be a very long command).
func Read(r io.Reader) ([]record.Record, Stats, error) {
	var (
		recs  []record.Record
		st    Stats
		br    = bufio.NewReaderSize(r, 64*1024)
		first = true
	)
	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			if first {
				line = bytes.TrimPrefix(line, utf8BOM)
			}
			first = false
			line = bytes.TrimRight(line, "\r\n")
			if len(bytes.TrimSpace(line)) > 0 {
				st.Lines++
				rec, derr := record.Decode(line)
				switch {
				case derr != nil:
					st.Corrupt++
				case !rec.Valid():
					st.Unknown++
				default:
					recs = append(recs, rec)
				}
			}
		}
		if errors.Is(err, io.EOF) {
			return recs, st, nil
		}
		if err != nil {
			return recs, st, err
		}
	}
}

// ReadFile reads the history file at path. A missing file is an empty history.
func ReadFile(path string) ([]record.Record, Stats, error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, Stats{}, nil
	}
	if err != nil {
		return nil, Stats{}, err
	}
	defer f.Close()
	return Read(f)
}
