package dataimport_test

import (
	"errors"
	"webtyp.com/ddl"
	"webtyp.com/storage"
)

type memTable struct {
	name string
	cols []string
	rows [][]*any
}

type memSnapshot struct {
	tables []memTable
	notices []error
}

func (s *memSnapshot) Tables() ([]string, error) {
	var names []string
	for _, t := range s.tables {
		names = append(names, t.name)
	}
	return names, nil
}

func (s *memSnapshot) Columns(table string) ([]ddl.ColumnInfo, error) {
	for _, t := range s.tables {
		if t.name == table {
			var infos []ddl.ColumnInfo
			for _, c := range t.cols {
				infos = append(infos, ddl.ColumnInfo{Name: c})
			}
			return infos, nil
		}
	}
	return nil, errors.New("table not found")
}

func (s *memSnapshot) ReadTable(table string) (storage.Rows, error) {
	for _, t := range s.tables {
		if t.name == table {
			return &memRows{cols: t.cols, rows: t.rows, curr: -1}, nil
		}
	}
	return nil, errors.New("table not found")
}

func (s *memSnapshot) Close() error { return nil }

func (s *memSnapshot) Notices() []error { return s.notices }

type memRows struct {
	cols []string
	rows [][]*any
	curr int
}

func (r *memRows) Next() bool {
	r.curr++
	return r.curr < len(r.rows)
}

func (r *memRows) Scan(dest ...any) error {
	if r.curr >= len(r.rows) {
		return errors.New("EOF")
	}
	if len(dest) != len(r.cols) {
		return errors.New("dest len mismatch")
	}
	row := r.rows[r.curr]
	for i, d := range dest {
		a := d.(*any)
		if row[i] == nil {
			*a = nil
		} else {
			*a = *row[i]
		}
	}
	return nil
}

func (r *memRows) Columns() ([]string, error) {
	return r.cols, nil
}

func (r *memRows) Close() error { return nil }
func (r *memRows) Err() error   { return nil }
