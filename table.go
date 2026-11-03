package dataimport

import (
	"errors"

	"webtyp.com/ddl"
)

// Table is one legacy table loaded from the snapshot, every value as text.
type Table struct {
	Name     string
	IDColumn string   // declared in Plan.Tables
	Columns  []string // snapshot order
	Rows     []Row
}

type cell struct{ text string; null bool }
type Row struct{ values []cell }

// Value returns the column's text, "" for NULL. An unknown column is a mapping bug: it panics with
// "dataimport: table <t> has no column <c>".
//
// Example: name := staff.Value(row, "staff_name")
func (t *Table) Value(r Row, col string) string { return r.values[t.index(col)].text }

// IsNull reports whether the column is NULL in r.
func (t *Table) IsNull(r Row, col string) bool { return r.values[t.index(col)].null }

// ID returns the row's legacy id (IDColumn).
func (t *Table) ID(r Row) string { return t.Value(r, t.IDColumn) }

func (t *Table) index(col string) int {
	for i, c := range t.Columns {
		if c == col {
			return i
		}
	}
	panic("dataimport: table " + t.Name + " has no column " + col)
}

func (t *Table) set(r Row, col, value string) { r.values[t.index(col)] = cell{text: value, null: false} }

var ErrTableNotDeclared = errors.New("dataimport: table not declared in Plan.Tables")

// loadTable reads every row of name from snap.
func loadTable(snap ddl.Snapshot, name, idColumn string) (*Table, error) {
	rows, err := snap.ReadTable(name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	hasID := false
	for _, c := range cols {
		if c == idColumn {
			hasID = true
			break
		}
	}
	if !hasID {
		return nil, errors.New("dataimport: table " + name + " has no id column " + idColumn)
	}

	t := &Table{Name: name, IDColumn: idColumn, Columns: cols}
	for rows.Next() {
		raw := make([]any, len(cols))
		dest := make([]any, len(cols))
		for i := range raw {
			dest[i] = &raw[i]
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}
		cells := make([]cell, len(cols))
		for i, v := range raw {
			if v == nil {
				cells[i] = cell{null: true}
			} else if s, ok := v.(string); ok {
				cells[i] = cell{text: s, null: false}
			} else if b, ok := v.([]byte); ok {
				cells[i] = cell{text: string(b), null: false}
			} else {
				return nil, errors.New("dataimport: table " + name + " column " + cols[i] + ": value is not text")
			}
		}
		t.Rows = append(t.Rows, Row{values: cells})
	}
	return t, rows.Err()
}
