# Troubleshooting Guide

This document covers common problems encountered when building, running, and
connecting to sqlite-server, along with their causes and solutions.

---

## Table of Contents

1. [Build Errors](#build-errors)
2. [Connection Errors](#connection-errors)
3. [Authentication Errors](#authentication-errors)
4. [SQL / Query Errors](#sql--query-errors)
5. [Runtime / Performance Issues](#runtime--performance-issues)
6. [Test Failures](#test-failures)
7. [go vet / go test OOM](#go-vet--go-test-oom)
8. [FAQ](#faq)

---

## Build Errors

### `cannot use engine.New() (type *engine.Executor) as type engine.Executor`

**Cause**: The `ConnPool` struct declared `executor engine.Executor` (value type)
but `engine.New()` returns `*engine.Executor` (pointer type).

**Fix**: Change the field declaration in `internal/pool/connpool.go`:

```go
// Before (wrong)
type ConnPool struct {
    executor engine.Executor
    ...
}

// After (correct)
type ConnPool struct {
    executor *engine.Executor
    ...
}
```

---

### `import cycle not allowed`

**Cause**: Two packages import each other, e.g. `wire` imports `engine` and `engine`
imports `wire`.

**Fix**: The `internal/pgproto` package is the shared leaf. Move any types that both
sides need into `pgproto`. Then update both packages to import `pgproto` instead of
each other.

```
// Rule: nothing imports wire; wire imports everything else
wire  →  engine  →  pgproto  (leaf, no internal imports)
wire  →  catalog →  pgproto
```

---

### `undefined: ast.RawExpr`

**Cause**: `RawExpr` was defined outside the `sql/ast` package. The `exprTag()`
method is **unexported**, so types satisfying the `Expr` interface must be in the
same package.

**Fix**: Define `RawExpr` in `sql/ast/ast.go`:

```go
// sql/ast/ast.go
type RawExpr struct{ SQL string }
func (e *RawExpr) nodeTag() {}
func (e *RawExpr) exprTag() {}
func (e *RawExpr) String() string { return e.SQL }
```

In `sql/planner/rewriter.go`, use `&ast.RawExpr{SQL: "..."}`.

---

### `undefined: token.KW_WITHOUT` (or KW_ZONE, KW_WORK, etc.)

**Cause**: The keyword constants were not added to `sql/lexer/token.go`.

**Fix**: Add the missing constants to the `TokenType` iota block and update
`IsKeyword()` upper bound:

```go
// sql/lexer/token.go — at end of keyword block
KW_WITHOUT
KW_ZONE
KW_WORK
KW_LOCAL
KW_SESSION
KW_OF    // ← upper bound for IsKeyword()
```

Also add them to the `keywords` map in `sql/lexer/lexer.go`:

```go
"without": KW_WITHOUT,
"zone":    KW_ZONE,
"work":    KW_WORK,
"local":   KW_LOCAL,
"session": KW_SESSION,
"of":      KW_OF,
```

---

### First build takes 3–5 minutes

**Cause**: `modernc.org/sqlite` transpiles a large C codebase to Go on the first
compilation. Subsequent builds use the cached result and are fast.

**Fix**: This is expected behaviour. Warm the module cache in CI:

```yaml
# GitHub Actions example
- uses: actions/cache@v3
  with:
    path: |
      ~/go/pkg/mod
      ~/.cache/go-build
    key: go-${{ hashFiles('go.sum') }}
```

---

### `checksum mismatch` in go.sum

**Cause**: The `go.sum` file is stale or was hand-edited.

**Fix**: Delete `go.sum` and regenerate it:

```bash
rm go.sum
GONOSUMDB="*" go mod tidy
go mod verify
```

---

## Connection Errors

### `connection refused` / `dial tcp 127.0.0.1:5432: connect: connection refused`

**Cause**: The server is not running, or it started on a different port.

**Fix**:
1. Check that the server process is running: `ps aux | grep sqlite-server`
2. Verify the port: `./sqlite-server serve --addr 127.0.0.1:5432 ...`
3. Check for port conflict: `ss -tlnp | grep 5432` (Linux) or `netstat -an | grep 5432`

---

### `too many connections`

**Cause**: Active connections exceed `--max-conn`.

**Fix**: Increase `--max-conn` or close idle connections in your application:

```bash
./sqlite-server serve --max-conn 200 ...
```

In Go applications, configure the connection pool:
```go
db.SetMaxOpenConns(20)
db.SetMaxIdleConns(5)
db.SetConnMaxLifetime(5 * time.Minute)
```

---

### `SSL SYSCALL error` / `SSL connection has been closed unexpectedly`

**Cause**: Client is requesting TLS but server is running without TLS (or vice versa).

**Fix**: Add `sslmode=disable` to your client connection string, or start the server
with `--ssl-cert` and `--ssl-key`:

```bash
# Client side — disable TLS
psql "host=127.0.0.1 port=5432 user=x dbname=x sslmode=disable"

# Server side — enable TLS
./sqlite-server serve --ssl-cert cert.pem --ssl-key key.pem
```

---

## Authentication Errors

### `password authentication failed for user "..."`

**Cause**: Server is running with authentication enabled (default) but client is
providing an incorrect password.

**Fix — Option 1**: Run the server with `--no-auth` for development:
```bash
./sqlite-server serve --no-auth ...
```

**Fix — Option 2**: Connect with the correct credentials (check your config file).

---

### `FATAL: no pg_hba.conf entry for host "..."`

**Cause**: This error message is copied verbatim from PostgreSQL but in
sqlite-server it means the client's startup message was malformed or the
handshake failed at the protocol level.

**Fix**: Check that you are using a PostgreSQL client (not MySQL, Redis, etc.) and
that the port is correct.

---

## SQL / Query Errors

### `ERROR: syntax error at or near "::"`

**Cause**: The `::` cast operator was not recognized by the lexer/parser.

**Expected**: The planner should translate `'42'::INTEGER` → `CAST('42' AS INTEGER)`.
If you see this error, it means the query reached SQLite un-translated.

**Fix**: File a bug with the exact SQL string. As a workaround, use `CAST(...)` directly:
```sql
-- Instead of
SELECT '2024-01-01'::DATE

-- Use
SELECT CAST('2024-01-01' AS TEXT)
```

---

### `ERROR: column "ctid" does not exist`

**Cause**: Some PostgreSQL clients (e.g. older DBeaver versions) query the `ctid`
system column which does not exist in SQLite.

**Fix**: Update DBeaver to the latest version. The virtual catalog in newer versions
of sqlite-server handles this query.

---

### `disk I/O error` during tests

**Cause**: The server process was killed while a transaction was open, or the test
binary started a second server instance pointing to the same database file.

**Fix**:
1. Delete the leftover database file: `rm -f *.db`
2. Make sure only one server instance runs at a time on the same database file
3. Use `:memory:` for test runs to avoid file-level conflicts

---

### `no such table: pg_type`

**Cause**: A query references `pg_type` without the `pg_catalog.` prefix, and the
catalog interceptor uses the fully-qualified form.

**Fix**: Always use the fully-qualified name: `pg_catalog.pg_type`. Or use
`information_schema` views which do not require a prefix.

---

### `RETURNING` clause returns an error

**Cause**: `RETURNING` requires SQLite 3.35 or later. Older versions of
`modernc.org/sqlite` may not support it.

**Fix**: Upgrade the module to `v1.33.1` or later (which includes SQLite 3.45+):

```bash
go get modernc.org/sqlite@v1.33.1
go mod tidy
```

---

## Runtime / Performance Issues

### High CPU usage under load

**Cause**: All write queries are serialised through the writer goroutine. Many
concurrent `INSERT`/`UPDATE`/`DELETE` statements queue up, causing context-switching
overhead.

**Fix**:
- Use batch `INSERT` statements: `INSERT INTO t VALUES (1), (2), (3)`
- Wrap multiple writes in a single transaction
- Reduce `max-conn` to limit parallelism: `--max-conn 20`

---

### Slow reads on large tables

**Cause**: Missing index on the `WHERE` column.

**Fix**: Add an index:
```sql
CREATE INDEX idx_users_email ON users(email);
```

Verify the query plan with:
```sql
EXPLAIN QUERY PLAN SELECT * FROM users WHERE email = 'alice@example.com';
```

---

### Database file grows unbounded

**Cause**: SQLite WAL mode keeps old pages in the WAL file until a checkpoint runs.

**Fix**: The WAL is checkpointed automatically every 1000 pages. To checkpoint
manually, connect and run:
```sql
PRAGMA wal_checkpoint(TRUNCATE);
```

For very high write workloads, configure `PRAGMA wal_autocheckpoint`:
```sql
PRAGMA wal_autocheckpoint = 100;   -- checkpoint every 100 pages
```

---

## Test Failures

### `⚠ Could not start sqlite-server: fork/exec ./sqlite-server-test-bin: no such file or directory`

**Cause**: `TestMain` in `crud_test.go` tries to build and run the server binary
from the `tests/integration/` directory. The relative path `../../cmd/sqlite-server`
resolves differently depending on where `go test` is invoked from.

**Fix**: Start the server manually before running integration tests:

```bash
# Terminal 1: Start server
./sqlite-server serve --addr 127.0.0.1:15432 --database :memory: --no-auth

# Terminal 2: Run tests
go test ./tests/integration/... -v -timeout 120s
```

---

### Integration tests skip with `server not available`

**Cause**: The integration tests call `t.Skip()` if they cannot connect to
`127.0.0.1:15432`. This is intentional — integration tests are skipped, not failed,
when the server is absent.

**Fix**: Start the server before running integration tests (see above).

---

### Unit test `unexpected translation` error

**Cause**: The planner rewrote a query differently than the test expected, likely
after a change to `sql/planner/rewriter.go`.

**Fix**: Review the planner change and update the expected string in the test, or
fix the planner if the new output is wrong.

---

## go vet / go test OOM

### `go vet ./...` killed (out of memory)

**Cause**: `modernc.org/sqlite/lib` contains an enormous generated Go file
(~200 MB source). The vet tool allocates too much memory analysing it.

**Fix**: Run vet only on your own packages:

```bash
go vet ./cmd/... ./internal/... ./sql/... ./compat/... ./tests/...
```

Or use the Makefile target:
```bash
make vet
```

---

### `go test ./...` killed during compilation

**Cause**: Same as above — compiling the sqlite lib in test mode uses significant
memory.

**Fix**: Run tests on specific packages rather than `./...`:

```bash
go test ./tests/unit/... -v
go test ./tests/integration/... -v
```

---

## FAQ

**Q: Can I use sqlite-server as a drop-in replacement for PostgreSQL?**

A: For simple CRUD applications yes. Complex PostgreSQL features (triggers, stored
procedures, full-text search, arrays, JSONB indexing, logical replication) are not
supported. See [SQL Compatibility](06-sql-compatibility.md) for the full list.

---

**Q: Is the data persistent between restarts?**

A: Yes, if you use a file path for `--database`. Use `--database :memory:` only for
ephemeral/test workloads.

---

**Q: Can multiple processes open the same database file?**

A: No. SQLite allows only one writer process at a time. Running two sqlite-server
instances on the same `.db` file will cause `database is locked` errors.

---

**Q: What happens to in-flight transactions if the server crashes?**

A: SQLite's WAL journal ensures the database is never left in a corrupt state.
Uncommitted transactions are rolled back automatically on the next open.

---

**Q: Does sqlite-server support SSL/TLS?**

A: Yes. Pass `--ssl-cert` and `--ssl-key` flags. The server performs the TLS
handshake before the PostgreSQL startup message. Most clients use `sslmode=require`.

---

**Q: How do I enable foreign key enforcement?**

A: SQLite requires an explicit `PRAGMA` per connection. Connect and run:
```sql
PRAGMA foreign_keys = ON;
```
Note: this must be run once per connection and does not persist in the file.

---

**Q: Can I use DBeaver / pgAdmin / TablePlus?**

A: Yes. Connect using the PostgreSQL driver, point to `127.0.0.1:5432`, and set
`sslmode=disable`. The virtual catalog provides the metadata queries these tools
expect. See [Running sqlite-server — DBeaver section](03-running-and-cli.md#dbeaver).

---

**Q: Why does `go build` succeed but `./sqlite-server` fails with `exec format error`?**

A: The binary was cross-compiled for a different OS/architecture. For example,
building with `GOOS=linux` on macOS produces a Linux binary that cannot run on macOS.
Build for your current platform:

```bash
# Remove any cross-compiled binary
rm sqlite-server

# Build for current platform
go build -o sqlite-server ./cmd/sqlite-server/
```
