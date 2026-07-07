# sqlite-server

Expose a SQLite database over the **PostgreSQL wire protocol (v3)**.  
Any PostgreSQL-compatible client — `psql`, DBeaver, pgAdmin, Npgsql, psycopg2, GORM, Hibernate, Entity Framework — connects without modification while all data lives in a single `.db` file on disk.

---


<img src="assets/images/sqlite-server.jpg" width="1024" height="636">




## Table of Contents

- [Features](#features)
- [Quick Start](#quick-start)
- [Installation](#installation)
- [CLI Reference](#cli-reference)
- [SQL Compatibility](#sql-compatibility)
- [Architecture](#architecture)
- [Configuration Examples](#configuration-examples)
- [DBeaver / Client Setup](#dbeaver--client-setup)
- [Limitations](#limitations)
- [License](#license)

---

## Features

| Feature | Details |
|---------|---------|
| **CGO-free single binary** | Pure Go, CGO-free SQLite via `modernc.org/sqlite` |
| **PostgreSQL Wire Protocol v3 compatible** | Startup, auth, simple query, extended query (prepared statements) |
| **SQL translation** | Automatic translation for common PostgreSQL syntax: `SERIAL`, `BOOLEAN`, `ILIKE`, `EXTRACT`, `::` casts, `$N` params, `NOW()`, `RETURNING` |
| **Virtual catalog** | Common `information_schem`a and `pg_catalog` views queries answered in-process |
| **WAL mode** | Single writer, many concurrent readers |
| **TLS** | Optional TLS via `--ssl-cert` / `--ssl-key` |
| **Graceful shutdown** | SIGINT / SIGTERM drains in-flight queries |

---

## Quick Start

```bash
# 1. Build
go build -o sqlite-server ./cmd/sqlite-server

# 2. Start (no auth, WAL mode)
./sqlite-server --no-auth -- myapp.db

# 3. Connect with psql
psql -h localhost -p 5432 -U any -d any
```

```
sqlite-server dev
  database  : myapp.db
  listen    : 0.0.0.0:5432
  max-conn  : 100
  wal-mode  : true
  auth      : false

Ready. Accepting connections on 0.0.0.0:5432
```

---

## Installation

### From source (requires Go 1.22+)

```bash
git clone https://github.com/Brah-Timo/sqlite-server
cd sqlite-server
go build -o sqlite-server ./cmd/sqlite-server
```

### Build with version tag

```bash
go build -ldflags "-X main.Version=$(git describe --tags --always)" \
  -o sqlite-server ./cmd/sqlite-server
```

---

## CLI Reference

```
sqlite-server [flags] <database.db>
```

| Flag | Default | Description |
|------|---------|-------------|
| `--addr` | `0.0.0.0:5432` | TCP listen address (`host:port`) |
| `--max-conn` | `100` | Maximum concurrent client connections |
| `--wal` | `true` | Enable WAL journal mode |
| `--no-auth` | `false` | Disable password auth (dev mode) |
| `--ssl-cert` | _(empty)_ | Path to TLS certificate PEM |
| `--ssl-key` | _(empty)_ | Path to TLS private key PEM |
| `--busy-timeout` | `5s` | SQLite busy_timeout duration |
| `--log-level` | `info` | Log verbosity: `debug\|info\|warn\|error` |
| `--read-only` | `false` | Open database in read-only mode |

### Sub-commands

```bash
sqlite-server version   # print version string
sqlite-server --help    # full help
```

### Examples

```bash
# Development — no auth, custom port
./sqlite-server --addr 127.0.0.1:5433 --no-auth -- dev.db

# Production — auth enabled, TLS, read-write
./sqlite-server --addr 0.0.0.0:5432 \
  --ssl-cert /etc/ssl/certs/server.crt \
  --ssl-key  /etc/ssl/private/server.key \
  -- /var/data/production.db

# Read-only replica
./sqlite-server --read-only --addr 0.0.0.0:5434 -- replica.db

# Disable WAL (single-writer workloads)
./sqlite-server --wal=false --no-auth -- simple.db
```

---

## SQL Compatibility

### Automatically Translated

| PostgreSQL syntax | SQLite equivalent |
|-------------------|-------------------|
| `SERIAL` / `BIGSERIAL` | `INTEGER PRIMARY KEY AUTOINCREMENT` |
| `BOOLEAN` type | `INTEGER` (0/1) |
| `ILIKE` | `LIKE` (case-insensitive) |
| `EXTRACT(YEAR FROM col)` | `STRFTIME('%Y', col)` |
| `col::INTEGER` | `CAST(col AS INTEGER)` |
| `$1, $2, …` params | `?, ?, …` |
| `NOW()` | `DATETIME('now')` |
| `CURRENT_TIMESTAMP` | `DATETIME('now')` |
| `SET client_encoding = …` | no-op (accepted) |
| `SHOW server_version` | virtual result |

### Virtual Catalog (DBeaver-compatible)

These tables/views are answered in-process without touching SQLite:

- `information_schema.tables`
- `information_schema.columns`
- `pg_catalog.pg_tables`
- `pg_catalog.pg_namespace`
- `pg_catalog.pg_class`
- `pg_catalog.pg_attribute`
- `pg_catalog.pg_type`
- `pg_catalog.pg_database`
- `SELECT version()`
- `SELECT current_database()`
- `SELECT current_schema()`
- `SELECT pg_backend_pid()`

### Supported Statement Types

`SELECT`, `INSERT`, `UPDATE`, `DELETE`, `CREATE TABLE/INDEX/VIEW`, `DROP`, `ALTER`, `BEGIN`, `COMMIT`, `ROLLBACK`, `SAVEPOINT`, `RELEASE SAVEPOINT`, `EXPLAIN`, `PRAGMA`, `WITH` (CTE), `RETURNING`

---

## Architecture

```
Client (psql / DBeaver / GORM)
        │  PostgreSQL Wire Protocol v3 (TCP)
        ▼
┌─────────────────────────────────────────────┐
│  internal/wire                              │
│  ├── startup.go      (handshake, auth)      │
│  ├── session.go      (per-connection loop)  │
│  ├── simple_query.go (Simple Query)         │
│  ├── extended_query.go (Parse/Bind/Execute) │
│  └── messages.go     (RowDescription, etc.) │
└────────────────┬────────────────────────────┘
                 │
┌────────────────▼────────────────────────────┐
│  internal/pool                              │
│  ├── ConnPool        (connection lifecycle) │
│  └── writerScheduler (single-writer WAL)    │
└────────────────┬────────────────────────────┘
                 │
┌────────────────▼────────────────────────────┐
│  internal/engine                            │
│  ├── Executor.Execute()                     │
│  └── Rewrite() → sql/planner               │
└──────┬─────────────────────┬───────────────┘
       │                     │
┌──────▼──────┐   ┌──────────▼─────────────────┐
│ sql/planner │   │ internal/catalog            │
│ ├── Lexer   │   │ VirtualCatalog.Handle()     │
│ ├── Parser  │   │ (pg_catalog, info_schema)   │
│ ├── AST     │   └────────────────────────────┘
│ └── Rewriter│
└──────┬──────┘
       │
┌──────▼──────────────────────────────────────┐
│  modernc.org/sqlite  (CGO-free driver)      │
│  SQLite 3.x database file on disk           │
└─────────────────────────────────────────────┘

Shared types (no import cycles):
  internal/pgproto  →  ColumnDesc, QueryResult, OID constants
```

### Key Design Decisions

1. **No import cycles** — `internal/pgproto` is a leaf package imported by `wire`, `engine`, `catalog`, and `pool` but importing nothing internal itself.
2. **Single writer** — `writerScheduler` serialises all write transactions; readers run concurrently in WAL mode.
3. **CGO-free** — `modernc.org/sqlite` transpiles the SQLite C source to Go, so the binary has zero C dependencies and cross-compiles cleanly.
4. **Type aliases** — `wire/types.go` re-exports pgproto types as aliases so existing wire code keeps short names.

---

## Configuration Examples

See [`configs/`](configs/) for ready-to-use configuration templates:

- `configs/dev.yaml` — local development, no auth
- `configs/production.yaml` — TLS, auth, tuned settings
- `configs/docker-compose.yml` — containerised deployment

---

## DBeaver / Client Setup

### DBeaver

1. **New Connection** → choose **PostgreSQL**
2. Host: `localhost`, Port: `5432`
3. Database: _(any string, e.g. `main`)_
4. Username / Password: _(any, or leave blank if `--no-auth`)_
5. Test Connection → ✅

### psql

```bash
psql -h localhost -p 5432 -U test -d test
# with --no-auth, password is ignored
```

### Python (psycopg2)

```python
import psycopg2
conn = psycopg2.connect(
    host="localhost", port=5432,
    user="test", password="test", dbname="test"
)
```

### Go (pgx)

```go
conn, err := pgx.Connect(ctx,
    "postgresql://test:test@localhost:5432/test")
```

### Node.js (pg)

```javascript
const { Pool } = require('pg')
const pool = new Pool({ host: 'localhost', port: 5432,
  user: 'test', password: 'test', database: 'test' })
```

---

## Limitations

| Limitation | Notes |
|------------|-------|
| **Single file** | All data in one `.db` file; no multi-database queries |
| **No replication** | SQLite WAL is local only |
| **No LISTEN/NOTIFY** | PG pub/sub not supported |
| **No stored procedures** | `CREATE FUNCTION` / `CREATE PROCEDURE` not supported |
| **No partial indexes with predicates** | SQLite limitation |
| **Auth is simple** | Cleartext password over the wire (use TLS in production) |
| **No `pg_dump` restore** | `pg_dump` DDL may use unsupported PG syntax |

---

## License

MIT — see [LICENSE](LICENSE).
