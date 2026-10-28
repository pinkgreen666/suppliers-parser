# Repository Guidelines

## Project Structure & Module Organization

This Go application collects supplier information and price lists into SQLite and exports Excel workbooks.

- `cmd/parse/`: parser CLI and source selection.
- `cmd/export/`: Excel export CLI.
- `cmd/enrich/` and `internal/leadership/`: DaData leadership lookup and shared cache workflow.
- `internal/core/`: `Source` interface and source registry.
- `internal/sources/yopt/` and `internal/sources/productcenter/`: site-specific adapters.
- `internal/runner/`: parsing orchestration, persistence, and downloads.
- `internal/models/` and `internal/storage/`: shared records, SQLite access, and file storage.
- `migrations/`: numbered SQL migrations with paired `.up.sql` and `.down.sql` files.

Tests live alongside packages in `*_test.go`; classifier fixtures are in `internal/categorize/testdata/`. Runtime downloads go into `prices/`.

## Build, Test, and Development Commands

Run commands from the repository root. Use the Go version declared in `go.mod` (currently `1.27.1`), a C compiler with CGO enabled for `go-sqlite3`, and a `migrate` CLI supporting SQLite.

- `go build ./...`: compile all packages and check both CLIs.
- `go test ./...`: run all package tests.
- `go vet ./...`: run Go's static checks.
- `go run ./cmd/enrich --dry-run --limit 5`: validate lookup identifiers without API calls. Real lookups require `DADATA_API_KEY`; never log or commit it.
- `make migrate-up`: apply migrations to `data.db`; run before parsing or exporting.
- `go run ./cmd/parse -sources yopt -db data.db`: collect suppliers; use `-sources all` for both adapters or `-force` to reprocess existing records.
- `go run ./cmd/export -db data.db -out companies.xlsx`: export records; optionally filter with `-source yopt`.
- `make migrate-create name=add_something`: create a migration pair.

## Coding Style & Naming Conventions

Format Go changes with `gofmt`; use tabs, lowercase package names, and idiomatic exported names. No custom formatter or linter configuration exists. Keep site-specific selectors inside their adapter. New adapters must implement `core.Source`, register through `core.Register`, and be blank-imported by `cmd/parse/main.go`.

## Testing Guidelines

Add tests using Go's `testing` package in adjacent `*_test.go` files, with `TestXxx` function names. Prefer table-driven parser cases, local HTML fixtures under `testdata/`, and `httptest` servers over live websites. Test storage against temporary migrated databases. No coverage threshold is configured; cover changed behavior and error paths.

## Commit & Pull Request Guidelines

History uses short descriptive subjects such as `productcenter adapter` and `fix price-list sheet header`; no mandatory prefix convention is evident. Keep commits focused. PRs should describe behavior changes, affected sources or schema, verification commands and results, and related issues when applicable. Include export examples for workbook changes. Keep generated `*.db`, `*.xlsx`, `prices/`, and `.env` files out of commits.
