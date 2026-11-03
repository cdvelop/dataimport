---
PLAN: "feat: dataimport — import steps over a ddl.Snapshot, rehearse with rollback, reconcile every row"
TAG: v0.1.0
EXECUTOR: jules
REVIEWER: none
STATUS: running
SESSION: 16441366779111196737
---

> Read AGENTS.md at the repo root before any change.
> **Finish by yourself:** when the delivery check passes, commit, push and open the pull request. Never ask whether to
> proceed or finalize — nobody answers, and a session that waits ends without your work.

# Plan — `webtyp.com/dataimport`: bring a legacy system's data into an app without losing a row

## Tools

```bash
go install webtyp.com/devflow/cmd/gotest@latest
go get webtyp.com/ddl@v0.0.39 webtyp.com/orm@v0.12.24 webtyp.com/storage@v0.1.18 webtyp.com/model@v0.2.13 webtyp.com/sqlite@v0.3.28
```

Environment: `GOPRIVATE=webtyp.com/*,github.com/veltylabs/*`, `GOFLAGS=-mod=mod`, `go mod tidy` after `go get`.

## Why

An app that replaces a legacy system must bring its data, repeatedly (rehearsals while developing, then the real
cut-over). Without this library each app writes a one-off script that skips module validation and discovers
problems with half the data written. This library is the domain-free part:

1. **Read** the legacy tables through `ddl.Snapshot` (any engine: `postgres.Backups`, later sqlite…).
2. **Run steps** the app declares, in dependency order, the ones the user **selects** plus their dependencies,
   inside **one** `orm.DB` transaction.
3. **Rehearse** (always roll back) or **Apply** (commit only with zero problems and a balanced report).
4. **Reconcile**: per legacy table owned by a step, `rows = imported + merged + discarded`.
5. **Correct**: apply an exact corrections CSV before the steps read a table.

The same engine serves a CLI (`migrate/pastdb` in the first consumer, `veltylabs/mjosefa-cms`) and the import
screen of `webtyp.com/erd` (a later plan). Domain mapping lives in the app.

## Ecosystem search

- `ddl.Snapshot` (ddl v0.0.39) → **reuse** as the only source; no dump parser here.
- `orm.DB.Tx` → **reuse** for all-or-nothing.
- No existing import/reconcile/corrections code in `webtyp/*` or `veltylabs/*` (grep `reconcil`, `dataimport`,
  `corrections`) → **new**, created with `gonew` (v0.0.1 exists, with a placeholder `dataimport.go`).

## Design gate

1. **Prior art.** Django `loaddata` (one transaction, all-or-nothing), Terraform `plan`/`apply` (rehearse then
   apply the same code), Rails data-migration gems with `--dry-run` — unverified links. Shared standard: a
   dry run that is the same code as the real run; followed, and stronger here: the rehearsal runs against the
   real target and rolls back, so it exercises real constraints.
2. **Names.** `dataimport.Run(db, snap, fixes, plan, selected, mode)`, `Rehearse`/`Apply`, `Report.Balanced()`,
   `Step.Owns`. A junior reads them as sentences.
3. **Ledger.** Concepts: `Plan`, `Step`, `Session`, `Table`, `Report`, `Corrections`, `Mode` (+7). Ways to import
   legacy data: 0 → 1.
4. **Where.** Own repo: not an ORM concern (`orm`), not a driver (`postgres`), not UI (`erd`). Depends only on
   contracts (`ddl`, `orm`, `storage`); `sqlite` is a test dependency.
5. **Deletes.** `dataimport.go` placeholder (`Dataimport`, `New`). Otherwise new capability.

## APIs used

