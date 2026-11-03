package dataimport_test

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"webtyp.com/dataimport"
	"webtyp.com/ddl"
	"webtyp.com/orm"
	"webtyp.com/sqlite"
	"webtyp.com/storage"
	"webtyp.com/storage/conformance"
)

func setupDB(t *testing.T) *orm.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "t.db")
	conn, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open sqlite: %v", err)
	}
	compiler, _ := sqlite.DDLCompiler(conn)
	db := ddl.New(conn, compiler)
	if err := db.Sync(&conformance.Widget{}); err != nil {
		t.Fatalf("failed to sync schema: %v", err)
	}
	return orm.New(conn)
}

func countWidgets(t *testing.T, conn storage.Conn) int {
	t.Helper()
	var n int
	err := conn.QueryRow("SELECT COUNT(*) FROM " + conformance.WidgetModel.Name).Scan(&n)
	if err != nil {
		t.Fatalf("failed to count widgets: %v", err)
	}
	return n
}

func anyPtr(a any) *any { return &a }

func TestValueAndNull(t *testing.T) {
	snap := &memSnapshot{
		tables: []memTable{
			{
				name: "legacy_widget",
				cols: []string{"id", "name"},
				rows: [][]*any{
					{anyPtr("1"), nil},
				},
			},
		},
	}
	db := setupDB(t)
	plan := dataimport.Plan[*orm.DB]{
		Tables: []dataimport.SourceTable{{Name: "legacy_widget", IDColumn: "id"}},
		Build: func(tx *orm.DB) (*orm.DB, error) { return tx, nil },
		Steps: []dataimport.Step[*orm.DB]{
			{
				Name: "widgets",
				Owns: []string{"legacy_widget"},
				Run: func(s dataimport.Session[*orm.DB]) error {
					table, err := s.Table("legacy_widget")
					if err != nil {
						t.Fatal(err)
					}
					row := table.Rows[0]
					if table.Value(row, "name") != "" {
						t.Errorf("expected empty string for NULL name, got %q", table.Value(row, "name"))
					}
					if !table.IsNull(row, "name") {
						t.Error("expected IsNull to be true for NULL name")
					}
					s.Report.Count("legacy_widget", dataimport.Imported, 1)
					return nil
				},
			},
		},
	}

	_, err := dataimport.Run(db, snap, nil, plan, []string{"widgets"}, dataimport.Apply)
	if err != nil {
		t.Fatal(err)
	}
}

func TestValueUnknownColumnPanics(t *testing.T) {
	snap := &memSnapshot{
		tables: []memTable{
			{
				name: "legacy_widget",
				cols: []string{"id", "name"},
				rows: [][]*any{
					{anyPtr("1"), anyPtr("a")},
				},
			},
		},
	}
	db := setupDB(t)
	plan := dataimport.Plan[*orm.DB]{
		Tables: []dataimport.SourceTable{{Name: "legacy_widget", IDColumn: "id"}},
		Build: func(tx *orm.DB) (*orm.DB, error) { return tx, nil },
		Steps: []dataimport.Step[*orm.DB]{
			{
				Name: "widgets",
				Owns: []string{"legacy_widget"},
				Run: func(s dataimport.Session[*orm.DB]) error {
					table, err := s.Table("legacy_widget")
					if err != nil {
						t.Fatal(err)
					}
					row := table.Rows[0]

					defer func() {
						r := recover()
						if r == nil {
							t.Fatal("expected panic")
						}
						if msg, ok := r.(string); !ok || msg != "dataimport: table legacy_widget has no column nope" {
							t.Errorf("expected panic 'dataimport: table legacy_widget has no column nope', got %v", r)
						}
					}()

					table.Value(row, "nope")
					return nil
				},
			},
		},
	}

	_, _ = dataimport.Run(db, snap, nil, plan, []string{"widgets"}, dataimport.Apply)
}

