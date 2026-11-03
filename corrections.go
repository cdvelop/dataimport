package dataimport

import (
	"encoding/csv"
	"io"

	wfmt "webtyp.com/fmt"
)

type Correction struct{ Table, ID, Field, Original, Corrected, Reason string }
type Corrections struct{ list []Correction; applied []bool }

const correctionsHeader = "tabla,id,campo,valor_original,valor_corregido,motivo"

var ErrCorrectionsHeader = wfmt.Err("dataimport:", "the corrections file must start with this header:", correctionsHeader)

func ReadCorrections(r io.Reader) (*Corrections, error) {
	cr := csv.NewReader(r)
	records, err := cr.ReadAll()
	if err != nil {
		return &Corrections{}, nil
	}
	if len(records) == 0 {
		return &Corrections{}, nil
	}

	header := records[0]
	if len(header) != 6 || header[0] != "tabla" || header[1] != "id" || header[2] != "campo" || header[3] != "valor_original" || header[4] != "valor_corregido" || header[5] != "motivo" {
		return nil, ErrCorrectionsHeader
	}

	var list []Correction
	for _, rec := range records[1:] {
		list = append(list, Correction{
			Table:     rec[0],
			ID:        rec[1],
			Field:     rec[2],
			Original:  rec[3],
			Corrected: rec[4],
			Reason:    rec[5],
		})
	}

	return &Corrections{list: list, applied: make([]bool, len(list))}, nil
}

func (c *Corrections) apply(t *Table, rep *Report) {
	if c == nil {
		return
	}
	for i, corr := range c.list {
		if corr.Table != t.Name {
			continue
		}

		found := false
		for _, r := range t.Rows {
			if t.ID(r) == corr.ID {
				found = true

				hasCol := false
				for _, col := range t.Columns {
					if col == corr.Field {
						hasCol = true
						break
					}
				}

				if !hasCol {
					rep.Problem(Issue{Table: t.Name, SourceID: corr.ID, Field: corr.Field, Rule: "correction_target_missing", Message: wfmt.Err("this row or column is not in the backup — remove the correction or fix its id or campo").Error()})
					break
				}

				if t.Value(r, corr.Field) != corr.Original {
					rep.Problem(Issue{Table: t.Name, SourceID: corr.ID, Field: corr.Field, Rule: "correction_stale", Message: wfmt.Err("the value in the backup is now", t.Value(r, corr.Field), "— update valor_original in the corrections file").Error()})
					break
				}

				t.set(r, corr.Field, corr.Corrected)
				c.applied[i] = true
				rep.Note(t.Name, corr.ID, "correction_applied", corr.Reason)
				break
			}
		}
		if !found {
			rep.Problem(Issue{Table: t.Name, SourceID: corr.ID, Field: corr.Field, Rule: "correction_target_missing", Message: wfmt.Err("this row or column is not in the backup — remove the correction or fix its id or campo").Error()})
		}
	}
}
