package dataimport

import (
	"errors"
	"fmt"
	"webtyp.com/ddl"
	wfmt "webtyp.com/fmt"
	"webtyp.com/orm"
)

type Mode int

const (
	Rehearse Mode = iota // everything inside a transaction that always rolls back
	Apply                // commits only with zero problems and a balanced report
)

// SourceTable declares a legacy table and its id column.
// RuleBackupNotice marks a note that comes from the backup engine (ddl.Snapshot.Notices).
const RuleBackupNotice = "backup_notice"

type SourceTable struct{ Name, IDColumn string }

type Step[T any] struct {
	Name  string   // stable, shown to users: "devices", "staff"
	After []string // steps that must run first; Run adds them when selected steps need them
	Owns  []string // legacy tables this step reconciles; each table is owned by at most one step
	Run   func(s Session[T]) error // error = unexpected failure (aborts); data problems go to s.Report.Problem
}

type Session[T any] struct {
	Report  *Report
	Modules T // what Plan.Build made on the transaction
	source  *source
}

// Table returns the legacy table (loaded once, corrections applied). Undeclared -> ErrTableNotDeclared.
func (s Session[T]) Table(name string) (*Table, error) {
	return s.source.table(name)
}

type Plan[T any] struct {
	Tables []SourceTable
	Build  func(tx *orm.DB) (T, error)
	Steps  []Step[T]
}

func (p Plan[T]) order(selected []string) ([]Step[T], error) {
	if len(selected) == 0 {
		return nil, ErrNoneSelected
	}

	byName := make(map[string]Step[T])
	owns := make(map[string]string)
	for _, s := range p.Steps {
		byName[s.Name] = s
		for _, o := range s.Owns {
			if _, ok := owns[o]; ok {
				return nil, ErrOwnedTwice
			}
			owns[o] = s.Name
		}
	}

	needed := make(map[string]bool)
	var walk func(string) error
	walk = func(name string) error {
		if needed[name] {
			return nil
		}
		s, ok := byName[name]
		if !ok {
			return fmt.Errorf("%w: %s", ErrUnknownStep, name)
		}
		needed[name] = true
		for _, a := range s.After {
			if err := walk(a); err != nil {
				return err
			}
		}
		return nil
	}
	for _, sel := range selected {
		if err := walk(sel); err != nil {
			return nil, err
		}
	}

	var ordered []Step[T]
	done := make(map[string]bool)
	inProgress := make(map[string]bool)

	var tsort func(string) error
	tsort = func(name string) error {
		if done[name] {
			return nil
		}
		if inProgress[name] {
			return ErrStepCycle
		}
		inProgress[name] = true
		s := byName[name]
		for _, a := range s.After {
			if !needed[a] {
				continue
			}
			if err := tsort(a); err != nil {
				return err
			}
		}
		inProgress[name] = false
		done[name] = true
		ordered = append(ordered, s)
		return nil
	}

	for _, s := range p.Steps {
		if needed[s.Name] {
			if err := tsort(s.Name); err != nil {
				return nil, err
			}
		}
	}

	return ordered, nil
}

// StepNames lists the steps in execution order (what a UI offers).
func (p Plan[T]) StepNames() ([]string, error) {
	var all []string
	for _, s := range p.Steps {
		all = append(all, s.Name)
	}
	order, err := p.order(all)
	if err != nil && !errors.Is(err, ErrNoneSelected) {
		return nil, err
	}
	if errors.Is(err, ErrNoneSelected) {
		return nil, nil
	}
	var names []string
	for _, s := range order {
		names = append(names, s.Name)
	}
	return names, nil
}

var (
	ErrRolledBack   = wfmt.Err("dataimport:", "nothing was saved: the import was rolled back")
	ErrUnknownStep  = wfmt.Err("dataimport:", "this step does not exist in the plan — check the name you selected")
	ErrStepCycle    = wfmt.Err("dataimport:", "two or more steps wait for each other (After), so none can start — remove one of the After links")
	ErrOwnedTwice   = wfmt.Err("dataimport:", "two steps own the same table — a table is reconciled by only one step")
	ErrNoneSelected = wfmt.Err("dataimport:", "no step was selected — choose at least one")
)

type source struct {
	snap   ddl.Snapshot
	tables []SourceTable
	fixes  *Corrections
	rep    *Report
	loaded map[string]*Table
}

func (s *source) table(name string) (*Table, error) {
	if s.loaded == nil {
		s.loaded = make(map[string]*Table)
	}
	if t, ok := s.loaded[name]; ok {
		return t, nil
	}

	idCol := ""
	for _, t := range s.tables {
		if t.Name == name {
			idCol = t.IDColumn
			break
		}
	}
	if idCol == "" {
		return nil, fmt.Errorf("%w: %s", ErrTableNotDeclared, name)
	}

	t, err := loadTable(s.snap, name, idCol)
	if err != nil {
		return nil, err
	}
	s.fixes.apply(t, s.rep)
	s.loaded[name] = t
	return t, nil
}

// Run executes the selected steps plus every step they need (After, transitively), in topological order
// (ties: declaration order), on one transaction.
//
// Example:
//
//	rep, err := dataimport.Run(db, snap, fixes, plan, []string{"staff"}, dataimport.Rehearse)
func Run[T any](db *orm.DB, snap ddl.Snapshot, fixes *Corrections, p Plan[T], selected []string, mode Mode) (*Report, error) {
	order, err := p.order(selected) // validates: ErrNoneSelected, ErrUnknownStep (wrapped with the name), ErrStepCycle, ErrOwnedTwice
	if err != nil {
		return nil, err
	}
	rep := &Report{}
	// What the engine tolerated while opening the backup reaches the person reading the report.
	for _, n := range snap.Notices() {
		rep.Note("", "", RuleBackupNotice, n.Error())
	}
	src := &source{snap: snap, tables: p.Tables, fixes: fixes, rep: rep}
	txErr := db.Tx(func(tx *orm.DB) error {
		mods, err := p.Build(tx)
		if err != nil {
			return err
		}
		for _, st := range order {
			for _, owned := range st.Owns {
				t, err := src.table(owned)
				if err != nil {
					return fmt.Errorf("%s %s: %w", wfmt.Err("step").Error(), st.Name, err)
				}
				rep.setSource(owned, len(t.Rows))
			}
			if err := st.Run(Session[T]{Report: rep, Modules: mods, source: src}); err != nil {
				return fmt.Errorf("%s %s: %w", wfmt.Err("step").Error(), st.Name, err)
			}
		}
		if mode == Rehearse || len(rep.Problems()) > 0 || rep.Balanced() != nil {
			return ErrRolledBack
		}
		return nil
	})
	switch {
	case txErr == nil:
		return rep, nil
	case errors.Is(txErr, ErrRolledBack) && mode == Rehearse:
		return rep, nil
	default:
		return rep, txErr
	}
}
