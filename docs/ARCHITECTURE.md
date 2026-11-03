# Architecture

## Purpose
Engine-neutral import of a legacy system's data into an app without losing a row. It supports rehearsals with rollback and reconciles every row.

## Components
- **Table / Row:** Represents legacy data loaded into memory, storing text values.
- **Report:** A ledger tracking imports, merges, discards, derivations, notes, and problems, ensuring everything is accounted for.
- **Corrections:** Manages CSV-based data corrections to fix issues before the mapping logic executes.
- **Plan / Step:** Defines execution steps, topological ordering, dependencies, and execution contexts.

## Flows
The primary flow involves reading a snapshot, validating steps, starting a single database transaction, setting up corrections, loading table data, executing the step logic, and committing (if applying and error-free) or rolling back (if rehearsing or encountering errors).

## Decisions
- **`ddl.Snapshot` as Source:** Legacy data is read strictly via this snapshot contract, bypassing any direct driver or SQL dialect interaction.
- **Values as Text:** Legacy records are parsed uniformly into text strings (or null) to simplify reading and reporting independent of legacy data types.
- **One Transaction:** The entire run happens within a single transaction to ensure atomicity. If applying, it either fully succeeds or entirely fails.
- **Dependencies:** Steps define their dependencies. The runner pulls the necessary dependencies, topologically sorting them for execution.
- **Rehearsal is the Same Code:** Rehearsing runs the exact same code as applying, meaning real constraints and side-effects are properly evaluated before the transaction is forcefully rolled back.
- **Messages:** every message is built with `webtyp.com/fmt.Err` and translated through `lang.json`; numbers and names are data.
