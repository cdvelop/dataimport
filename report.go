package dataimport

import (
	"fmt"
	"io"
)

type Outcome int

const (
	Imported Outcome = iota
	Merged
	Discarded
)

// Issue blocks Apply. Rule is a stable identifier (snake_case); Message is for people.
type Issue struct{ Table, SourceID, Field, Rule, Message string }

// Note is informative: a rule that merged, discarded or adjusted a row, or a provisional value.
type Note struct{ Table, SourceID, Rule, Message string }

// TableCount is the reconciliation line of one owned legacy table.
type TableCount struct {
	Table                         string
	Source, Imported, Merged, Discarded int
}

type Report struct {
	counts   []TableCount
	derived  []DerivedCount
	notes    []Note
	problems []Issue
}

// DerivedCount counts target rows that do not come 1:1 from a legacy row (roles, specialties…).
type DerivedCount struct {
	Target string
	N      int
}

func (r *Report) setSource(table string, n int) {
	for i := range r.counts {
		if r.counts[i].Table == table {
			r.counts[i].Source = n
			return
		}
	}
	r.counts = append(r.counts, TableCount{Table: table, Source: n})
}

func (r *Report) Count(table string, o Outcome, n int) {
	for i := range r.counts {
		if r.counts[i].Table == table {
			switch o {
			case Imported:
				r.counts[i].Imported += n
			case Merged:
				r.counts[i].Merged += n
			case Discarded:
				r.counts[i].Discarded += n
			}
			return
		}
	}
	c := TableCount{Table: table}
	switch o {
	case Imported:
		c.Imported = n
	case Merged:
		c.Merged = n
	case Discarded:
		c.Discarded = n
	}
	r.counts = append(r.counts, c)
}

func (r *Report) Derived(target string, n int) {
	for i := range r.derived {
		if r.derived[i].Target == target {
			r.derived[i].N += n
			return
		}
	}
	r.derived = append(r.derived, DerivedCount{Target: target, N: n})
}

func (r *Report) Note(table, sourceID, rule, message string) {
	r.notes = append(r.notes, Note{Table: table, SourceID: sourceID, Rule: rule, Message: message})
}

func (r *Report) Problem(i Issue) {
	r.problems = append(r.problems, i)
}

func (r *Report) Counts() []TableCount {
	return r.counts
}

func (r *Report) DerivedCounts() []DerivedCount {
	return r.derived
}

func (r *Report) Notes() []Note {
	return r.notes
}

func (r *Report) Problems() []Issue {
	return r.problems
}

// Balanced: for every TableCount, Source == Imported+Merged+Discarded. The error names the first table and
// the difference, verbatim: "dataimport: table staff: 56 source rows, 55 accounted (imported 50, merged 0, discarded 5)".
func (r *Report) Balanced() error {
	for _, c := range r.counts {
		if c.Source != c.Imported+c.Merged+c.Discarded {
			return fmt.Errorf("dataimport: table %s: %d source rows, %d accounted (imported %d, merged %d, discarded %d)",
				c.Table, c.Source, c.Imported+c.Merged+c.Discarded, c.Imported, c.Merged, c.Discarded)
		}
	}
	return nil
}

// WriteText: counts table, derived lines, notes grouped by Rule (count + first 5 source ids), then problems.
func (r *Report) WriteText(w io.Writer) error {
	for _, c := range r.counts {
		if _, err := fmt.Fprintf(w, "table %s: %d source rows, %d accounted (imported %d, merged %d, discarded %d)\n",
			c.Table, c.Source, c.Imported+c.Merged+c.Discarded, c.Imported, c.Merged, c.Discarded); err != nil {
			return err
		}
	}
	for _, d := range r.derived {
		if _, err := fmt.Fprintf(w, "derived %s: %d\n", d.Target, d.N); err != nil {
			return err
		}
	}

	notesByRule := make(map[string][]Note)
	var rules []string
	for _, n := range r.notes {
		if len(notesByRule[n.Rule]) == 0 {
			rules = append(rules, n.Rule)
		}
		notesByRule[n.Rule] = append(notesByRule[n.Rule], n)
	}
	for _, rule := range rules {
		notes := notesByRule[rule]
		if _, err := fmt.Fprintf(w, "note %s (%d): ", rule, len(notes)); err != nil {
			return err
		}
		for i, n := range notes {
			if i >= 5 {
				if _, err := fmt.Fprint(w, "..."); err != nil {
					return err
				}
				break
			}
			if i > 0 {
				if _, err := fmt.Fprint(w, ", "); err != nil {
					return err
				}
			}
			if _, err := fmt.Fprint(w, n.SourceID); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprint(w, "\n"); err != nil {
			return err
		}
	}

	for _, p := range r.problems {
		if _, err := fmt.Fprintf(w, "problem %s: %s\n", p.Rule, p.Message); err != nil {
			return err
		}
	}

	return nil
}
