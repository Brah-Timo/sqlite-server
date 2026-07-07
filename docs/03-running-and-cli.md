# Running sqlite-server & CLI Reference

This document covers every way to start, configure, and stop **sqlite-server**, including
a full flag reference, environment variables, shell examples (Bash and PowerShell), and
connecting from common clients.

---

## Table of Contents

1. [Quick Start](#quick-start)
2. [CLI Flags](#cli-flags)
3. [Environment Variables](#environment-variables)
4. [Config File](#config-file)
5. [Running Examples](#running-examples)
   - [Bash / Linux / macOS](#bash--linux--macos)
   - [PowerShell / Windows](#powershell--windows)
6. [Connecting from Clients](#connecting-from-clients)
   - [psql](#psql)
   - [Python (psycopg2)](#python-psycopg2)
   - [Node.js (pg)](#nodejs-pg)
   - [Go (lib/pq)](#go-libpq)
   - [DBeaver](#dbeaver)
7. [Process Signals](#process-signals)
8. [Graceful Shutdown](#graceful-shutdown)

---

## Quick Start

```bash
# Build the binary (one-time)
go build -o sqlite-server ./cmd/sqlite-server/

# Start with an in-memory database (development)
./sqlite-server serve --no-auth

# Start with a persistent database file
./sqlite-server serve --database myapp.db --addr 127.0.0.1:5432
```

---

## CLI Flags

### Global flags

| Flag | Default | Description |
|------|---------|-------------|
| `--help`, `-h` | — | Show help text |
| `--version`, `-v` | — | Print version string and exit |

### `serve` subcommand

```
sqlite-server serve [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--addr` | string | `127.0.0.1:5432` | TCP address to listen on (`host:port`) |
| `--database` | string | `sqlite.db` | Path to the SQLite database file. Use `:memory:` for in-memory. |
| `--max-conn` | int | `100` | Maximum number of simultaneous client connections |
| `--wal` | bool | `true` | Enable SQLite WAL journal mode (improves concurrency) |
| `--no-auth` | bool | `false` | Disable password authentication (trust all connections) |
| `--ssl-cert` | string | `""` | Path to TLS certificate file (PEM) |
| `--ssl-key` | string | `""` | Path to TLS private key file (PEM) |
| `--log-level` | string | `info` | Log verbosity: `debug`, `info`, `warn`, `error` |
| `--config` | string | `""` | Path to a YAML config file (overrides defaults) |

### `version` subcommand

```
sqlite-server version
```

Prints the build version and exits with code 0. Useful for smoke-testing a deployment.

### `help` subcommand

```
sqlite-server help [command]
```

Prints help for any subcommand.

---

## Environment Variables

All CLI flags can be set via environment variables using the prefix `SQLITE_SERVER_` and
uppercased flag name with hyphens replaced by underscores:

| Environment Variable | Equivalent Flag |
|----------------------|-----------------|
| `SQLITE_SERVER_ADDR` | `--addr` |
| `SQLITE_SERVER_DATABASE` | `--database` |
| `SQLITE_SERVER_MAX_CONN` | `--max-conn` |
| `SQLITE_SERVER_WAL` | `--wal` |
| `SQLITE_SERVER_NO_AUTH` | `--no-auth` |
| `SQLITE_SERVER_SSL_CERT` | `--ssl-cert` |
| `SQLITE_SERVER_SSL_KEY` | `--ssl-key` |
| `SQLITE_SERVER_LOG_LEVEL` | `--log-level` |
| `SQLITE_SERVER_CONFIG` | `--config` |

**Priority order** (highest to lowest):
1. CLI flag
2. Environment variable
3. Config file value
4. Built-in default

---

## Config File

Pass `--config path/to/config.yaml` to load settings from a YAML file.

```yaml
# configs/dev.yaml
addr: "127.0.0.1:5432"
database: "dev.db"
max-conn: 20
wal: true
no-auth: true
log-level: "debug"
```

```yaml
# configs/production.yaml
addr: "0.0.0.0:5432"
database: "/var/lib/sqlite-server/prod.db"
max-conn: 100
wal: true
ssl-cert: "/etc/ssl/certs/server.crt"
ssl-key: "/etc/ssl/private/server.key"
log-level: "info"
```

Start with a config file:

```bash
./sqlite-server serve --config configs/production.yaml
```

---

## Running Examples

### Bash / Linux / macOS

```bash
# ── Development ──────────────────────────────────────────────────────────────

# Minimal: in-memory DB, trust all connections, default port 5432
./sqlite-server serve --no-auth --database :memory:

# Persistent file, debug logging, custom port
./sqlite-server serve \
  --database ./myapp.db \
  --addr 127.0.0.1:5433 \
  --log-level debug \
  --no-auth

# Using a config file
./sqlite-server serve --config configs/dev.yaml

# ── Production ───────────────────────────────────────────────────────────────

# All interfaces, TLS, production DB
./sqlite-server serve \
  --addr 0.0.0.0:5432 \
  --database /var/lib/sqlite-server/prod.db \
  --max-conn 100 \
  --wal \
  --ssl-cert /etc/ssl/certs/server.crt \
  --ssl-key  /etc/ssl/private/server.key \
  --log-level info

# Via environment variables
export SQLITE_SERVER_DATABASE=/var/lib/sqlite-server/prod.db
export SQLITE_SERVER_ADDR=0.0.0.0:5432
export SQLITE_SERVER_LOG_LEVEL=info
./sqlite-server serve

# ── Background / systemd ─────────────────────────────────────────────────────

# Run in background, redirect logs
./sqlite-server serve --config configs/production.yaml \
  >> /var/log/sqlite-server.log 2>&1 &

echo "PID: $!"          # save PID for later
```

### PowerShell / Windows

```powershell
# Build (from project root)
go build -o sqlite-server.exe .\cmd\sqlite-server\

# Development — trust all, in-memory
.\sqlite-server.exe serve --no-auth --database :memory:

# Persistent file, custom port
.\sqlite-server.exe serve `
  --database C:\data\myapp.db `
  --addr 127.0.0.1:5433 `
  --log-level debug `
  --no-auth

# Using a config file
.\sqlite-server.exe serve --config .\configs\dev.yaml

# Production with TLS
.\sqlite-server.exe serve `
  --addr 0.0.0.0:5432 `
  --database C:\data\prod.db `
  --max-conn 100 `
  --ssl-cert C:\ssl\server.crt `
  --ssl-key  C:\ssl\server.key `
  --log-level info

# Via environment variables
$env:SQLITE_SERVER_DATABASE = "C:\data\prod.db"
$env:SQLITE_SERVER_ADDR     = "0.0.0.0:5432"
.\sqlite-server.exe serve

# Run as a background job
$job = Start-Job -ScriptBlock {
    & "C:\bin\sqlite-server.exe" serve --config C:\configs\production.yaml
}
Write-Host "Job ID: $($job.Id)"
```

---

## Connecting from Clients

When connecting from any PostgreSQL-compatible client:

| Setting | Value |
|---------|-------|
| Host | `127.0.0.1` (or your server IP) |
| Port | `5432` (or `--addr` port) |
| Database | any string (maps to the single SQLite file) |
| User | any string (ignored when `--no-auth`) |
| Password | any string (ignored when `--no-auth`) |
| SSL mode | `disable` (unless TLS flags are set) |

### psql

```bash
# No auth (development)
psql -h 127.0.0.1 -p 5432 -U anyuser -d anydb

# With password prompt
psql -h 127.0.0.1 -p 5432 -U myuser -d mydb -W

# One-liner query
psql -h 127.0.0.1 -p 5432 -U anyuser -d anydb \
     -c "SELECT version();"

# Connection string form
psql "host=127.0.0.1 port=5432 user=anyuser dbname=anydb sslmode=disable"
```

### Python (psycopg2)

```python
import psycopg2

conn = psycopg2.connect(
    host="127.0.0.1",
    port=5432,
    dbname="mydb",
    user="anyuser",
    password="anypass",   # ignored when --no-auth
    sslmode="disable",
)
conn.autocommit = True

with conn.cursor() as cur:
    cur.execute("CREATE TABLE IF NOT EXISTS users (id SERIAL, name TEXT)")
    cur.execute("INSERT INTO users (name) VALUES (%s)", ("Alice",))
    cur.execute("SELECT * FROM users")
    print(cur.fetchall())

conn.close()
```

### Node.js (pg)

```javascript
const { Client } = require('pg');

const client = new Client({
  host:     '127.0.0.1',
  port:     5432,
  database: 'mydb',
  user:     'anyuser',
  password: 'anypass',
  ssl:      false,
});

async function main() {
  await client.connect();

  await client.query(
    'CREATE TABLE IF NOT EXISTS events (id SERIAL, name TEXT, ts TIMESTAMP)'
  );
  await client.query(
    'INSERT INTO events (name, ts) VALUES ($1, NOW())',
    ['user_login']
  );

  const { rows } = await client.query('SELECT * FROM events');
  console.log(rows);

  await client.end();
}

main().catch(console.error);
```

### Go (lib/pq)

```go
package main

import (
    "database/sql"
    "fmt"
    "log"

    _ "github.com/lib/pq"
)

func main() {
    dsn := "host=127.0.0.1 port=5432 user=anyuser dbname=mydb sslmode=disable"
    db, err := sql.Open("postgres", dsn)
    if err != nil {
        log.Fatal(err)
    }
    defer db.Close()

    if err := db.Ping(); err != nil {
        log.Fatal("ping:", err)
    }

    _, err = db.Exec(`CREATE TABLE IF NOT EXISTS items (
        id   SERIAL PRIMARY KEY,
        name TEXT NOT NULL
    )`)
    if err != nil {
        log.Fatal(err)
    }

    var id int
    err = db.QueryRow(
        "INSERT INTO items (name) VALUES ($1) RETURNING id", "widget",
    ).Scan(&id)
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println("inserted id:", id)

    rows, err := db.Query("SELECT id, name FROM items")
    if err != nil {
        log.Fatal(err)
    }
    defer rows.Close()

    for rows.Next() {
        var id int
        var name string
        rows.Scan(&id, &name)
        fmt.Printf("  %d  %s\n", id, name)
    }
}
```

### DBeaver

1. Open DBeaver → **New Database Connection** → choose **PostgreSQL**.
2. Fill in the connection form:

   | Field | Value |
   |-------|-------|
   | Host | `127.0.0.1` |
   | Port | `5432` |
   | Database | `mydb` (any string) |
   | User | `anyuser` |
   | Password | *(leave blank or any value)* |

3. On the **SSL** tab → set **SSL mode** to `disable`.
4. Click **Test Connection** — a success dialog confirms the server is reachable.
5. Browse tables, run SQL, inspect schema via the **Database Navigator** panel.

> **Note**: DBeaver sends several `pg_catalog` and `information_schema` queries on
> startup. sqlite-server's virtual catalog intercepts all of these, so schema browsing
> works out of the box.

---

## Process Signals

| Signal | Effect |
|--------|--------|
| `SIGINT` (Ctrl+C) | Graceful shutdown: stop accepting new connections, drain active ones |
| `SIGTERM` | Same as `SIGINT` — used by systemd/Docker stop |
| `SIGHUP` | Log rotation / config reload (planned; currently behaves as SIGTERM) |
| `SIGKILL` | Immediate kill — SQLite WAL ensures DB is not corrupted |

---

## Graceful Shutdown

When sqlite-server receives `SIGINT` or `SIGTERM`:

1. The listener stops accepting new TCP connections.
2. Existing sessions are allowed to finish their current query.
3. The writer goroutine flushes any pending writes and closes the SQLite file.
4. The process exits with code `0`.

The shutdown window is **30 seconds** by default. If sessions do not finish within
that window, they are forcibly closed and the process exits with code `1`.

```bash
# Send graceful shutdown to background process
kill -SIGTERM $(pgrep sqlite-server)

# Or by saved PID
kill -SIGTERM $SERVER_PID
```

On Windows, Ctrl+C sends the equivalent interrupt and triggers the same shutdown path.