func TestRehearseLeavesNoRows(t *testing.T) {
	snap := &memSnapshot{
		tables: []memTable{
			{
				name: "legacy_widget",
				cols: []string{"id", "name"},
				rows: [][]*any{
					{anyPtr("1"), anyPtr("a")},
					{anyPtr("2"), anyPtr("b")},
				},
			},
		},
	}

	dbPath := filepath.Join(t.TempDir(), "t.db")
	conn, _ := sqlite.Open(dbPath)
	compiler, _ := sqlite.DDLCompiler(conn)
	db_ddl := ddl.New(conn, compiler)
	db_ddl.Sync(&conformance.Widget{})
	db := orm.New(conn)

	plan := dataimport.Plan[*orm.DB]{
		Tables: []dataimport.SourceTable{{Name: "legacy_widget", IDColumn: "id"}},
		Build: func(tx *orm.DB) (*orm.DB, error) { return tx, nil },
		Steps: []dataimport.Step[*orm.DB]{
			{
				Name: "widgets",
				Owns: []string{"legacy_widget"},
				Run: func(s dataimport.Session[*orm.DB]) error {
					table, _ := s.Table("legacy_widget")
					for _, r := range table.Rows {
						id := table.ID(r)
						name := table.Value(r, "name")
						err := s.Modules.Create(&conformance.Widget{Id: "w"+id, Name: name})
						if err != nil {
							return err
						}
						s.Report.Count("legacy_widget", dataimport.Imported, 1)
					}
					return nil
				},
			},
		},
	}

	rep, err := dataimport.Run(db, snap, nil, plan, []string{"widgets"}, dataimport.Rehearse)
	if err != nil {
		t.Errorf("expected nil error for rehearse, got %v", err)
	}

	counts := rep.Counts()
	if len(counts) != 1 || counts[0].Table != "legacy_widget" || counts[0].Source != 2 || counts[0].Imported != 2 || counts[0].Merged != 0 || counts[0].Discarded != 0 {
		t.Errorf("unexpected counts: %+v", counts)
	}

	n := countWidgets(t, conn)
	if n != 0 {
		t.Errorf("expected 0 widgets, got %d", n)
	}
}

func TestNonTextValueRejected(t *testing.T) {
	snap := &memSnapshot{
		tables: []memTable{
			{
				name: "legacy_widget",
				cols: []string{"id", "name"},
				rows: [][]*any{
					{anyPtr("1"), anyPtr(int64(7))},
				},
			},
		},
	}
	db := setupDB(t)
	plan := dataimport.Plan[*orm.DB]{
		Tables: []dataimport.SourceTable{{Name: "legacy_widget", IDColumn: "id"}},
		Build: func(tx *orm.DB) (*orm.DB, error) { return tx, nil },
		Steps: []dataimport.Step[*orm.DB]{
			{
				Name: "widgets",
				Owns: []string{"legacy_widget"},
				Run: func(s dataimport.Session[*orm.DB]) error {
					return nil
				},
			},
		},
	}

	_, err := dataimport.Run(db, snap, nil, plan, []string{"widgets"}, dataimport.Apply)
	if err == nil || err.Error() != "step widgets: dataimport: table legacy_widget column name: value is not text" {
		t.Errorf("expected 'value is not text' error, got %v", err)
	}
}

func TestApplyCommits(t *testing.T) {
	snap := &memSnapshot{
		tables: []memTable{
			{
				name: "legacy_widget",
				cols: []string{"id", "name"},
				rows: [][]*any{
					{anyPtr("1"), anyPtr("a")},
					{anyPtr("2"), anyPtr("b")},
				},
			},
		},
	}

	dbPath := filepath.Join(t.TempDir(), "t.db")
	conn, _ := sqlite.Open(dbPath)
	compiler, _ := sqlite.DDLCompiler(conn)
	db_ddl := ddl.New(conn, compiler)
	db_ddl.Sync(&conformance.Widget{})
	db := orm.New(conn)

	plan := dataimport.Plan[*orm.DB]{
		Tables: []dataimport.SourceTable{{Name: "legacy_widget", IDColumn: "id"}},
		Build: func(tx *orm.DB) (*orm.DB, error) { return tx, nil },
		Steps: []dataimport.Step[*orm.DB]{
			{
				Name: "widgets",
				Owns: []string{"legacy_widget"},
				Run: func(s dataimport.Session[*orm.DB]) error {
					table, _ := s.Table("legacy_widget")
					for _, r := range table.Rows {
						id := table.ID(r)
						name := table.Value(r, "name")
						err := s.Modules.Create(&conformance.Widget{Id: "w"+id, Name: name})
						if err != nil {
							return err
						}
						s.Report.Count("legacy_widget", dataimport.Imported, 1)
					}
					return nil
				},
			},
		},
	}

	_, err := dataimport.Run(db, snap, nil, plan, []string{"widgets"}, dataimport.Apply)
	if err != nil {
		t.Errorf("expected nil error for apply, got %v", err)
	}

	n := countWidgets(t, conn)
	if n != 2 {
		t.Errorf("expected 2 widgets, got %d", n)
	}
}

