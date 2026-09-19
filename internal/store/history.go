package store

import (
	"time"

	"github.com/TimelordUK/hit/internal/record"
)

// Entry is a command with its end record merged in.
type Entry struct {
	ID      string
	Time    time.Time
	Cmd     string
	Cwd     string
	Shell   string
	Host    string
	Session string
	Src     string
	Exit    *int   // nil: unknown (no end record yet, or imported)
	Ms      *int64 // nil: unknown
}

// Visit is a directory the user was in.
type Visit struct {
	Time    time.Time
	Dir     string
	Shell   string
	Host    string
	Session string
}

// History is the merged view of a set of records.
type History struct {
	Entries []Entry // file order, which is roughly time order
	Visits  []Visit // file order
}

// Build merges records into a History:
//   - end records attach to the cmd with the same id, wherever they appear
//     (with several files, an end can be read before its cmd);
//   - del records hide the cmd with that id, and its end;
//   - a repeated cmd id keeps the first occurrence.
func Build(recs []record.Record) *History {
	ends := map[string]*record.Record{}
	dels := map[string]bool{}
	for i := range recs {
		switch r := &recs[i]; r.K {
		case record.KindEnd:
			if _, seen := ends[r.ID]; !seen {
				ends[r.ID] = r
			}
		case record.KindDel:
			dels[r.ID] = true
		}
	}

	h := &History{}
	seen := map[string]bool{}
	for i := range recs {
		r := &recs[i]
		switch r.K {
		case record.KindCmd:
			if dels[r.ID] || seen[r.ID] {
				continue
			}
			seen[r.ID] = true
			e := Entry{
				ID: r.ID, Time: r.Time(), Cmd: r.Cmd, Cwd: r.Cwd,
				Shell: r.Sh, Host: r.Host, Session: r.Sid, Src: r.Src,
			}
			if end := ends[r.ID]; end != nil {
				e.Exit, e.Ms = end.Exit, end.Ms
			}
			h.Entries = append(h.Entries, e)
		case record.KindCd:
			h.Visits = append(h.Visits, Visit{
				Time: r.Time(), Dir: r.Dir, Shell: r.Sh, Host: r.Host, Session: r.Sid,
			})
		}
	}
	return h
}
