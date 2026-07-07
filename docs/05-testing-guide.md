# Testing Guide

This document explains how to run all tests for sqlite-server, what each test
suite covers, and how to write new tests.

---

## Table of Contents

1. [Test Suites Overview](#test-suites-overview)
2. [Prerequisites](#prerequisites)
3. [Unit Tests](#unit-tests)
   - [Running Unit Tests](#running-unit-tests)
   - [Unit Test Files](#unit-test-files)
   - [translator_test.go — Test List](#translator_testgo--test-list)
   - [messages_test.go — Test List](#messages_testgo--test-list)
4. [Integration Tests](#integration-tests)
   - [Running Integration Tests (Bash)](#running-integration-tests-bash)
   - [Running Integration Tests (PowerShell)](#running-integration-tests-powershell)
   - [crud_test.go — Test List](#crud_testgo--test-list)
5. [Race Detector](#race-detector)
6. [Coverage Reports](#coverage-reports)
7. [Make Targets](#make-targets)
8. [Writing New Tests](#writing-new-tests)
   - [New Unit Test](#new-unit-test)
   - [New Integration Test](#new-integration-test)
9. [CI Notes](#ci-notes)

---

## Test Suites Overview

| Suite | Location | Requires server | Go package |
|-------|----------|----------------|------------|
| Unit — SQL translator | `tests/unit/translator_test.go` | No | `tests/unit` |
| Unit — pgproto messages | `tests/unit/messages_test.go` | No | `tests/unit` |
| Integration — CRUD | `tests/integration/crud_test.go` | Yes (port 15432) | `tests/integration` |

---

## Prerequisites

- Go 1.21+ installed
- `lib/pq` driver (integration tests only) — already in `go.mod`
- A built `sqlite-server` binary for integration tests

```bash
# Build the binary
go build -o sqlite-server ./cmd/sqlite-server/
```

---

## Unit Tests

Unit tests have **no external dependencies**. They test internal packages directly
without starting any server or database.

### Running Unit Tests

```bash
# Run all unit tests
go test ./tests/unit/... -v

# With race detector
go test ./tests/unit/... -race -v

# With coverage
go test ./tests/unit/... -race -cover -coverprofile=coverage.out

# View coverage in browser
go tool cover -html=coverage.out

# Quick smoke test (no verbose output)
go test ./tests/unit/...
```

Expected output:
```
=== RUN   TestTranslateSelectOne
--- PASS: TestTranslateSelectOne (0.00s)
=== RUN   TestTranslateNowFunction
--- PASS: TestTranslateNowFunction (0.00s)
...
PASS
ok      github.com/sqlite-server/sqlite-server/tests/unit    0.004s
```

All 29+ unit tests pass in under 1 second (no I/O, no network).

### Unit Test Files

```
tests/unit/
├── translator_test.go    # Tests sql/planner Rewrite() output
└── messages_test.go      # Tests internal/pgproto types and OID mapping
```

### translator_test.go — Test List

Tests that `planner.New().Rewrite(input)` produces the expected SQLite SQL:

| Test function | Input (PG SQL) | Verified output |
|---------------|---------------|-----------------|
| `TestTranslateSelectOne` | `SELECT 1` | `SELECT 1` |
| `TestTranslateNowFunction` | `SELECT NOW()` | `SELECT datetime('now')` |
| `TestTranslateCurrentTimestamp` | `SELECT CURRENT_TIMESTAMP` | `SELECT datetime('now')` |
| `TestTranslateILIKE` | `SELECT … WHERE name ILIKE '%alice%'` | `… name LIKE '%alice%'` |
| `TestTranslateExtractYear` | `EXTRACT(YEAR FROM created_at)` | `strftime('%Y', created_at)` |
| `TestTranslateSerialType` | `CREATE TABLE t (id SERIAL)` | `id INTEGER` |
| `TestTranslateBigSerial` | `CREATE TABLE t (id BIGSERIAL)` | `id INTEGER` |
| `TestTranslatePlaceholders` | `SELECT … WHERE id = $1` | `… WHERE id = ?` |
| `TestTranslateDoubleColon` | `SELECT '2024-01-01'::DATE` | `SELECT CAST('2024-01-01' AS DATE)` |
| `TestTranslateSetStatement` | `SET TIME ZONE 'UTC'` | *(no-op / absorbed)* |
| `TestTranslateShowStatement` | `SHOW search_path` | returns `"public"` |
| `TestTranslateCTE` | `WITH cte AS (SELECT 1) SELECT …` | valid SQLite CTE |
| `TestTranslateEmptyQuery` | `""` / `"  "` | returns empty without error |
| `TestTranslateIdempotent` | Already-translated SQL | passes through unchanged |

### messages_test.go — Test List

Tests for `internal/pgproto` (OID constants, type mapping, encoding):

| Test function | What it verifies |
|---------------|-----------------|
| `TestSQLiteTypeToOIDText` | `"TEXT"` → OID 25 |
| `TestSQLiteTypeToOIDInteger` | `"INTEGER"` → OID 23 |
| `TestSQLiteTypeToOIDReal` | `"REAL"` → OID 701 |
| `TestSQLiteTypeToOIDBlob` | `"BLOB"` → OID 17 |
| `TestSQLiteTypeToOIDNumeric` | `"NUMERIC"` → OID 1700 |
| `TestSQLiteTypeToOIDBoolean` | `"BOOLEAN"` → OID 16 |
| `TestSQLiteTypeToOIDTimestamp` | `"TIMESTAMP"` → OID 1114 |
| `TestSQLiteTypeToOIDDate` | `"DATE"` → OID 1082 |
| `TestSQLiteTypeToOIDVarchar` | `"VARCHAR(100)"` → OID 25 |
| `TestSQLiteTypeToOIDEmpty` | `""` → OID 25 (TEXT fallback) |
| `TestOIDToTypeName` | 18 OID → name spot-checks |
| `TestOIDToTypeNameUnknown` | unknown OID → `"unknown"` |
| `TestColumnDescDefaults` | zero-value `ColumnDesc` fields |
| `TestQueryResultIsSelect_True` | result with Columns → `IsSelect()` true |
| `TestQueryResultIsSelect_False` | result without Columns → `IsSelect()` false |
| `TestQueryResultIsSelect_EmptyCols` | empty Columns slice → false |
| `TestBigEndianEncoding` | verifies network byte order helpers |
| `TestDecodeParamValueText` | text-format param → string |
| `TestDecodeParamValueBinary` | binary-format int4 param → int32 |
| `TestDecodeParamValueNull` | nil data → nil value |
| `TestOIDConstantsSpotCheck` | OIDBool=16, OIDInt4=23, OIDText=25, … |
| `TestOIDFloat8` | `OIDFloat8 == 701` |
| `TestOIDTimestamp` | `OIDTimestamp == 1114` |
| `TestOIDUUID` | `OIDUUID == 2950` |
| `TestOIDVarchar` | `OIDVarchar == 1043` |
| `TestOIDJSON` | `OIDJSON == 114` |
| `TestOIDJSONB` | `OIDJSONB == 3802` |
| `TestOIDArray` | array OID construction |

---

## Integration Tests

Integration tests connect to a live sqlite-server instance. They test the full stack:
TCP connection → wire protocol → engine → SQLite → wire response → client.

The test binary connects to `127.0.0.1:15432` (non-standard port to avoid conflicts
with a local PostgreSQL install).

### Running Integration Tests (Bash)

```bash
# ── Step 1: Build the binary ──────────────────────────────────────────────
go build -o sqlite-server ./cmd/sqlite-server/

# ── Step 2: Start the server on port 15432 ───────────────────────────────
./sqlite-server serve \
  --addr 127.0.0.1:15432 \
  --database :memory: \
  --no-auth \
  --log-level debug &

SERVER_PID=$!
sleep 1   # wait for server to be ready

# ── Step 3: Run integration tests ────────────────────────────────────────
go test ./tests/integration/... \
  -v \
  -timeout 120s \
  -tags integration

# ── Step 4: Stop the server ───────────────────────────────────────────────
kill $SERVER_PID
```

**One-liner version:**

```bash
go build -o sqlite-server ./cmd/sqlite-server/ && \
  ./sqlite-server serve --addr 127.0.0.1:15432 --database :memory: --no-auth &
SERVER_PID=$! && sleep 1 && \
  go test ./tests/integration/... -v -timeout 120s; \
  kill $SERVER_PID
```

### Running Integration Tests (PowerShell)

```powershell
# ── Step 1: Build ─────────────────────────────────────────────────────────
go build -o sqlite-server.exe .\cmd\sqlite-server\

# ── Step 2: Start server ───────────────────────────────────────────────────
$srv = Start-Process -FilePath ".\sqlite-server.exe" `
  -ArgumentList "serve","--addr","127.0.0.1:15432",`
                "--database",":memory:","--no-auth","--log-level","debug" `
  -PassThru -NoNewWindow

Start-Sleep -Seconds 2

# ── Step 3: Run integration tests ─────────────────────────────────────────
go test .\tests\integration\... -v -timeout 120s -tags integration

# ── Step 4: Stop server ────────────────────────────────────────────────────
Stop-Process -Id $srv.Id
```

### crud_test.go — Test List

The integration test file (`tests/integration/crud_test.go`) contains 25+ test
functions. `TestMain` starts the server automatically if it is not already running.

| Test function | Category | What it tests |
|---------------|----------|---------------|
| `TestConnectivity` | Connectivity | Can connect and execute `SELECT 1` |
| `TestDatabaseVersion` | Connectivity | `SELECT version()` returns non-empty string |
| `TestCurrentDatabase` | Connectivity | `SELECT current_database()` returns a string |
| `TestCreateTable` | DDL | `CREATE TABLE` succeeds |
| `TestDropTable` | DDL | `CREATE TABLE` then `DROP TABLE` |
| `TestCreateIndex` | DDL | `CREATE INDEX` on a column |
| `TestInsertAndSelect` | CRUD | Insert a row and read it back |
| `TestInsertMultipleRows` | CRUD | Bulk insert + `COUNT(*)` |
| `TestUpdateRow` | CRUD | `UPDATE … SET … WHERE` |
| `TestDeleteRow` | CRUD | `DELETE … WHERE` |
| `TestInsertReturning` | CRUD | `INSERT … RETURNING id` |
| `TestTransactionCommit` | Transactions | `BEGIN … COMMIT` — rows persist |
| `TestTransactionRollback` | Transactions | `BEGIN … ROLLBACK` — rows disappear |
| `TestTransactionIsolation` | Transactions | Concurrent sessions don't see uncommitted data |
| `TestSavepoint` | Transactions | `SAVEPOINT` + `ROLLBACK TO SAVEPOINT` |
| `TestPreparedStatementSelect` | Prepared Stmts | `$1` placeholder in SELECT |
| `TestPreparedStatementInsert` | Prepared Stmts | `$1` / `$2` in INSERT |
| `TestPreparedStatementReuse` | Prepared Stmts | Execute same prepared stmt 100× |
| `TestDBeaverPgType` | DBeaver Catalog | `SELECT typname, oid FROM pg_catalog.pg_type` |
| `TestDBeaverTables` | DBeaver Catalog | `information_schema.tables` returns user tables |
| `TestDBeaverColumns` | DBeaver Catalog | `information_schema.columns` returns column names |
| `TestNullValues` | Data Types | INSERT / SELECT NULL columns |
| `TestDataTypes` | Data Types | INTEGER, TEXT, REAL, BOOLEAN, TIMESTAMP round-trip |
| `TestConcurrentConnections` | Concurrency | 10 goroutines run SELECTs simultaneously |
| `TestConcurrentWrites` | Concurrency | 5 goroutines INSERT without conflicts |
| `TestCTEQuery` | SQL Features | `WITH … AS (…) SELECT …` |
| `TestSystemFunctions` | SQL Features | `NOW()`, `CURRENT_TIMESTAMP`, `EXTRACT(…)` |

---

## Race Detector

Always run unit tests with `-race` before submitting a PR:

```bash
go test ./tests/unit/... -race -v -timeout 60s
```

For integration tests (with server running):

```bash
go test ./tests/integration/... -race -v -timeout 120s
```

The race detector adds ~2–10× overhead but catches data races that only manifest
under concurrent load.

---

## Coverage Reports

```bash
# Generate coverage profile
go test ./tests/unit/... \
  -race \
  -cover \
  -coverprofile=coverage.out \
  -coverpkg=./...

# Display per-package summary in terminal
go tool cover -func=coverage.out

# Open interactive HTML report
go tool cover -html=coverage.out -o coverage.html
open coverage.html   # macOS
xdg-open coverage.html   # Linux
start coverage.html  # Windows
```

Target coverage: **≥ 80%** on `internal/` and `sql/` packages.

---

## Make Targets

```bash
make test-unit          # go test ./tests/unit/... -race -cover
make test-integration   # start server + run integration tests + stop server
make coverage           # unit tests + HTML coverage report
make fmt                # go fmt ./...
make vet                # go vet (own packages, skips sqlite lib)
make lint               # staticcheck ./... (requires staticcheck installed)
make tidy               # go mod tidy
```

---

## Writing New Tests

### New Unit Test

Add a function to an existing file in `tests/unit/`, or create a new file:

```go
// tests/unit/my_test.go
package unit

import (
    "testing"
    "github.com/sqlite-server/sqlite-server/sql/planner"
)

func TestMyNewTranslation(t *testing.T) {
    p := planner.New()
    got, err := p.Rewrite("SELECT CURRENT_DATE")
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    want := "SELECT date('now')"
    if got != want {
        t.Errorf("got %q, want %q", got, want)
    }
}
```

Run it:

```bash
go test ./tests/unit/... -run TestMyNewTranslation -v
```

### New Integration Test

Add a function to `tests/integration/crud_test.go`:

```go
func TestMyFeature(t *testing.T) {
    db := openTestDB(t)  // helper that connects to 127.0.0.1:15432
    defer db.Close()

    _, err := db.Exec(`CREATE TABLE IF NOT EXISTS widgets (id SERIAL, label TEXT)`)
    if err != nil {
        t.Fatal(err)
    }

    _, err = db.Exec(`INSERT INTO widgets (label) VALUES ($1)`, "gear")
    if err != nil {
        t.Fatal(err)
    }

    var label string
    err = db.QueryRow(`SELECT label FROM widgets WHERE label = $1`, "gear").Scan(&label)
    if err != nil {
        t.Fatal(err)
    }
    if label != "gear" {
        t.Errorf("got %q, want %q", label, "gear")
    }
}
```

The `openTestDB` helper (defined at the top of `crud_test.go`) calls `t.Skip()`
automatically if the server is not reachable, so the test is skipped rather than
failing in CI environments where the server is not running.

---

## CI Notes

- Unit tests run in CI on every pull request without any additional setup.
- Integration tests require a running server; set them up in CI with a step that
  builds and starts the server before the test step.
- The build step (`go build ./cmd/sqlite-server/`) compiles `modernc.org/sqlite`
  which may take **3–5 minutes** on a cold cache. Warm Go module cache in CI to
  speed this up.
- Use `-timeout 120s` for integration tests to avoid hanging CI jobs.
- `go vet ./...` will OOM on `modernc.org/sqlite/lib` (a very large generated file).
  Run vet only on project packages:

```bash
go vet $(go list ./... | grep -v 'modernc.org')
# or equivalently:
go vet ./cmd/... ./internal/... ./sql/... ./compat/... ./tests/...
```