func TestApplyWithProblemRollsBack(t *testing.T) {
	snap := &memSnapshot{
		tables: []memTable{
			{
				name: "legacy_widget",
				cols: []string{"id", "name"},
				rows: [][]*any{
					{anyPtr("1"), anyPtr("a")},
				},
			},
		},
	}

	dbPath := filepath.Join(t.TempDir(), "t.db")
	conn, _ := sqlite.Open(dbPath)
	compiler, _ := sqlite.DDLCompiler(conn)
	db_ddl := ddl.New(conn, compiler)
	db_ddl.Sync(&conformance.Widget{})
	db := orm.New(conn)

	plan := dataimport.Plan[*orm.DB]{
		Tables: []dataimport.SourceTable{{Name: "legacy_widget", IDColumn: "id"}},
		Build: func(tx *orm.DB) (*orm.DB, error) { return tx, nil },
		Steps: []dataimport.Step[*orm.DB]{
			{
				Name: "widgets",
				Owns: []string{"legacy_widget"},
				Run: func(s dataimport.Session[*orm.DB]) error {
					table, _ := s.Table("legacy_widget")
					for _, r := range table.Rows {
						id := table.ID(r)
						name := table.Value(r, "name")
						err := s.Modules.Create(&conformance.Widget{Id: "w"+id, Name: name})
						if err != nil {
							return err
						}
						s.Report.Count("legacy_widget", dataimport.Imported, 1)
					}
					s.Report.Problem(dataimport.Issue{Table: "legacy_widget", Rule: "dummy_problem"})
					return nil
				},
			},
		},
	}

	_, err := dataimport.Run(db, snap, nil, plan, []string{"widgets"}, dataimport.Apply)
	if !errors.Is(err, dataimport.ErrRolledBack) {
		t.Errorf("expected ErrRolledBack, got %v", err)
	}

	n := countWidgets(t, conn)
	if n != 0 {
		t.Errorf("expected 0 widgets, got %d", n)
	}
}

func TestApplyUnbalancedRollsBack(t *testing.T) {
	snap := &memSnapshot{
		tables: []memTable{
			{
				name: "legacy_widget",
				cols: []string{"id", "name"},
				rows: [][]*any{
					{anyPtr("1"), anyPtr("a")},
					{anyPtr("2"), anyPtr("b")},
				},
			},
		},
	}

	dbPath := filepath.Join(t.TempDir(), "t.db")
	conn, _ := sqlite.Open(dbPath)
	compiler, _ := sqlite.DDLCompiler(conn)
	db_ddl := ddl.New(conn, compiler)
	db_ddl.Sync(&conformance.Widget{})
	db := orm.New(conn)

	plan := dataimport.Plan[*orm.DB]{
		Tables: []dataimport.SourceTable{{Name: "legacy_widget", IDColumn: "id"}},
		Build: func(tx *orm.DB) (*orm.DB, error) { return tx, nil },
		Steps: []dataimport.Step[*orm.DB]{
			{
				Name: "widgets",
				Owns: []string{"legacy_widget"},
				Run: func(s dataimport.Session[*orm.DB]) error {
					table, _ := s.Table("legacy_widget")
					r := table.Rows[0]
					id := table.ID(r)
					name := table.Value(r, "name")
					err := s.Modules.Create(&conformance.Widget{Id: "w"+id, Name: name})
					if err != nil {
						return err
					}
					s.Report.Count("legacy_widget", dataimport.Imported, 1)
					return nil
				},
			},
		},
	}

	_, err := dataimport.Run(db, snap, nil, plan, []string{"widgets"}, dataimport.Apply)
	if !errors.Is(err, dataimport.ErrRolledBack) {
		t.Errorf("expected ErrRolledBack, got %v", err)
	}

	n := countWidgets(t, conn)
	if n != 0 {
		t.Errorf("expected 0 widgets, got %d", n)
	}
}