| Symbol | Signature | Tag | File |
|---|---|---|---|
| `ddl.Snapshot` | `interface { Tables() ([]string, error); Columns(table string) ([]ddl.ColumnInfo, error); ReadTable(table string) (storage.Rows, error); Close() error }` — `ReadTable` returns every value as text, NULL as NULL | ddl v0.0.39 | `backup.go` |
| `ddl.ColumnInfo` | `struct { Name, Type string; NotNull, PK bool }` | ddl v0.0.39 | `schema.go` |
| `storage.Rows` | `interface { Next() bool; Scan(dest ...any) error; Columns() ([]string, error); Close() error; Err() error }` | storage v0.1.18 | `executor.go` |
| `orm.DB.Tx` | `func (d *DB) Tx(fn func(tx *DB) error) error` — rolls back when fn returns an error | orm v0.12.24 | `tx.go` |
| `orm.New` | `func New(conn storage.Conn) *DB` | orm v0.12.24 | `db.go` |
| `orm.DB.Create` | `func (d *DB) Create(m model.Model) error` | orm v0.12.24 | `db.go` |
| `sqlite.Open` (tests) | `func Open(dsn string) (storage.Conn, error)` | sqlite v0.3.28 | `adapter.go` |
| `conformance.Widget` (tests) | `type Widget struct { Id, Name string; Qty int64; Active bool; Note string }`, table `conformance.WidgetModel.Name` | storage v0.1.18 | `conformance/model.go` |
| `ddl.New`, `(*ddl.DB).Sync` (tests) | `func New(conn Execer, c Compiler) *DB`; `Sync(models ...model.Model) error` | ddl v0.0.39 | `db.go`, `sync.go` |

If a signature in the published tag differs from this table, stop and name the symbol in the PR; never adapt it by guessing.

## Stage 1 — delete the placeholder; `table.go`

Delete `dataimport.go` (`grep -rn "type Dataimport" .` → empty).

```go
package dataimport

import (
	"database/sql"
	"errors"
)

// Table is one legacy table loaded from the snapshot, every value as text.
type Table struct {
	Name     string
	IDColumn string   // declared in Plan.Tables
	Columns  []string // snapshot order
	Rows     []Row
}

// Row is one legacy row; read it through its Table.
type Row struct{ values []sql.NullString }

// Value returns the column's text, "" for NULL. An unknown column is a mapping bug: it panics with
// "dataimport: table <t> has no column <c>".
//
// Example: name := staff.Value(row, "staff_name")
func (t *Table) Value(r Row, col string) string { return r.values[t.index(col)].String }

// IsNull reports whether the column is NULL in r.
func (t *Table) IsNull(r Row, col string) bool { return !r.values[t.index(col)].Valid }

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

func (t *Table) set(r Row, col, value string) { r.values[t.index(col)] = sql.NullString{String: value, Valid: true} }

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
	t := &Table{Name: name, IDColumn: idColumn, Columns: cols}
	for rows.Next() {
		vals := make([]sql.NullString, len(cols))
		dest := make([]any, len(cols))
		for i := range vals {
			dest[i] = &vals[i]
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}
		t.Rows = append(t.Rows, Row{values: vals})
	}
	return t, rows.Err()
}
```

`loadTable` also checks `idColumn` is among `cols`; if not → `errors.New("dataimport: table " + name + " has no id column " + idColumn)`.

## Stage 2 — `report.go`

```go
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

func (r *Report) Count(table string, o Outcome, n int) // adds n to the outcome of table (creates the line if new)
func (r *Report) Derived(target string, n int)
func (r *Report) Note(table, sourceID, rule, message string)
func (r *Report) Problem(i Issue)
func (r *Report) Counts() []TableCount   // in first-touched order
func (r *Report) DerivedCounts() []DerivedCount
func (r *Report) Notes() []Note
func (r *Report) Problems() []Issue
// Balanced: for every TableCount, Source == Imported+Merged+Discarded. The error names the first table and
// the difference, verbatim: "dataimport: table staff: 56 source rows, 55 accounted (imported 50, merged 0, discarded 5)".
func (r *Report) Balanced() error
// WriteText: counts table, derived lines, notes grouped by Rule (count + first 5 source ids), then problems.
func (r *Report) WriteText(w io.Writer) error
```

`Source` of each owned table is set by `Run` (Stage 4) from `len(Table.Rows)`; steps never set it.

## Stage 3 — `corrections.go`

CSV, exact header `tabla,id,campo,valor_original,valor_corregido,motivo` (Spanish header is the format users fill).

