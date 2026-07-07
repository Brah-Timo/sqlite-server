# Overview — What sqlite-server Is and How It Works

---

## What Is sqlite-server?

**sqlite-server** is a Go server that exposes a SQLite database file over the
**PostgreSQL Wire Protocol v3**.  Any client, library, or ORM that speaks to
PostgreSQL can connect to sqlite-server without any modification, while all data
lives in a single `.db` file on disk.

```
DBeaver / psql / pgAdmin / GORM / Hibernate / psycopg2
                    │
                    │  PostgreSQL Wire Protocol v3  (TCP/IP)
                    │
           ┌────────▼────────────────────────────────────┐
           │              sqlite-server                   │
           │  • Receives queries in PostgreSQL dialect    │
           │  • Rewrites them to SQLite SQL               │
           │  • Returns results in PostgreSQL wire format │
           └────────────────────┬────────────────────────┘
                                │
                        ┌───────▼────────┐
                        │  myapp.db      │
                        │  SQLite 3.x    │
                        └────────────────┘
```

---

## Why sqlite-server?

| Feature | Details |
|---------|---------|
| **Zero CGO** | Uses `modernc.org/sqlite`, which transpiles SQLite C to Go — no C toolchain needed |
| **Single file** | All data in one `.db` file — trivial to back up, copy, or move |
| **Full protocol** | Startup + Auth + Simple Query + Extended Query (prepared statements) |
| **SQL translation** | PostgreSQL → SQLite automatically: `SERIAL`, `ILIKE`, `EXTRACT`, `::` casts, `$N` params, `NOW()`, `RETURNING` |
| **Virtual catalog** | `information_schema` and `pg_catalog` work out of the box |
| **WAL mode** | Single writer + many concurrent readers |
| **TLS** | Optional TLS via `--ssl-cert` / `--ssl-key` |
| **Graceful shutdown** | SIGINT / SIGTERM drains in-flight queries before exiting |

---

## Connection Lifecycle — Step by Step

When a PostgreSQL client connects to sqlite-server, the following phases occur:

### Phase 1 — Startup Handshake

```
Client                              Server
  │                                   │
  │──── 4 bytes  (length) ───────────▶│
  │──── 4 bytes  (version = 196608) ──▶│  ← 3.0 = (3 << 16) | 0
  │──── key\0value\0 ... \0\0 ────────▶│  ← user=test, database=mydb, ...
  │                                   │
  │◀─── AuthenticationCleartextPassword ('R') ──│  if --no-auth is false
  │──── Password ('p') ───────────────▶│
  │◀─── AuthenticationOk ('R') ────────│
  │◀─── ParameterStatus ('S') × N ─────│  server_version=14.5, DateStyle=ISO ...
  │◀─── BackendKeyData ('K') ──────────│  PID + SecretKey
  │◀─── ReadyForQuery ('Z') ───────────│  TxStatus = 'I'
```

### Phase 2 — Simple Query Protocol

```
Client                              Server
  │                                   │
  │──── Query ('Q') ──────────────────▶│  "SELECT * FROM users"
  │                                   │
  │◀─── RowDescription ('T') ──────────│  column names + types
  │◀─── DataRow ('D') × N ─────────────│  one message per row
  │◀─── CommandComplete ('C') ─────────│  "SELECT 5"
  │◀─── ReadyForQuery ('Z') ───────────│
```

### Phase 3 — Extended Query Protocol (Prepared Statements)

```
Client                              Server
  │                                   │
  │──── Parse ('P') ──────────────────▶│  name="" sql="SELECT * FROM t WHERE id=$1"
  │──── Bind ('B') ───────────────────▶│  portal="" params=[42]
  │──── Describe ('D') ───────────────▶│  'P' (portal)
  │──── Execute ('E') ────────────────▶│  portal="" maxRows=0
  │──── Sync ('S') ───────────────────▶│
  │                                   │
  │◀─── ParseComplete ('1') ───────────│
  │◀─── BindComplete ('2') ────────────│
  │◀─── RowDescription ('T') ──────────│
  │◀─── DataRow ('D') × N ─────────────│
  │◀─── CommandComplete ('C') ─────────│
  │◀─── ReadyForQuery ('Z') ───────────│
```

---

## Project Layout

