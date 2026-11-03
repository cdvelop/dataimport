---
PLAN: "feat: dataimport messages for beginners, translatable with langc"
EXECUTOR: jules
REVIEWER: none
---

> Read AGENTS.md at the repo root before any change.
> **Finish by yourself:** when the delivery check passes, commit, push and open the pull request. Never ask whether to
> proceed or finalize — nobody answers, and a session that waits ends without your work.

# Plan — every message of `dataimport` readable by a beginner, in their own language

## Tools

```bash
go install webtyp.com/devflow/cmd/gotest@latest
go install webtyp.com/lang/cmd/langc@latest
go get webtyp.com/fmt@v1.0.0
```

Environment: `GOPRIVATE=webtyp.com/*,github.com/veltylabs/*`, `GOFLAGS=-mod=mod`, `go mod tidy` after `go get`.

## Why

webtyp is built for people who are starting to program, and many of them do not read English. Every error, report
line and notice this library produces must say what happened, whether the data is affected, and what to do — and be
built with `webtyp.com/fmt.Err` so `langc` extracts its words and `lang.json` translates them. Today every message is
stdlib `errors.New`/`fmt.Errorf`/`fmt.Fprintf`: none can be translated.

## Ecosystem search

- Sentinel errors built with `fmt.Err` already exist in `webtyp/orm/errors.go` and `webtyp/media/errors.go` → **follow that pattern**.
- `postgres/backup.go` builds a translatable notice with numbers passed as data → **same technique** for counts.

## Rules for this plan (anti-footguns)

- Import webtyp fmt with an alias: `wfmt "webtyp.com/fmt"`. Keep stdlib `fmt` **only** for `fmt.Errorf("%w: …", sentinel, data)`
  wrapping (webtyp `fmt.Err` does not wrap) and `encoding/csv`/`io` plumbing.
- **Words are separate phrases; names, numbers and values are data** (variables), never literals inside a phrase:
  `wfmt.Err("table", name, "has no column", col)`, never `"table " + name`.
- Each phrase is a whole idea a translator can render; reuse the same phrase wherever the idea repeats.
- Do not change any exported name, type or behaviour; only the text. Every existing `errors.Is` test keeps passing.

## Stage 1 — sentinels (`run.go`, `table.go`, `corrections.go`)

Replace each `errors.New(...)` sentinel, keeping its name, with exactly:

| Var | New value |
|---|---|
| `ErrRolledBack` | `wfmt.Err("dataimport:", "nothing was saved: the import was rolled back")` |
| `ErrUnknownStep` | `wfmt.Err("dataimport:", "this step does not exist in the plan — check the name you selected")` |
| `ErrStepCycle` | `wfmt.Err("dataimport:", "two or more steps wait for each other (After), so none can start — remove one of the After links")` |
| `ErrOwnedTwice` | `wfmt.Err("dataimport:", "two steps own the same table — a table is reconciled by only one step")` |
| `ErrNoneSelected` | `wfmt.Err("dataimport:", "no step was selected — choose at least one")` |
| `ErrTableNotDeclared` | `wfmt.Err("dataimport:", "the step reads a table that is not listed in Plan.Tables — add it there with its id column")` |
| `ErrCorrectionsHeader` | `wfmt.Err("dataimport:", "the corrections file must start with this header:", correctionsHeader)` where `const correctionsHeader = "tabla,id,campo,valor_original,valor_corregido,motivo"` (data, not translated) |

The existing `fmt.Errorf("%w: %s", ErrUnknownStep, name)` and `ErrTableNotDeclared` wraps stay (stdlib, for `errors.Is`).
`"dataimport:"` is one shared key.

## Stage 2 — messages built at call time