```go
type Correction struct{ Table, ID, Field, Original, Corrected, Reason string }
type Corrections struct{ list []Correction; applied []bool }

var ErrCorrectionsHeader = errors.New("dataimport: corrections header must be tabla,id,campo,valor_original,valor_corregido,motivo")

func ReadCorrections(r io.Reader) (*Corrections, error) // encoding/csv; empty reader → empty Corrections
func (c *Corrections) apply(t *Table, rep *Report)       // on load, for corrections of t.Name
```

`apply` per correction of table `t.Name`: find row with `t.ID(row) == ID`; missing → `Problem{Rule: "correction_target_missing"}`;
column missing → same rule; current value ≠ `Original` → `Problem{Rule: "correction_stale", Message: "current value <v>"}`;
else `t.set(...)`, mark applied, `Note{Rule: "correction_applied", Message: Reason}`. A nil `*Corrections` applies nothing.

## Stage 4 — `run.go`

```go
type Mode int

const (
	Rehearse Mode = iota // everything inside a transaction that always rolls back
	Apply                // commits only with zero problems and a balanced report
)

// SourceTable declares a legacy table and its id column.
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

// Table returns the legacy table (loaded once, corrections applied). Undeclared → ErrTableNotDeclared.
func (s Session[T]) Table(name string) (*Table, error)

type Plan[T any] struct {
	Tables []SourceTable
	Build  func(tx *orm.DB) (T, error)
	Steps  []Step[T]
}

// StepNames lists the steps in execution order (what a UI offers).
func (p Plan[T]) StepNames() ([]string, error)

var (
	ErrRolledBack   = errors.New("dataimport: rolled back")
	ErrUnknownStep  = errors.New("dataimport: unknown step")
	ErrStepCycle    = errors.New("dataimport: steps form a cycle")
	ErrOwnedTwice   = errors.New("dataimport: table owned by two steps")
	ErrNoneSelected = errors.New("dataimport: no step selected")
)

// Run executes the selected steps plus every step they need (After, transitively), in topological order
// (ties: declaration order), on one transaction.
//
// Example:
//
//	rep, err := dataimport.Run(db, snap, fixes, plan, []string{"staff"}, dataimport.Rehearse)
func Run[T any](db *orm.DB, snap ddl.Snapshot, fixes *Corrections, p Plan[T], selected []string, mode Mode) (*Report, error)
```

Body of `Run` (write it like this):

```go
	order, err := p.order(selected) // validates: ErrNoneSelected, ErrUnknownStep (wrapped with the name), ErrStepCycle, ErrOwnedTwice
	if err != nil {
		return nil, err
	}
	rep := &Report{}
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
					return fmt.Errorf("step %s: %w", st.Name, err)
				}
				rep.setSource(owned, len(t.Rows))
			}
			if err := st.Run(Session[T]{Report: rep, Modules: mods, source: src}); err != nil {
				return fmt.Errorf("step %s: %w", st.Name, err)
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
```

`source.table(name)`: cached; not in `p.Tables` → `ErrTableNotDeclared`; else `loadTable` then `fixes.apply(t, rep)`.
`rep.setSource` is unexported. A rehearsal returns `nil` error with a report that may contain problems: the
caller decides by reading `Problems()` and `Balanced()`.

## Stage 5 — tests (`tests/`, package `dataimport_test`)

Fixture: `memSnapshot` in `tests/snapshot_test.go` implementing `ddl.Snapshot` over
`[]struct{name string; cols []string; rows [][]*string}` (nil pointer = NULL), with `memRows` implementing
`storage.Rows` whose `Scan` fills `*sql.NullString`. Target DB: `sqlite.Open(filepath.Join(t.TempDir(), "t.db"))`,
`ddl.New(conn, compiler).Sync(&conformance.Widget{})` where `compiler, _ := sqlite.DDLCompiler(conn)`; count rows with
`conn.QueryRow("SELECT COUNT(*) FROM " + conformance.WidgetModel.Name).Scan(&n)`.

Test plan: `Plan[*orm.DB]{Tables: {{"legacy_widget","id"}}, Build: func(tx *orm.DB) (*orm.DB, error) { return tx, nil }}`,
step `widgets` owns `legacy_widget` and creates one `conformance.Widget{Id: "w"+id, Name: name}` per row, `Count(Imported)`.

