# dataimport

## Why This Exists

An app that replaces a legacy system must bring its data, repeatedly (rehearsals while developing, then the real cut-over). Without this library each app writes a one-off script that skips module validation and discovers problems with half the data written. This library is the domain-free part:

1. Read the legacy tables through `ddl.Snapshot`
2. Run steps the app declares, in dependency order, inside one transaction.
3. Rehearse (always roll back) or Apply (commit only with zero problems and a balanced report).
4. Reconcile: per legacy table owned by a step, rows = imported + merged + discarded.
5. Correct: apply an exact corrections CSV before the steps read a table.

## Before You Start

You need an engine implementing `ddl.BackupFormat`, e.g., `postgres.Backups`. And you need the app's modules to build the actual data mapping.

## Step-by-Step Guide

```go
package main

import (
	"os"
	"webtyp.com/dataimport"
	"webtyp.com/ddl"
	"webtyp.com/orm"
)

func runImport(db *orm.DB, snap ddl.Snapshot) {
	plan := dataimport.Plan[*orm.DB]{
		Tables: []dataimport.SourceTable{{"legacy_staff", "id"}},
		Build:  func(tx *orm.DB) (*orm.DB, error) { return tx, nil },
		Steps: []dataimport.Step[*orm.DB]{
			{
				Name: "staff",
				Owns: []string{"legacy_staff"},
				Run: func(s dataimport.Session[*orm.DB]) error {
					table, _ := s.Table("legacy_staff")
					for _, r := range table.Rows {
						s.Report.Count("legacy_staff", dataimport.Imported, 1)
					}
					return nil
				},
			},
		},
	}

	rep, _ := dataimport.Run(db, snap, nil, plan, []string{"staff"}, dataimport.Rehearse)
	rep.WriteText(os.Stdout)

	// Then apply when ready
	// dataimport.Run(db, snap, nil, plan, []string{"staff"}, dataimport.Apply)
}
```

## Documentation Map

- [Architecture](docs/ARCHITECTURE.md)
- [Run Flow](docs/diagrams/RUN_FLOW.md)