func TestStepErrorRollsBack(t *testing.T) {
	snap := &memSnapshot{
		tables: []memTable{
			{
				name: "legacy_widget",
				cols: []string{"id", "name"},
				rows: [][]*any{
					{anyPtr("1"), anyPtr("a")},
				},
			},
		},
	}

	dbPath := filepath.Join(t.TempDir(), "t.db")
	conn, _ := sqlite.Open(dbPath)
	compiler, _ := sqlite.DDLCompiler(conn)
	db_ddl := ddl.New(conn, compiler)
	db_ddl.Sync(&conformance.Widget{})
	db := orm.New(conn)

	plan := dataimport.Plan[*orm.DB]{
		Tables: []dataimport.SourceTable{{Name: "legacy_widget", IDColumn: "id"}},
		Build: func(tx *orm.DB) (*orm.DB, error) { return tx, nil },
		Steps: []dataimport.Step[*orm.DB]{
			{
				Name: "widgets",
				Owns: []string{"legacy_widget"},
				Run: func(s dataimport.Session[*orm.DB]) error {
					table, _ := s.Table("legacy_widget")
					r := table.Rows[0]
					id := table.ID(r)
					name := table.Value(r, "name")
					err := s.Modules.Create(&conformance.Widget{Id: "w"+id, Name: name})
					if err != nil {
						return err
					}
					return errors.New("boom")
				},
			},
		},
	}

	_, err := dataimport.Run(db, snap, nil, plan, []string{"widgets"}, dataimport.Apply)
	if err == nil || err.Error() != "step widgets: boom" {
		t.Errorf("expected step error, got %v", err)
	}

	n := countWidgets(t, conn)
	if n != 0 {
		t.Errorf("expected 0 widgets, got %d", n)
	}
}

func TestSelectedPullsDependencies(t *testing.T) {
	var execOrder []string
	plan := dataimport.Plan[any]{
		Steps: []dataimport.Step[any]{
			{Name: "a", Run: func(s dataimport.Session[any]) error { execOrder = append(execOrder, "a"); return nil }},
			{Name: "b", After: []string{"a"}, Run: func(s dataimport.Session[any]) error { execOrder = append(execOrder, "b"); return nil }},
			{Name: "c", Run: func(s dataimport.Session[any]) error { execOrder = append(execOrder, "c"); return nil }},
		},
		Build: func(tx *orm.DB) (any, error) { return nil, nil },
	}

	dbPath := filepath.Join(t.TempDir(), "t.db")
	conn, _ := sqlite.Open(dbPath)
	db := orm.New(conn)

	_, _ = dataimport.Run(db, &memSnapshot{}, nil, plan, []string{"b"}, dataimport.Rehearse)

	if len(execOrder) != 2 || execOrder[0] != "a" || execOrder[1] != "b" {
		t.Errorf("expected exec order [a b], got %v", execOrder)
	}
}

func TestUnknownStep(t *testing.T) {
	plan := dataimport.Plan[any]{}
	dbPath := filepath.Join(t.TempDir(), "t.db")
	conn, _ := sqlite.Open(dbPath)
	db := orm.New(conn)

	_, err := dataimport.Run(db, &memSnapshot{}, nil, plan, []string{"nope"}, dataimport.Rehearse)
	if !errors.Is(err, dataimport.ErrUnknownStep) {
		t.Errorf("expected ErrUnknownStep, got %v", err)
	}
}

