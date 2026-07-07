# Architecture Deep Dive

This document explains the internal design of sqlite-server: how each package works,
how they interconnect, the concurrency model, and the full request lifecycle from
TCP bytes to SQLite rows and back.

---

## Table of Contents

1. [High-Level Architecture](#high-level-architecture)
2. [Package Map](#package-map)
3. [Import Graph](#import-graph)
4. [Layer-by-Layer Breakdown](#layer-by-layer-breakdown)
   - [Transport Layer — `internal/wire`](#transport-layer--internalwire)
   - [Shared Types — `internal/pgproto`](#shared-types--internalpgproto)
   - [Connection Pool — `internal/pool`](#connection-pool--internalpool)
   - [Engine — `internal/engine`](#engine--internalengine)
   - [Virtual Catalog — `internal/catalog`](#virtual-catalog--internalcatalog)
   - [SQL Pipeline — `sql/`](#sql-pipeline--sql)
   - [Compatibility Layer — `compat/postgres`](#compatibility-layer--compatpostgres)
   - [Error Handling — `internal/errors`](#error-handling--internalerrors)
5. [Concurrency Model](#concurrency-model)
   - [Writer Scheduler](#writer-scheduler)
   - [Reader Concurrency](#reader-concurrency)
6. [PostgreSQL Wire Protocol v3 Implementation](#postgresql-wire-protocol-v3-implementation)
   - [Startup / Authentication](#startup--authentication)
   - [Simple Query Flow](#simple-query-flow)
   - [Extended Query Flow](#extended-query-flow)
7. [SQL Translation Pipeline](#sql-translation-pipeline)
8. [Virtual Catalog Design](#virtual-catalog-design)
9. [Data Flow Diagram](#data-flow-diagram)

---

## High-Level Architecture

```
┌──────────────────────────────────────────────────────────┐
│                    Client (psql / app)                    │
└───────────────────────┬──────────────────────────────────┘
                        │  TCP  (PostgreSQL Wire Protocol v3)
┌───────────────────────▼──────────────────────────────────┐
│                  internal/wire                            │
│  Listener → Session → SimpleQuery / ExtendedQuery        │
└───────────────────────┬──────────────────────────────────┘
                        │
┌───────────────────────▼──────────────────────────────────┐
│                  internal/pool                            │
│  ConnPool → Writer Scheduler (single goroutine)          │
└───────────┬───────────────────────────┬──────────────────┘
            │                           │
┌───────────▼──────────┐   ┌────────────▼─────────────────┐
│  internal/catalog    │   │      internal/engine          │
│  Virtual pg_catalog  │   │  Executor → SQL Pipeline      │
│  & info_schema       │   │  → modernc.org/sqlite         │
└──────────────────────┘   └──────────────────────────────┘
```

---

## Package Map

```
sqlite-server/
├── cmd/sqlite-server/       # Binary entry point (cobra CLI)
├── internal/
│   ├── pgproto/             # ← Leaf package: shared types & OID constants
│   ├── wire/                # PostgreSQL wire protocol (uses pgproto)
│   ├── pool/                # Connection pool + writer scheduler
│   ├── engine/              # SQL executor (uses pgproto, sql/, compat/)
│   ├── catalog/             # Virtual pg_catalog / information_schema
│   └── errors/              # PGError + SQLSTATE codes
├── sql/
│   ├── lexer/               # Tokenizer (keywords, literals, operators)
│   ├── ast/                 # AST node types (Stmt, Expr, RawExpr …)
│   ├── parser/              # Recursive-descent parser → AST
│   └── planner/             # Rewriter + Normalizer (PG SQL → SQLite SQL)
├── compat/postgres/         # Functions, types, operators compatibility
├── tests/
│   ├── unit/                # Unit tests (no server needed)
│   └── integration/         # End-to-end tests (requires live server)
└── configs/                 # Example YAML config files
```

---

## Import Graph

The most important design constraint is **no import cycles**. The graph is strictly
layered:

```
cmd/sqlite-server
    └── internal/wire
    └── internal/pool
            └── internal/engine
                    └── internal/catalog
                    │       └── internal/pgproto  ← LEAF (stdlib only)
                    └── sql/planner
                    │       └── sql/parser
                    │               └── sql/ast
                    │               └── sql/lexer
                    └── compat/postgres
                    └── internal/errors
                    └── internal/pgproto          ← LEAF
```

`internal/pgproto` imports **only** Go stdlib (`strings`, `fmt`). Everything else
imports it. This is what breaks the cycle that would otherwise occur between `wire`
and `engine`.

---

## Layer-by-Layer Breakdown

### Transport Layer — `internal/wire`

**Files**: `server.go`, `session.go`, `startup.go`, `auth.go`, `simple_query.go`,
`extended_query.go`, `messages.go`, `types.go`

Responsibilities:
- `server.go` — `net.Listen` TCP, accept loops, pass connections to `pool`
- `startup.go` — read the PostgreSQL startup packet, send `ParameterStatus` + `BackendKeyData` + `ReadyForQuery`
- `auth.go` — MD5 or trust authentication
- `session.go` — per-connection state machine; dispatches on message type byte
- `simple_query.go` — handle `'Q'` (Query) messages: parse SQL, call engine, send `RowDescription` + `DataRow` + `CommandComplete`
- `extended_query.go` — handle `'P'` (Parse) / `'B'` (Bind) / `'E'` (Execute) / `'S'` (Sync) pipeline
- `messages.go` — low-level helpers: `sendRowDescription`, `sendDataRow`, `sendCommandComplete`, `sendErrorResponse`
- `types.go` — thin type aliases re-exporting `pgproto.ColumnDesc`, `pgproto.QueryResult`, and all OID constants

`types.go` uses **type aliases** (`type ColumnDesc = pgproto.ColumnDesc`) so that the
rest of `wire` can use short names without re-defining any types.

### Shared Types — `internal/pgproto`

**File**: `types.go`

The **leaf package** — imported by everyone, imports nothing from this repo.

Defines:
- `ColumnDesc` — column metadata sent in `RowDescription`
- `QueryResult` — rows returned from engine to wire layer
- 25+ OID constants (`OIDBool`, `OIDInt4`, `OIDText`, …)
- `SQLiteTypeToOID(s string) (uint32, int16)` — maps SQLite type affinity → PG OID
- `OIDToTypeName(oid uint32) string` — human-readable type name for error messages
- `DecodeParamValue(data []byte, oid uint32, format int16) interface{}` — decodes binary/text bind parameters

### Connection Pool — `internal/pool`

**File**: `connpool.go`

Responsibilities:
- Maintains a pool of SQLite database handles
- Tracks active connection count vs `max-conn`
- Owns the **writer goroutine** (see [Writer Scheduler](#writer-scheduler))
- Exposes `Acquire() / Release()` to `wire/session.go`
- Passes an `*engine.Executor` to each session

Key types:
```go
type ConnPool struct {
    db       *sql.DB           // modernc.org/sqlite handle
    executor *engine.Executor  // pointer — shared across sessions
    writeCh  chan writeRequest  // serialises all mutations
    sem      chan struct{}      // counting semaphore for max-conn
}
```

### Engine — `internal/engine`

**Files**: `executor.go`, `translator.go`, `optimizer.go`

The engine receives a `QueryResult`-shaped request from `wire` and returns a
`pgproto.QueryResult`.

`executor.go`:
1. Detects catalog queries (`pg_catalog.*`, `information_schema.*`) — delegates to `catalog`
2. Otherwise calls `planner.New().Rewrite(sql)` to translate PG SQL → SQLite SQL
3. Runs the translated SQL via `modernc.org/sqlite`
4. Converts column types to OIDs using `pgproto.SQLiteTypeToOID`

`translator.go`: helpers used by the planner for type conversions not covered by the AST rewriter.

`optimizer.go`: lightweight query optimizer (e.g., pushes `LIMIT` past `ORDER BY` when safe).

### Virtual Catalog — `internal/catalog`

**File**: `catalog.go`

sqlite-server intercepts any query that references:
- `pg_catalog.*` (e.g. `pg_type`, `pg_class`, `pg_namespace`, `pg_attribute`)
- `information_schema.*` (e.g. `tables`, `columns`)
- Special functions: `current_database()`, `current_schema()`, `pg_get_userbyid()`

All such queries return **synthetic data** derived from the live SQLite schema (`PRAGMA table_info`, `PRAGMA table_list`). This makes DBeaver, pgAdmin, and other GUI tools work without modification.

The catalog package imports only `internal/pgproto` — no wire, no pool, no engine.

### SQL Pipeline — `sql/`

```
Raw SQL string
     │
     ▼
sql/lexer     → []Token
     │
     ▼
sql/parser    → ast.Statement
     │
     ▼
sql/planner   → rewritten ast.Statement → SQLite SQL string
```

**`sql/lexer`**: hand-written scanner. Produces tokens: keywords (`KW_SELECT`, …),
identifiers, literals, operators, punctuation. Special: `$N` placeholders are
tokenized as `TOK_PLACEHOLDER`.

**`sql/ast`**: pure data types — no logic. All node types satisfy one of:
```go
type Node interface { nodeTag() }
type Stmt interface { Node; stmtTag() }
type Expr interface { Node; exprTag() }
```
`RawExpr` is defined here (same package as `Expr`) because `exprTag()` is unexported.

**`sql/parser`**: recursive-descent parser. Produces a full AST from a token stream.
Handles `SELECT`, `INSERT`, `UPDATE`, `DELETE`, `CREATE TABLE`, `CREATE INDEX`,
`DROP`, `BEGIN`/`COMMIT`/`ROLLBACK`, `SAVEPOINT`, `SET`, `SHOW`, CTEs, subqueries,
JOINs, window functions, and more.

**`sql/planner`**: two passes:
1. **Normalizer** — canonical form (e.g. uppercase keywords, strip redundant parens)
2. **Rewriter** — PG-specific constructs → SQLite equivalents (see [SQL Translation Pipeline](#sql-translation-pipeline))

### Compatibility Layer — `compat/postgres`

**Files**: `functions.go`, `types.go`, `operators.go`

Look-up tables and helpers for PostgreSQL constructs that have no direct SQLite
equivalent:

- `functions.go` — maps `NOW()`, `CLOCK_TIMESTAMP()`, `EXTRACT(...)`, `DATE_TRUNC`, `GENERATE_SERIES`, `PG_SLEEP`, …
- `types.go` — maps `SERIAL`, `BIGSERIAL`, `SMALLSERIAL`, `BOOLEAN`, `BYTEA`, `UUID`, `JSONB`, `TIMESTAMPTZ`, …
- `operators.go` — maps `ILIKE`, `~`, `~*`, `||` (concatenation), `->`, `->>` (JSON operators), …

### Error Handling — `internal/errors`

**Files**: `pgerrors.go`, `sqlstate.go`

`PGError` implements the full PostgreSQL wire error response (`severity`, `code`,
`message`, `detail`, `hint`, `position`).

`TranslateSQLiteError(err error) *PGError` converts modernc.org/sqlite error codes
to the nearest SQLSTATE code so clients get meaningful errors.

---

## Concurrency Model

### Writer Scheduler

SQLite allows only **one writer at a time**. sqlite-server enforces this with a
dedicated **writer goroutine**:

```
Session A (INSERT) ─────┐
                         ▼
Session B (UPDATE) ──► writeCh (buffered channel) ──► writer goroutine ──► SQLite
                         ▲
Session C (DELETE) ─────┘
```

All `INSERT`, `UPDATE`, `DELETE`, `CREATE`, `DROP`, `BEGIN`, `COMMIT`, `ROLLBACK`
messages are sent to `writeCh`. The writer goroutine processes them one at a time,
guaranteeing serialised writes. Results are returned via per-request reply channels.

### Reader Concurrency

`SELECT` queries bypass the writer channel and go directly to SQLite. With WAL mode
enabled, SQLite allows many concurrent readers in parallel with a single writer. The
pool manages a `*sql.DB` with `SetMaxOpenConns` tuned to `max-conn`.

---

## PostgreSQL Wire Protocol v3 Implementation

### Startup / Authentication

```
Client                          Server
  │                               │
  │── StartupMessage ────────────►│  (version 3.0, user, database params)
  │                               │
  │◄── AuthenticationMD5Password ─│  (or AuthenticationOk if --no-auth)
  │                               │
  │── PasswordMessage ───────────►│
  │                               │
  │◄── AuthenticationOk ──────────│
  │◄── ParameterStatus (N) ───────│  (server_version, client_encoding, …)
  │◄── BackendKeyData ────────────│  (pid, secret key)
  │◄── ReadyForQuery ('I') ───────│
```

### Simple Query Flow

Triggered by message type `'Q'`:

```
Client                          Server
  │                               │
  │── Query ('Q') ───────────────►│  "SELECT id, name FROM users"
  │                               │
  │◄── RowDescription ────────────│  (column names + OIDs)
  │◄── DataRow ───────────────────│  (row 1)
  │◄── DataRow ───────────────────│  (row 2)
  │◄── …                          │
  │◄── CommandComplete ───────────│  "SELECT 2"
  │◄── ReadyForQuery ('I') ───────│
```

For non-SELECT statements:

```
Client                          Server
  │── Query ('Q') ───────────────►│  "INSERT INTO users VALUES (1, 'Bob')"
  │◄── CommandComplete ───────────│  "INSERT 0 1"
  │◄── ReadyForQuery ('I') ───────│
```

### Extended Query Flow

Triggered by `'P'` Parse → `'B'` Bind → `'E'` Execute → `'S'` Sync:

```
Client                          Server
  │── Parse ('P') ───────────────►│  name="s1", query="SELECT … WHERE id=$1"
  │◄── ParseComplete ─────────────│
  │                               │
  │── Bind ('B') ────────────────►│  portal="p1", params=[42]
  │◄── BindComplete ──────────────│
  │                               │
  │── Execute ('E') ─────────────►│  portal="p1"
  │◄── DataRow ───────────────────│
  │◄── CommandComplete ───────────│
  │                               │
  │── Sync ('S') ────────────────►│
  │◄── ReadyForQuery ─────────────│
```

Prepared statements are stored in a `map[string]*preparedStmt` per session, keyed
by the statement name.

---

## SQL Translation Pipeline

The planner rewrites PostgreSQL SQL to SQLite-compatible SQL before execution:

| PostgreSQL construct | SQLite equivalent |
|---------------------|-------------------|
| `$1`, `$2` … | `?`, `?` … |
| `value::TYPE` | `CAST(value AS TYPE)` |
| `SERIAL` | `INTEGER` + trigger |
| `BIGSERIAL` | `INTEGER` + trigger |
| `BOOLEAN` literals | `1` / `0` |
| `NOW()` | `datetime('now')` |
| `CURRENT_TIMESTAMP` | `datetime('now')` |
| `EXTRACT(YEAR FROM x)` | `strftime('%Y', x)` |
| `ILIKE` | `LIKE` (SQLite LIKE is case-insensitive by default) |
| `SET TIME ZONE …` | no-op (absorbed) |
| `SHOW search_path` | returns `"public"` |
| `BEGIN WORK` | `BEGIN` |
| `BEGIN TRANSACTION` | `BEGIN` |
| `START TRANSACTION` | `BEGIN` |
| `COMMIT WORK` | `COMMIT` |
| `ROLLBACK WORK` | `ROLLBACK` |

---

## Virtual Catalog Design

The virtual catalog is a query interceptor. Before any query reaches the engine,
`catalog.Handle(sql)` is called. If the query matches a known catalog pattern,
the catalog constructs a `pgproto.QueryResult` from live schema data and returns
it directly — SQLite never sees the query.

**Intercepted patterns** (examples):

```sql
-- DBeaver schema browsing
SELECT typname, oid FROM pg_catalog.pg_type WHERE oid = ANY(...)

-- Table listing
SELECT table_name FROM information_schema.tables
  WHERE table_schema = 'public'

-- Column introspection
SELECT column_name, data_type FROM information_schema.columns
  WHERE table_name = 'users'

-- psql \d
SELECT a.attname, pg_catalog.format_type(a.atttypid, a.atttypmod)
  FROM pg_catalog.pg_attribute a
  WHERE a.attrelid = ...
```

**Schema reflection**: The catalog queries SQLite's `PRAGMA table_list` and
`PRAGMA table_info(name)` to build the synthetic response.

---

## Data Flow Diagram

Complete flow for `SELECT id, name FROM users WHERE id = $1` with parameter `42`:

```
TCP bytes arrive
       │
       ▼
wire/session.go — read message type byte
       │  type = 'P' (Parse) or 'Q' (Query)
       ▼
wire/extended_query.go (or simple_query.go)
       │  extract SQL string, parameter types
       ▼
catalog.Handle(sql)
       │  NOT a catalog query → pass through
       ▼
engine/executor.go
       │
       ├─ planner.New().Rewrite(sql)
       │       │
       │       ├─ lexer.Tokenize(sql)      → []Token
       │       ├─ parser.Parse(tokens)     → ast.SelectStmt
       │       └─ planner.Rewrite(ast)     → "SELECT id, name FROM users WHERE id = ?"
       │
       ├─ pool.Acquire()                   → *sql.DB handle
       │
       ├─ db.QueryContext(ctx, translated, args...)
       │       └─ modernc.org/sqlite executes
       │
       ├─ build pgproto.QueryResult
       │       ├─ Columns: [{Name:"id", TypeOID:23}, {Name:"name", TypeOID:25}]
       │       └─ Rows:    [[42, "Alice"]]
       │
       └─ pool.Release()
              │
              ▼
engine returns QueryResult to wire layer
       │
       ▼
wire/messages.go
       ├─ sendRowDescription(cols)   → RowDescription message bytes
       ├─ sendDataRow(row)           → DataRow message bytes (per row)
       └─ sendCommandComplete("SELECT 1")
              │
              ▼
TCP bytes sent to client
```