| Test | Setup → call | Expected |
|---|---|---|
| `TestValueAndNull` | table with `name` NULL | `Value == ""`, `IsNull == true` |
| `TestValueUnknownColumnPanics` | `Value(row, "nope")` | panic message `dataimport: table legacy_widget has no column nope` |
| `TestRehearseLeavesNoRows` | 2 legacy rows, `Rehearse` | err nil; `Counts()[0]` = `{legacy_widget, 2, 2, 0, 0}`; widget count 0 |
| `TestApplyCommits` | same, `Apply` | err nil; widget count 2 |
| `TestApplyWithProblemRollsBack` | step adds one `Problem` | `errors.Is(err, ErrRolledBack)`; widget count 0 |
| `TestApplyUnbalancedRollsBack` | step counts only 1 of 2 | `ErrRolledBack`; `Balanced()` error text exactly `dataimport: table legacy_widget: 2 source rows, 1 accounted (imported 1, merged 0, discarded 0)` |
| `TestStepErrorRollsBack` | step returns `errors.New("boom")` after creating one widget, `Apply` | error text `step widgets: boom`; count 0 |
| `TestSelectedPullsDependencies` | steps `a`, `b After a`, `c`; select `b` | executed order `[a b]` (record names in a slice) |
| `TestUnknownStep`, `TestCycle`, `TestOwnedTwice`, `TestNoneSelected` | invalid plans | the matching `Err*` via `errors.Is` |
| `TestCorrectionApplied` | CSV row fixing `name` of id `1` from `Ana` to `Ana María` | step sees `Ana María`; `Notes()` has rule `correction_applied` |
| `TestCorrectionStale` | Original `Bea` but value is `Ana` | `Problems()[0].Rule == "correction_stale"` |
| `TestCorrectionTargetMissing` | id `99` | `Problems()[0].Rule == "correction_target_missing"` |
| `TestCorrectionsBadHeader` | header `a,b` | `ErrCorrectionsHeader` |
| `TestTableNotDeclared` | step reads `other` | error wraps `ErrTableNotDeclared` |
| `TestWriteText` | report with counts, a note, a problem | output contains `legacy_widget`, the rule name and the problem message |

## Stage 6 — documentation

- `README.md` ≤ 150 lines, sections in order: `Why This Exists` (without: one-off scripts, half-written data;
  with: rehearse/apply, reconciliation), `Before You Start` (an engine implementing `ddl.BackupFormat`, e.g.
  `postgres.Backups`; the app's modules), `Step-by-Step Guide` (complete compilable example: open snapshot with
  `postgres.Backups(dsn).OpenBackup(path)`, a `Plan` with one step, `Run` with `Rehearse`, print with
  `WriteText(os.Stdout)`, then `Apply`), `Documentation Map`.
- `docs/ARCHITECTURE.md` (4 sections: Purpose, Components, Flows, Decisions). Decisions: read only through
  `ddl.Snapshot`; values as text; one transaction; selected steps pull their dependencies; a rehearsal is the
  same code as Apply.
- `docs/diagrams/RUN_FLOW.md`: context header, `flowchart TD` of `Run` (validate plan → Tx → Build → per step:
  load owned tables + corrections → Run → decide commit/rollback), numbered walkthrough.

## Delivery check

1. `gotest` passes (a PR that does not compile, or whose tests could not run, is a failed delivery).
2. `grep -rn "type Dataimport" .` → empty.
3. `git diff --stat main` lists every file above.

Commit after every stage. Never run `git reset`, `git restore`, `git checkout -- <file>` or `git stash` on your own
work. Stuck 30 minutes: open the PR anyway listing what fails.

## Stages

| Stage | Files | Done when |
|---|---|---|
| 1 | `table.go`, delete `dataimport.go` | compiles |
| 2 | `report.go` | compiles |
| 3 | `corrections.go` | compiles |
| 4 | `run.go` | compiles |
| 5 | `tests/*` | every test in the table passes |
| 6 | `README.md`, `docs/ARCHITECTURE.md`, `docs/diagrams/RUN_FLOW.md` | written |
