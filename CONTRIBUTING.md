# Contributing to sqlite-server

Thank you for your interest in contributing!

## Development Setup

```bash
git clone https://github.com/Brah-Timo/sqlite-server
cd sqlite-server

# Verify Go ≥ 1.22
go version

# Download dependencies (first run compiles modernc.org/sqlite — ~3 min)
go mod download

# Build
go build -o sqlite-server ./cmd/sqlite-server

# Smoke test
./sqlite-server --no-auth -- /tmp/test.db &
psql -h localhost -p 5432 -U test -c "SELECT 1"
kill %1
```

## Running Tests

```bash
# Unit tests (fast, no server required)
go test ./tests/unit/... -v

# Unit tests with race detector
go test ./tests/unit/... -race

# Unit tests with coverage
go test ./tests/unit/... -cover

# Integration tests (starts server automatically)
go test ./tests/integration/... -v -timeout 120s

# All tests
go test ./... -timeout 120s
```

## Code Style

```bash
go fmt ./...          # format all files
go vet ./internal/... ./sql/... ./compat/... ./cmd/...  # vet (exclude sqlite lib)
```

## Project Layout

```
cmd/sqlite-server/    # CLI entry point (cobra)
internal/
  pgproto/            # shared leaf package — ColumnDesc, QueryResult, OID constants
  wire/               # PostgreSQL wire protocol v3
  pool/               # SQLite connection pool + writer scheduler
  engine/             # SQL executor (planner + catalog integration)
  catalog/            # virtual pg_catalog / information_schema
  errors/             # PGError + SQLite → PG error translation
sql/
  lexer/              # SQL tokenizer
  ast/                # AST node types
  parser/             # PostgreSQL grammar parser
  planner/            # rewriter: PG SQL → SQLite SQL
compat/postgres/      # type/function/operator compatibility tables
tests/
  unit/               # offline unit tests (planner, pgproto)
  integration/        # end-to-end tests via lib/pq
configs/              # example configuration files
```

## Adding SQL Translation Rules

Edit `sql/planner/rewriter.go` — the `rewriteNode()` function dispatches on AST node type.  
Add a unit test in `tests/unit/translator_test.go` covering the new translation.

## Adding Catalog Queries

Edit `internal/catalog/catalog.go` — the `Handle()` method checks query prefixes and returns synthetic `*pgproto.QueryResult` values.

## Pull Request Process

1. Fork and create a feature branch: `git checkout -b feat/my-feature`
2. Write tests for new behaviour
3. Run `go fmt ./...` and ensure `go test ./tests/unit/...` passes
4. Open a PR with a clear description of the change and why
5. Ensure CI passes

## Reporting Bugs

Open a GitHub issue with:
- sqlite-server version (`./sqlite-server version`)
- Go version (`go version`)
- OS / arch
- Minimal reproducer (SQL + client)
- Actual vs expected behaviour