```
sqlite-server/
├── cmd/
│   └── sqlite-server/
│       └── main.go                ← CLI entry point (cobra)
│
├── internal/
│   ├── pgproto/
│   │   └── types.go               ← leaf package — imports nothing internal
│   ├── wire/
│   │   ├── server.go              ← TCP listener + goroutine dispatcher
│   │   ├── session.go             ← per-connection state + command loop
│   │   ├── startup.go             ← handshake + authentication
│   │   ├── auth.go                ← password authentication
│   │   ├── simple_query.go        ← Simple Query protocol
│   │   ├── extended_query.go      ← Parse / Bind / Describe / Execute / Sync
│   │   ├── messages.go            ← RowDescription, DataRow, CommandComplete
│   │   ├── error.go               ← ErrorResponse
│   │   ├── ready.go               ← ReadyForQuery
│   │   └── types.go               ← type aliases → pgproto
│   ├── pool/
│   │   └── connpool.go            ← SQLite connection pool + WAL write scheduler
│   ├── engine/
│   │   ├── executor.go            ← SQL executor (ties planner + catalog + SQLite)
│   │   ├── translator.go          ← translation helpers
│   │   └── optimizer.go           ← query optimizer
│   ├── catalog/
│   │   └── catalog.go             ← virtual pg_catalog + information_schema
│   └── errors/
│       ├── pgerrors.go            ← PGError type
│       └── sqlstate.go            ← SQLSTATE constants
│
├── sql/
│   ├── lexer/
│   │   ├── token.go               ← token types (keywords, literals)
│   │   └── lexer.go               ← SQL tokenizer
│   ├── ast/
│   │   └── ast.go                 ← typed AST node definitions
│   ├── parser/
│   │   └── parser.go              ← PostgreSQL grammar parser
│   └── planner/
│       ├── planner.go             ← entry point: Rewrite(pgSQL) → sqliteSQL
│       ├── rewriter.go            ← rewrite rules
│       └── normalizer.go          ← AST normalization
│
├── compat/
│   └── postgres/
│       ├── functions.go           ← function compatibility tables
│       ├── types.go               ← type compatibility tables
│       └── operators.go           ← operator compatibility tables
│
├── tests/
│   ├── unit/
│   │   ├── translator_test.go     ← offline planner tests
│   │   └── messages_test.go       ← pgproto OID / type tests
│   └── integration/
│       └── crud_test.go           ← full end-to-end tests via lib/pq
│
├── configs/
│   ├── dev.yaml
│   ├── production.yaml
│   └── docker-compose.yml
│
├── Makefile
├── Dockerfile
├── go.mod
├── go.sum
├── README.md
└── CONTRIBUTING.md
```

---

## Full Data-Flow Diagram

```
Client request: INSERT INTO users VALUES($1, $2)
                           │
                     internal/wire
                     session.go → commandLoop()
                           │
               ┌───────────▼────────────┐
               │  Simple Query  ('Q')   │
               │  or  Parse     ('P')   │
               └───────────┬────────────┘
                           │
                     internal/pool
                     ConnPool.Execute()
                           │
               ┌───────────▼────────────┐
               │    internal/catalog    │
               │  Is this a pg_catalog  │
               │  or information_schema │
               │  query?                │
               └──────┬─────────┬───────┘
                    YES         NO
                     │          │
             return  │    internal/engine
             virtual │    executor.go
             result  │          │
                          sql/planner
                          Planner.Rewrite()
                               │
                          PostgreSQL → SQLite
                          SERIAL → INTEGER
                          $1     → ?
                          ILIKE  → LIKE
                               │
                          modernc.org/sqlite
                          actual .db file
                               │
                          pgproto.QueryResult
                               │
                     internal/wire
                     messages.go
                     RowDescription + DataRow
                               │
                     TCP ──────▶ Client
```

---

## Import Graph (Simplified)

```
cmd/sqlite-server
      │
      ├── internal/wire  ────────────────────────────┐
      │        │                                     │
      │        └── internal/pool ──────────────────► │
      │                  │                           │
      │                  └── internal/engine ──────► │
      │                            │                 │
      │                            ├── internal/catalog ──► internal/pgproto
      │                            │                 │
      │                            └── sql/planner   │
      │                                              │
      └── (all packages above) ──► internal/pgproto ◄┘
                                        (leaf — imports only stdlib)
```

The critical rule: **`internal/pgproto` imports nothing from this module**.
Every other internal package may import `pgproto`, breaking the cycle that
existed before it was introduced.