| Where | Today | New |
|---|---|---|
| `table.go` `index` panic | `"dataimport: table " + t.Name + " has no column " + col` | `panic(wfmt.Err("dataimport:", "table", t.Name, "has no column", col, "— check the column name in your step").Error())` |
| `table.go` loadTable id column | `errors.New("dataimport: table " + name + " has no id column " + idColumn)` | `wfmt.Err("dataimport:", "table", name, "has no id column", idColumn, "— fix IDColumn in Plan.Tables")` |
| `table.go` non-text value | `… ": value is not text"` | `wfmt.Err("dataimport:", "table", name, "column", cols[i], "returned a value that is not text — the backup engine must return every value as text")` |
| `run.go` step wrap | `fmt.Errorf("step %s: %w", st.Name, err)` | `fmt.Errorf("%s %s: %w", wfmt.Err("step").Error(), st.Name, err)` |
| `corrections.go` stale | `Message: "current value " + v` | `Message: wfmt.Err("the value in the backup is now", v, "— update valor_original in the corrections file").Error()` |
| `corrections.go` target missing | (its current Message) | `wfmt.Err("this row or column is not in the backup — remove the correction or fix its id or campo").Error()` |
| `report.go` `Balanced` | `fmt.Errorf("dataimport: table %s: %d source rows, %d accounted (…)")` | `wfmt.Err("dataimport:", "table", c.Table, "has", c.Source, "rows in the backup but the steps accounted for", accounted, "(", "imported", c.Imported, "merged", c.Merged, "discarded", c.Discarded, ")", "— every row must be imported, merged or discarded, so nothing was saved")` |

## Stage 3 — `WriteText` labels (`report.go`)

Replace each `fmt.Fprintf` label with phrases from `wfmt.Err(...).Error()`, data as arguments:

- table line: `"table", name, ":", source, "rows in the backup", "—", "imported", n, "merged", n, "discarded", n`
- derived line: `"created", target, ":", n`
- note line: `"note", rule, "(", count, ")", ":"` then the ids (data) as today
- problem line: `"problem", rule, ":"` then `p.Message`
- `WriteText` keeps writing to `w` with `io.WriteString`; one line per item as today.

## Stage 4 — translations

`langc sync -tool .`, then fill **every** Spanish value in `lang.json` with natural Spanish written for a beginner
(e.g. `"nothing was saved: the import was rolled back"` → `"no se guardó nada: la importación se deshizo"`).
Keys that are not prose must not appear: if `langc` lists a name or code (a table name, `Plan.Tables`), pass it as a
variable instead of a literal. Other language columns `langc -tool` adds stay empty.

## Stage 5 — tests

Update the tests that compare text exactly (`TestValueUnknownColumnPanics`, `TestApplyUnbalancedRollsBack`,
`TestStepErrorRollsBack`, non-text value test) to the new English text (the tests run without a dictionary, so text is
English). Every `errors.Is` assertion stays unchanged and must pass.

## Stage 6 — docs

`README.md` and `docs/ARCHITECTURE.md` (Decisions): one line — every message is built with `webtyp.com/fmt.Err` and
translated through `lang.json`; numbers and names are data.

## Delivery check

1. `gotest` passes (a PR that does not compile, or whose tests could not run, is a failed delivery).
2. `langc sync -tool .` → 0 untranslated **es**, 0 reuse warnings, 0 identical values.
3. `grep -n 'errors.New(' *.go` → empty; `grep -n 'Fprintf' report.go` → empty.

Commit after every stage. Never run `git reset`, `git restore`, `git checkout -- <file>` or `git stash` on your own
work. Stuck 30 minutes: open the PR anyway listing what fails.

## Stages

| Stage | Files | Done when |
|---|---|---|
| 1 | `run.go`, `table.go`, `corrections.go` | compiles |
| 2 | `table.go`, `run.go`, `corrections.go`, `report.go` | compiles |
| 3 | `report.go` | compiles |
| 4 | `lang.json` | langc clean for es |
| 5 | `tests/*` | gotest passes |
| 6 | `README.md`, `docs/ARCHITECTURE.md` | written |