func TestCycle(t *testing.T) {
	plan := dataimport.Plan[any]{
		Steps: []dataimport.Step[any]{
			{Name: "a", After: []string{"b"}},
			{Name: "b", After: []string{"a"}},
		},
	}
	dbPath := filepath.Join(t.TempDir(), "t.db")
	conn, _ := sqlite.Open(dbPath)
	db := orm.New(conn)

	_, err := dataimport.Run(db, &memSnapshot{}, nil, plan, []string{"a"}, dataimport.Rehearse)
	if !errors.Is(err, dataimport.ErrStepCycle) {
		t.Errorf("expected ErrStepCycle, got %v", err)
	}
}

func TestOwnedTwice(t *testing.T) {
	plan := dataimport.Plan[any]{
		Steps: []dataimport.Step[any]{
			{Name: "a", Owns: []string{"t1"}},
			{Name: "b", Owns: []string{"t1"}},
		},
	}
	dbPath := filepath.Join(t.TempDir(), "t.db")
	conn, _ := sqlite.Open(dbPath)
	db := orm.New(conn)

	_, err := dataimport.Run(db, &memSnapshot{}, nil, plan, []string{"a"}, dataimport.Rehearse)
	if !errors.Is(err, dataimport.ErrOwnedTwice) {
		t.Errorf("expected ErrOwnedTwice, got %v", err)
	}
}

func TestNoneSelected(t *testing.T) {
	plan := dataimport.Plan[any]{
		Steps: []dataimport.Step[any]{
			{Name: "a"},
		},
	}
	dbPath := filepath.Join(t.TempDir(), "t.db")
	conn, _ := sqlite.Open(dbPath)
	db := orm.New(conn)

	_, err := dataimport.Run(db, &memSnapshot{}, nil, plan, []string{}, dataimport.Rehearse)
	if !errors.Is(err, dataimport.ErrNoneSelected) {
		t.Errorf("expected ErrNoneSelected, got %v", err)
	}
}

func TestCorrectionApplied(t *testing.T) {
	snap := &memSnapshot{
		tables: []memTable{
			{
				name: "legacy_widget",
				cols: []string{"id", "name"},
				rows: [][]*any{
					{anyPtr("1"), anyPtr("Ana")},
				},
			},
		},
	}

	csvData := "tabla,id,campo,valor_original,valor_corregido,motivo\nlegacy_widget,1,name,Ana,Ana María,typo"
	fixes, err := dataimport.ReadCorrections(strings.NewReader(csvData))
	if err != nil {
		t.Fatal(err)
	}

	dbPath := filepath.Join(t.TempDir(), "t.db")
	conn, _ := sqlite.Open(dbPath)
	db := orm.New(conn)

	plan := dataimport.Plan[*orm.DB]{
		Tables: []dataimport.SourceTable{{Name: "legacy_widget", IDColumn: "id"}},
		Build: func(tx *orm.DB) (*orm.DB, error) { return tx, nil },
		Steps: []dataimport.Step[*orm.DB]{
			{
				Name: "widgets",
				Owns: []string{"legacy_widget"},
				Run: func(s dataimport.Session[*orm.DB]) error {
					table, _ := s.Table("legacy_widget")
					row := table.Rows[0]
					if table.Value(row, "name") != "Ana María" {
						t.Errorf("expected corrected value 'Ana María', got %q", table.Value(row, "name"))
					}
					return nil
				},
			},
		},
	}

	rep, _ := dataimport.Run(db, snap, fixes, plan, []string{"widgets"}, dataimport.Rehearse)
	notes := rep.Notes()
	if len(notes) == 0 || notes[0].Rule != "correction_applied" {
		t.Errorf("expected correction_applied note, got %v", notes)
	}
}

