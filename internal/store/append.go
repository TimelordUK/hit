package store

import (
	"os"
	"path/filepath"

	"github.com/TimelordUK/hit/internal/record"
)

// Append writes records to the history file at path, creating it (and its
// directory) if needed. Each record is one whole line in a single write, so
// concurrent appenders never interleave inside a line.
// Locking against compaction arrives with C-004.
func Append(path string, recs ...*record.Record) error {
	var lines [][]byte
	for _, r := range recs {
		b, err := record.Encode(r)
		if err != nil {
			return err
		}
		lines = append(lines, b)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	for _, b := range lines {
		if _, err := f.Write(b); err != nil {
			f.Close()
			return err
		}
	}
	return f.Close()
}
