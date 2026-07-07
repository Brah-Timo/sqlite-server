# Developer Guide

This guide is for contributors who want to extend sqlite-server: adding new SQL
translation rules, supporting new catalog queries, adding wire protocol features,
or fixing bugs.

---

## Table of Contents

1. [Development Setup](#development-setup)
2. [Project Layout](#project-layout)
3. [Core Code Patterns](#core-code-patterns)
   - [Adding a SQL Translation Rule](#adding-a-sql-translation-rule)
   - [Adding a New Keyword](#adding-a-new-keyword)
   - [Adding a Virtual Catalog Query](#adding-a-virtual-catalog-query)
   - [Adding a New OID Constant](#adding-a-new-oid-constant)
   - [Adding a New CLI Flag](#adding-a-new-cli-flag)
   - [Handling a New Wire Protocol Message](#handling-a-new-wire-protocol-message)
4. [Testing Your Changes](#testing-your-changes)
5. [Code Style](#code-style)
6. [Debugging Tips](#debugging-tips)
7. [Pull Request Checklist](#pull-request-checklist)
8. [Module Management](#module-management)
9. [Release Process](#release-process)

---

## Development Setup

```bash
# Clone the repository
git clone https://github.com/sqlite-server/sqlite-server.git
cd sqlite-server

# Install Go 1.21 or later
go version   # should print "go version go1.21.x ..."

# Download dependencies (first time: 3–5 min for modernc.org/sqlite)
go mod download

# Build the binary
go build -o sqlite-server ./cmd/sqlite-server/

# Run unit tests (fast, no server needed)
go test ./tests/unit/... -race -v

# Smoke test
./sqlite-server version
```

### Recommended tools

```bash
# Formatting
gofmt -w .           # built-in
goimports -w .       # also organises imports (go install golang.org/x/tools/cmd/goimports@latest)

# Static analysis
staticcheck ./...    # go install honnef.co/go/tools/cmd/staticcheck@latest

# Editor: VS Code with the Go extension, or GoLand
```

---

## Project Layout

```
sqlite-server/
├── cmd/
│   └── sqlite-server/
│       └── main.go          ← cobra CLI (entry point)
│
├── internal/
│   ├── pgproto/
│   │   └── types.go         ← LEAF: shared types + OID constants
│   ├── wire/
│   │   ├── server.go        ← TCP listener
│   │   ├── session.go       ← per-connection state machine
│   │   ├── startup.go       ← startup + auth phase
│   │   ├── auth.go          ← MD5 / trust authentication
│   │   ├── simple_query.go  ← 'Q' message handler
│   │   ├── extended_query.go← 'P','B','E','D','S','C','H' handlers
│   │   ├── messages.go      ← low-level message builders
│   │   └── types.go         ← thin aliases over pgproto
│   ├── pool/
│   │   └── connpool.go      ← connection pool + writer goroutine
│   ├── engine/
│   │   ├── executor.go      ← query dispatcher
│   │   ├── translator.go    ← SQL helper functions
│   │   └── optimizer.go     ← lightweight optimizer
│   ├── catalog/
│   │   └── catalog.go       ← pg_catalog + information_schema interceptor
│   └── errors/
│       ├── pgerrors.go      ← PGError type
│       └── sqlstate.go      ← SQLSTATE constants
│
├── sql/
│   ├── lexer/
│   │   ├── token.go         ← TokenType constants + IsKeyword()
│   │   └── lexer.go         ← Tokenizer
│   ├── ast/
│   │   └── ast.go           ← AST node types (Stmt, Expr, RawExpr, …)
│   ├── parser/
│   │   └── parser.go        ← recursive-descent parser
│   └── planner/
│       ├── planner.go       ← Planner struct, Rewrite() entry point
│       ├── rewriter.go      ← AST → SQLite SQL rewriter
│       └── normalizer.go    ← AST normaliser
│
├── compat/postgres/
│   ├── functions.go         ← PG function → SQLite function map
│   ├── types.go             ← PG type → SQLite type map
│   └── operators.go         ← PG operator → SQLite operator map
│
├── tests/
│   ├── unit/
│   │   ├── translator_test.go
│   │   └── messages_test.go
│   └── integration/
│       └── crud_test.go
│
├── configs/
│   ├── dev.yaml
│   └── production.yaml
│
├── docs/                    ← this documentation
├── go.mod
├── go.sum
├── Makefile
└── Dockerfile
```

---

## Core Code Patterns

### Adding a SQL Translation Rule

The planner rewrites the AST in `sql/planner/rewriter.go`. Each rule is a case in
the AST-walking switch statement.

**Example: translate `MD5(s)` → `hex(hash(s))`**

**Step 1** — Add the function mapping to `compat/postgres/functions.go`:

```go
// compat/postgres/functions.go
var FunctionMap = map[string]string{
    // ... existing entries ...
    "md5": "hex(hash(%s))",   // ← new entry
}
```

**Step 2** — Handle it in the rewriter (`sql/planner/rewriter.go`):

```go
// In the rewriteExpr function, inside the *ast.FunctionCall case:
case *ast.FunctionCall:
    name := strings.ToLower(expr.Name)
    if tmpl, ok := compat.FunctionMap[name]; ok {
        // rewrite arguments recursively
        args := r.rewriteArgs(expr.Args)
        return &ast.RawExpr{SQL: fmt.Sprintf(tmpl, args...)}
    }
```

**Step 3** — Write a unit test in `tests/unit/translator_test.go`:

```go
func TestTranslateMD5(t *testing.T) {
    p := planner.New()
    got, err := p.Rewrite("SELECT MD5('hello')")
    if err != nil {
        t.Fatal(err)
    }
    want := "SELECT hex(hash('hello'))"
    if got != want {
        t.Errorf("got %q, want %q", got, want)
    }
}
```

**Step 4** — Run tests:

```bash
go test ./tests/unit/... -run TestTranslateMD5 -v
```

---

### Adding a New Keyword

If you need to parse a new PostgreSQL keyword, add it to both the token constants
and the keyword map.

**Step 1** — Add the constant to `sql/lexer/token.go`:

```go
// In the keyword TokenType iota block, after the last existing keyword:
KW_RECURSIVE   // ← example new keyword
// Update the upper bound comment:
// IsKeyword returns true for KW_SELECT ... KW_RECURSIVE
```

Update the `IsKeyword` function if the new constant is at the end of the range:

```go
func (t TokenType) IsKeyword() bool {
    return t >= KW_SELECT && t <= KW_RECURSIVE  // ← updated upper bound
}
```

**Step 2** — Add to the keyword map in `sql/lexer/lexer.go`:

```go
var keywords = map[string]TokenType{
    // ... existing entries ...
    "recursive": KW_RECURSIVE,  // ← lowercase
}
```

**Step 3** — Use it in the parser (`sql/parser/parser.go`):

```go
// Example: parse WITH RECURSIVE
if p.peek() == token.KW_WITH {
    p.consume()
    isRecursive := p.consumeIf(token.KW_RECURSIVE)
    // ... parse CTE body ...
}
```

---

### Adding a Virtual Catalog Query

All catalog interception lives in `internal/catalog/catalog.go`.

**Pattern**: match the SQL string, build a `pgproto.QueryResult`, return it.

```go
// internal/catalog/catalog.go

func Handle(sql string) (*pgproto.QueryResult, bool) {
    sqlLower := strings.ToLower(strings.TrimSpace(sql))

    // ── Existing intercepts ──────────────────────────────────────
    // ... existing switch/if chain ...

    // ── New intercept: pg_catalog.pg_locks ───────────────────────
    if strings.Contains(sqlLower, "pg_catalog.pg_locks") ||
       strings.Contains(sqlLower, "pg_locks") {
        return &pgproto.QueryResult{
            Tag: "SELECT 0",
            Columns: []pgproto.ColumnDesc{
                {Name: "pid",      TypeOID: pgproto.OIDInt4},
                {Name: "locktype", TypeOID: pgproto.OIDText},
            },
            Rows: [][]interface{}{},  // always empty for SQLite
        }, true
    }

    return nil, false   // not a catalog query — pass through to engine
}
```

**Guidelines:**
- Return `(nil, false)` for anything that should reach the real engine
- Return `(result, true)` to intercept and short-circuit
- Use `pgproto.OIDText`, `pgproto.OIDInt4`, etc. for column type OIDs
- Query live schema via `PRAGMA table_list` / `PRAGMA table_info(name)` for
  tables/columns data (see existing implementation for examples)

**Add a unit test:**

```go
// tests/unit/catalog_test.go (create if it does not exist)
func TestCatalogPgLocks(t *testing.T) {
    result, handled := catalog.Handle("SELECT * FROM pg_catalog.pg_locks")
    if !handled {
        t.Fatal("expected catalog to handle pg_locks query")
    }
    if result == nil {
        t.Fatal("expected non-nil result")
    }
}
```

---

### Adding a New OID Constant

Add it to `internal/pgproto/types.go`:

```go
const (
    // existing constants ...
    OIDMyNewType uint32 = 12345  // ← your new OID
)
```

Update `OIDToTypeName` if the type should appear in error messages:

```go
func OIDToTypeName(oid uint32) string {
    switch oid {
    // ... existing cases ...
    case OIDMyNewType:
        return "mytype"
    default:
        return "unknown"
    }
}
```

---

### Adding a New CLI Flag

All flags are defined in `cmd/sqlite-server/main.go` using cobra:

```go
// cmd/sqlite-server/main.go

var myNewFlag string

func init() {
    serveCmd.Flags().StringVar(
        &myNewFlag,
        "my-flag",          // flag name
        "default-value",    // default
        "Description of the flag",
    )
    // Bind to environment variable automatically:
    viper.BindPFlag("my-flag", serveCmd.Flags().Lookup("my-flag"))
    viper.BindEnv("my-flag", "SQLITE_SERVER_MY_FLAG")
}
```

Pass the value to `ServerConfig` in `runServer()`:

```go
func runServer(cmd *cobra.Command, args []string) error {
    cfg := wire.ServerConfig{
        // ... existing fields ...
        MyNewSetting: viper.GetString("my-flag"),
    }
    // ...
}
```

Add the field to `wire.ServerConfig` in `internal/wire/server.go`:

```go
type ServerConfig struct {
    // ... existing fields ...
    MyNewSetting string
}
```

---

### Handling a New Wire Protocol Message

New message types are dispatched in `internal/wire/session.go`, in the
`commandLoop()` function:

```go
func (s *Session) commandLoop() error {
    for {
        msgType, err := s.readByte()
        if err != nil { return err }

        switch msgType {
        case 'Q':
            if err := s.handleSimpleQuery(); err != nil { return err }
        case 'P':
            if err := s.handleParse(); err != nil { return err }
        // ... existing cases ...

        // ── New message type ──────────────────────────────────
        case 'F':  // FunctionCall (deprecated but some clients still send it)
            if err := s.handleFunctionCall(); err != nil { return err }

        default:
            // Unknown message — read and discard payload, send error
            if err := s.discardMessage(); err != nil { return err }
            s.sendError(errors.New("0A000", "unsupported message type"))
        }
    }
}
```

Implement the handler function:

```go
func (s *Session) handleFunctionCall() error {
    // Read the message payload (length already consumed by commandLoop)
    length, err := s.readInt32()
    if err != nil { return err }
    payload := make([]byte, length-4)
    if _, err := io.ReadFull(s.conn, payload); err != nil { return err }

    // Process payload...
    // Send response...
    return nil
}
```

---

## Testing Your Changes

```bash
# Unit tests only (fast, < 1 second)
go test ./tests/unit/... -race -v

# Run a specific test
go test ./tests/unit/... -run TestMyNewTranslation -v

# Integration tests (requires running server)
./sqlite-server serve --addr 127.0.0.1:15432 --database :memory: --no-auth &
PID=$!
go test ./tests/integration/... -v -timeout 120s
kill $PID

# Full check before committing
make fmt vet test-unit
```

---

## Code Style

- **Formatting**: `gofmt` (mandatory). Run `make fmt` before committing.
- **Imports**: stdlib first, then external, then internal. Use blank lines between groups.
- **Error handling**: always check errors; don't use `_` for error returns in non-trivial paths.
- **Comments**: exported functions and types must have doc comments.
- **Naming**:
  - Packages: short, lowercase, no underscores
  - Interfaces: single-method interfaces named `Verb`-`er` (e.g. `Executor`, `Handler`)
  - Test functions: `TestFunctionName_scenario` or `TestFunctionName`
- **No global mutable state** (except the `keywords` map which is read-only after init).
- **Context**: pass `context.Context` as the first argument for any function that
  may block or call I/O.

---

## Debugging Tips

### Enable debug logging

```bash
./sqlite-server serve --log-level debug --no-auth --database :memory:
```

Debug log lines include:
- Received message type and length
- SQL string before and after translation
- Catalog intercept hits
- Writer goroutine enqueue/dequeue events

### Inspect translated SQL

Add a temporary `fmt.Println` in `sql/planner/rewriter.go`:

```go
func (p *Planner) Rewrite(sql string) (string, error) {
    result, err := p.rewrite(sql)
    fmt.Printf("[planner] %q → %q\n", sql, result)  // temporary debug
    return result, err
}
```

### Trace wire protocol bytes

Use `tcpdump` or Wireshark on `lo` (loopback) to inspect the raw bytes:

```bash
# Linux — capture to file
sudo tcpdump -i lo -w /tmp/pg.pcap port 5432

# Open in Wireshark with PostgreSQL protocol dissector
wireshark /tmp/pg.pcap
```

### Unit test a single planner transformation

```go
package main

import (
    "fmt"
    "github.com/sqlite-server/sqlite-server/sql/planner"
)

func main() {
    p := planner.New()
    out, err := p.Rewrite("SELECT NOW()::DATE")
    fmt.Println(out, err)
}
```

```bash
go run ./cmd/debug/  # or paste into a test
```

---

## Pull Request Checklist

Before opening a PR:

- [ ] `go fmt ./...` — no formatting changes
- [ ] `go vet ./cmd/... ./internal/... ./sql/... ./compat/... ./tests/...` — no warnings
- [ ] `go test ./tests/unit/... -race -cover` — all tests pass
- [ ] New feature has unit tests
- [ ] New catalog query has a test
- [ ] Doc comment added for new exported symbols
- [ ] `CONTRIBUTING.md` updated if the contribution process changed
- [ ] `docs/06-sql-compatibility.md` updated if SQL compatibility changed
- [ ] `go mod tidy` run if dependencies changed

---

## Module Management

```bash
# Add a new dependency
go get github.com/some/package@v1.2.3
go mod tidy

# Update all dependencies
go get -u ./...
go mod tidy

# Verify checksums
go mod verify

# View dependency graph
go mod graph | head -30

# Remove unused dependency
go mod tidy   # automatically removes unused deps
```

---

## Release Process

1. Update the version string in `cmd/sqlite-server/main.go`:

   ```go
   var version = "1.2.0"
   ```

2. Tag the commit:

   ```bash
   git tag -a v1.2.0 -m "Release v1.2.0"
   git push origin v1.2.0
   ```

3. Build release binaries:

   ```bash
   make build-all
   # Produces: dist/sqlite-server-linux-amd64
   #           dist/sqlite-server-darwin-amd64
   #           dist/sqlite-server-darwin-arm64
   #           dist/sqlite-server-windows-amd64.exe
   ```

4. Create a GitHub release and upload the binaries from `dist/`.

5. Update `docs/02-installation-and-build.md` with the new version number if
   any installation instructions changed.