func TestCorrectionStale(t *testing.T) {
	snap := &memSnapshot{
		tables: []memTable{
			{
				name: "legacy_widget",
				cols: []string{"id", "name"},
				rows: [][]*any{
					{anyPtr("1"), anyPtr("Ana")},
				},
			},
		},
	}

	csvData := "tabla,id,campo,valor_original,valor_corregido,motivo\nlegacy_widget,1,name,Bea,Ana María,typo"
	fixes, _ := dataimport.ReadCorrections(strings.NewReader(csvData))

	dbPath := filepath.Join(t.TempDir(), "t.db")
	conn, _ := sqlite.Open(dbPath)
	db := orm.New(conn)

	plan := dataimport.Plan[*orm.DB]{
		Tables: []dataimport.SourceTable{{Name: "legacy_widget", IDColumn: "id"}},
		Build: func(tx *orm.DB) (*orm.DB, error) { return tx, nil },
		Steps: []dataimport.Step[*orm.DB]{
			{Name: "widgets", Owns: []string{"legacy_widget"}, Run: func(s dataimport.Session[*orm.DB]) error { s.Table("legacy_widget"); return nil }},
		},
	}

	rep, _ := dataimport.Run(db, snap, fixes, plan, []string{"widgets"}, dataimport.Rehearse)
	probs := rep.Problems()
	if len(probs) == 0 || probs[0].Rule != "correction_stale" {
		t.Errorf("expected correction_stale problem, got %v", probs)
	}
}

func TestCorrectionTargetMissing(t *testing.T) {
	snap := &memSnapshot{
		tables: []memTable{
			{
				name: "legacy_widget",
				cols: []string{"id", "name"},
				rows: [][]*any{
					{anyPtr("1"), anyPtr("Ana")},
				},
			},
		},
	}

	csvData := "tabla,id,campo,valor_original,valor_corregido,motivo\nlegacy_widget,99,name,Ana,Ana María,typo"
	fixes, _ := dataimport.ReadCorrections(strings.NewReader(csvData))

	dbPath := filepath.Join(t.TempDir(), "t.db")
	conn, _ := sqlite.Open(dbPath)
	db := orm.New(conn)

	plan := dataimport.Plan[*orm.DB]{
		Tables: []dataimport.SourceTable{{Name: "legacy_widget", IDColumn: "id"}},
		Build: func(tx *orm.DB) (*orm.DB, error) { return tx, nil },
		Steps: []dataimport.Step[*orm.DB]{
			{Name: "widgets", Owns: []string{"legacy_widget"}, Run: func(s dataimport.Session[*orm.DB]) error { s.Table("legacy_widget"); return nil }},
		},
	}

	rep, _ := dataimport.Run(db, snap, fixes, plan, []string{"widgets"}, dataimport.Rehearse)
	probs := rep.Problems()
	if len(probs) == 0 || probs[0].Rule != "correction_target_missing" {
		t.Errorf("expected correction_target_missing problem, got %v", probs)
	}
}

func TestCorrectionsBadHeader(t *testing.T) {
	csvData := "a,b\n1,2"
	_, err := dataimport.ReadCorrections(strings.NewReader(csvData))
	if !errors.Is(err, dataimport.ErrCorrectionsHeader) {
		t.Errorf("expected ErrCorrectionsHeader, got %v", err)
	}
}

func TestTableNotDeclared(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "t.db")
	conn, _ := sqlite.Open(dbPath)
	db := orm.New(conn)

	plan := dataimport.Plan[*orm.DB]{
		Build: func(tx *orm.DB) (*orm.DB, error) { return tx, nil },
		Steps: []dataimport.Step[*orm.DB]{
			{
				Name: "widgets",
				Run: func(s dataimport.Session[*orm.DB]) error {
					_, err := s.Table("other")
					if !errors.Is(err, dataimport.ErrTableNotDeclared) {
						t.Errorf("expected ErrTableNotDeclared, got %v", err)
					}
					return nil
				},
			},
		},
	}

	_, _ = dataimport.Run(db, &memSnapshot{}, nil, plan, []string{"widgets"}, dataimport.Rehearse)
}

func TestWriteText(t *testing.T) {
	var rep dataimport.Report
	rep.Count("legacy_widget", dataimport.Imported, 2)
	rep.Note("legacy_widget", "1", "fix_name", "ok")
	rep.Problem(dataimport.Issue{Table: "legacy_widget", Rule: "bad_val", Message: "oops"})

	var buf strings.Builder
	rep.WriteText(&buf)
	out := buf.String()

	if !strings.Contains(out, "legacy_widget") || !strings.Contains(out, "bad_val") || !strings.Contains(out, "oops") {
		t.Errorf("unexpected WriteText output: %s", out)
	}
}
