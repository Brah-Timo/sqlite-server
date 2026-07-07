# sqlite-server — Documentation Index

Welcome to the complete documentation for **sqlite-server**, a Go server that exposes a
SQLite database over the PostgreSQL Wire Protocol v3.

---

## Document Index

| # | File | Contents |
|---|------|----------|
| 01 | [01-overview.md](01-overview.md) | What it is, how it works end-to-end, project layout, full data-flow diagrams |
| 02 | [02-installation-and-build.md](02-installation-and-build.md) | Building from source, creating `.exe`, cross-compilation for all platforms |
| 03 | [03-running-and-cli.md](03-running-and-cli.md) | Starting the server, all CLI flags, PowerShell & Bash examples, connecting from various clients |
| 04 | [04-architecture-deep-dive.md](04-architecture-deep-dive.md) | Internals of every package, import-cycle fix, wire protocol implementation details |
| 05 | [05-testing-guide.md](05-testing-guide.md) | Unit tests, integration tests, all commands, CI scripts |
| 06 | [06-sql-compatibility.md](06-sql-compatibility.md) | Auto-translated syntax, compatibility tables, virtual catalog, known limitations |
| 07 | [07-troubleshooting.md](07-troubleshooting.md) | Common errors, error messages explained, FAQ |
| 08 | [08-wire-protocol-reference.md](08-wire-protocol-reference.md) | Full PostgreSQL Wire Protocol v3 reference, message formats, state machines |
| 09 | [09-developer-guide.md](09-developer-guide.md) | Adding new SQL rules, catalog entries, code patterns, contributing |
| 10 | [10-performance-and-production.md](10-performance-and-production.md) | Tuning, Docker / Kubernetes / systemd deployment, backup, security |

---

## Quick Start (3 commands)

```bash
# 1. Build
go build -o sqlite-server ./cmd/sqlite-server

# 2. Start
./sqlite-server --no-auth -- myapp.db

# 3. Connect
psql -h localhost -p 5432 -U test -c "SELECT 1"
```

---

## Build a Windows `.exe`

```powershell
# PowerShell
$env:GOOS="windows"; $env:GOARCH="amd64"; $env:CGO_ENABLED="0"
go build -o sqlite-server.exe .\cmd\sqlite-server
.\sqlite-server.exe version
```

```bash
# From Linux / macOS
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 \
  go build -o sqlite-server.exe ./cmd/sqlite-server
```

---

## Most Important Source Paths

| Path | Purpose |
|------|---------|
| `internal/wire/session.go` | Main command loop for every client connection |
| `internal/wire/startup.go` | PostgreSQL handshake sequence |
| `sql/planner/rewriter.go` | Translates PostgreSQL SQL → SQLite SQL |
| `internal/catalog/catalog.go` | Virtual `pg_catalog` / `information_schema` |
| `internal/pool/connpool.go` | SQLite connection pool + WAL writer scheduler |
| `internal/pgproto/types.go` | Shared types that break the import cycle |
| `cmd/sqlite-server/main.go` | CLI entry point (cobra) |

---

## Key Facts

- **Language**: Go 1.22+, zero CGO (`modernc.org/sqlite`)
- **Protocol**: PostgreSQL Wire Protocol v3 (full implementation)
- **Module**: `github.com/sqlite-server/sqlite-server`
- **Default port**: `5432`
- **Binary size**: ~16 MB (single self-contained executable)
- **First build**: 3–5 min (sqlite C→Go transpilation); subsequent builds < 10 s
