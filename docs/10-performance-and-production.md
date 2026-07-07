# Performance & Production Deployment

This document covers SQLite performance tuning, connection pool settings,
deployment patterns (Docker, Kubernetes, systemd), backup strategies, security
hardening, and monitoring for production use of sqlite-server.

---

## Table of Contents

1. [Performance Characteristics](#performance-characteristics)
2. [SQLite Tuning](#sqlite-tuning)
   - [WAL Mode](#wal-mode)
   - [Cache Size](#cache-size)
   - [Synchronous Mode](#synchronous-mode)
   - [Temp Store](#temp-store)
   - [Page Size](#page-size)
3. [Connection Pool Tuning](#connection-pool-tuning)
4. [Write Throughput](#write-throughput)
5. [Read Throughput](#read-throughput)
6. [Deployment Patterns](#deployment-patterns)
   - [systemd Service](#systemd-service)
   - [Docker](#docker)
   - [Docker Compose](#docker-compose)
   - [Kubernetes](#kubernetes)
7. [Backup and Recovery](#backup-and-recovery)
   - [Online Backup (SQLite API)](#online-backup-sqlite-api)
   - [File Copy Backup](#file-copy-backup)
   - [Point-in-Time Restore](#point-in-time-restore)
8. [Security Hardening](#security-hardening)
   - [TLS Configuration](#tls-configuration)
   - [Network Restrictions](#network-restrictions)
   - [File Permissions](#file-permissions)
9. [Monitoring](#monitoring)
10. [Capacity Limits](#capacity-limits)
11. [When to Use sqlite-server vs. PostgreSQL](#when-to-use-sqlite-server-vs-postgresql)

---

## Performance Characteristics

sqlite-server is designed for **read-heavy workloads** with moderate write
throughput. Key characteristics:

| Metric | Typical value | Notes |
|--------|--------------|-------|
| Read latency | < 1 ms | In-process SQLite, no network round-trip to DB |
| Write latency | 1–5 ms | Serialised through writer goroutine |
| Concurrent readers | Unlimited (WAL) | Readers never block readers |
| Concurrent writers | 1 at a time | Writer goroutine serialises all mutations |
| Max database size | 281 TB (SQLite limit) | Practical limit: few hundred GB per file |
| Max connections | Configurable via `--max-conn` | Default: 100 |

---

## SQLite Tuning

### WAL Mode

Always enable WAL mode in production. It allows readers and the single writer to
operate concurrently without blocking each other.

```bash
./sqlite-server serve --wal ...
```

To verify WAL is active, connect and run:
```sql
PRAGMA journal_mode;
-- should return: wal
```

### Cache Size

The default SQLite page cache is small (2 MB). Increase it for workloads that scan
large tables:

```sql
-- Set cache to 64 MB (negative value = kilobytes)
PRAGMA cache_size = -65536;
```

To apply automatically on every connection, add it to the server's startup sequence.
Currently this requires a code change in `internal/pool/connpool.go`:

```go
// After opening the database:
_, err = db.Exec("PRAGMA cache_size = -65536")
```

### Synchronous Mode

Controls how aggressively SQLite flushes data to disk:

| Mode | Description | Durability | Speed |
|------|-------------|-----------|-------|
| `FULL` | Sync after every write | Highest | Slowest |
| `NORMAL` | Sync at critical points (default for WAL) | Good | Fast |
| `OFF` | Never sync | None | Fastest |

For production with WAL mode, `NORMAL` is the recommended balance:

```sql
PRAGMA synchronous = NORMAL;
```

For maximum durability (e.g. financial data):

```sql
PRAGMA synchronous = FULL;
```

### Temp Store

Store temporary tables and indexes in memory instead of on disk:

```sql
PRAGMA temp_store = MEMORY;
```

This improves performance for queries with large sorts or temporary aggregations.

### Page Size

The page size must be set **before the database is created** (cannot be changed
later). For modern workloads, 8 KB or 16 KB is a good choice:

```sql
-- Only effective on a new empty database file
PRAGMA page_size = 8192;
```

Default is 4096 bytes (4 KB).

---

## Connection Pool Tuning

`--max-conn` controls how many simultaneous client connections are accepted.
The pool is backed by a counting semaphore.

```bash
# Conservative: low concurrency, predictable latency
./sqlite-server serve --max-conn 20 ...

# Moderate: typical OLTP workloads
./sqlite-server serve --max-conn 100 ...

# High: many short-lived connections (e.g. API gateway)
./sqlite-server serve --max-conn 500 ...
```

**Rule of thumb**: Set `--max-conn` to roughly 10× the number of concurrent active
writers you expect. More connections beyond this do not improve throughput and
increase context-switching overhead.

On the **client side**, configure the connection pool to match:

```go
// Go — database/sql
db.SetMaxOpenConns(20)
db.SetMaxIdleConns(5)
db.SetConnMaxLifetime(5 * time.Minute)
db.SetConnMaxIdleTime(1 * time.Minute)
```

```python
# Python — psycopg2 pool
from psycopg2 import pool
pool = pool.ThreadedConnectionPool(minconn=2, maxconn=20, dsn="...")
```

---

## Write Throughput

All writes are serialised through a single goroutine. To maximise write throughput:

### Batch inserts

```sql
-- Instead of 1000 individual INSERTs:
INSERT INTO events (name, ts) VALUES
  ('login',  '2024-01-01 10:00:00'),
  ('logout', '2024-01-01 10:05:00'),
  ('login',  '2024-01-01 10:10:00');
  -- ... up to a few thousand rows per statement
```

### Wrap in transactions

Each auto-committed INSERT is a separate WAL write. Wrapping in a transaction
reduces fsync calls dramatically:

```sql
BEGIN;
INSERT INTO events (name, ts) VALUES ('event1', NOW());
INSERT INTO events (name, ts) VALUES ('event2', NOW());
-- ... up to thousands of rows
COMMIT;
```

Benchmark comparison (approximate):
- Single auto-commit INSERTs: ~500–1000 rows/sec
- Batched in transaction: ~50,000–100,000 rows/sec

### Asynchronous writes from your application

Use a worker queue in your application to buffer writes and flush them in batches:

```go
// Example: batch writes every 10 ms or 100 rows
ticker := time.NewTicker(10 * time.Millisecond)
batch  := make([]Row, 0, 100)

for {
    select {
    case row := <-rowCh:
        batch = append(batch, row)
        if len(batch) >= 100 {
            flush(batch); batch = batch[:0]
        }
    case <-ticker.C:
        if len(batch) > 0 {
            flush(batch); batch = batch[:0]
        }
    }
}
```

---

## Read Throughput

Reads run concurrently (WAL mode). To maximise read throughput:

- **Add indexes** on frequently queried columns
- **Use `EXPLAIN QUERY PLAN`** to verify index usage
- **Increase `cache_size`** to keep hot pages in memory
- **Use prepared statements** to avoid re-parsing the same query

```sql
-- Check if an index is being used:
EXPLAIN QUERY PLAN
  SELECT * FROM users WHERE email = 'alice@example.com';

-- Output should include: USING INDEX idx_users_email
-- If not: CREATE INDEX idx_users_email ON users(email);
```

---

## Deployment Patterns

### systemd Service

Create `/etc/systemd/system/sqlite-server.service`:

```ini
[Unit]
Description=sqlite-server — PostgreSQL wire protocol over SQLite
After=network.target
StartLimitIntervalSec=60
StartLimitBurst=3

[Service]
Type=simple
User=sqlite-server
Group=sqlite-server
WorkingDirectory=/var/lib/sqlite-server
ExecStart=/usr/local/bin/sqlite-server serve \
    --config /etc/sqlite-server/production.yaml
ExecReload=/bin/kill -HUP $MAINPID
Restart=on-failure
RestartSec=5s

# Security hardening
NoNewPrivileges=true
ProtectSystem=strict
ReadWritePaths=/var/lib/sqlite-server /var/log/sqlite-server
PrivateTmp=true

[Install]
WantedBy=multi-user.target
```

Enable and start:

```bash
# Create user
useradd --system --home /var/lib/sqlite-server --shell /bin/false sqlite-server

# Create directories
mkdir -p /var/lib/sqlite-server /etc/sqlite-server
chown sqlite-server:sqlite-server /var/lib/sqlite-server

# Copy binary and config
cp sqlite-server /usr/local/bin/
cp configs/production.yaml /etc/sqlite-server/

# Enable and start
systemctl daemon-reload
systemctl enable sqlite-server
systemctl start sqlite-server
systemctl status sqlite-server

# View logs
journalctl -u sqlite-server -f
```

### Docker

```dockerfile
# Dockerfile (already in repo root)
FROM golang:1.21-alpine AS builder
WORKDIR /build
COPY . .
RUN go build -o sqlite-server ./cmd/sqlite-server/

FROM alpine:3.19
RUN apk add --no-cache ca-certificates
COPY --from=builder /build/sqlite-server /usr/local/bin/
RUN adduser -D -H sqlite-server
USER sqlite-server
EXPOSE 5432
ENTRYPOINT ["sqlite-server"]
CMD ["serve", "--addr", "0.0.0.0:5432", "--database", "/data/db.sqlite"]
```

Build and run:

```bash
# Build image
docker build -t sqlite-server:latest .

# Run with persistent volume
docker run -d \
  --name sqlite-server \
  -p 5432:5432 \
  -v sqlite-data:/data \
  -e SQLITE_SERVER_NO_AUTH=true \
  sqlite-server:latest

# Check logs
docker logs -f sqlite-server

# Connect
psql -h 127.0.0.1 -p 5432 -U anyuser -d anydb
```

### Docker Compose

`configs/docker-compose.yml`:

```yaml
version: "3.9"

services:
  sqlite-server:
    image: sqlite-server:latest
    build: .
    ports:
      - "5432:5432"
    volumes:
      - sqlite-data:/data
    environment:
      SQLITE_SERVER_ADDR:      "0.0.0.0:5432"
      SQLITE_SERVER_DATABASE:  "/data/prod.db"
      SQLITE_SERVER_MAX_CONN:  "100"
      SQLITE_SERVER_WAL:       "true"
      SQLITE_SERVER_LOG_LEVEL: "info"
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -h localhost -p 5432 || exit 1"]
      interval: 10s
      timeout:  5s
      retries:  5
    restart: unless-stopped

volumes:
  sqlite-data:
```

```bash
# Start
docker compose -f configs/docker-compose.yml up -d

# Health check
docker compose -f configs/docker-compose.yml ps

# Logs
docker compose -f configs/docker-compose.yml logs -f

# Stop
docker compose -f configs/docker-compose.yml down
```

### Kubernetes

`k8s/deployment.yaml`:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: sqlite-server
  labels:
    app: sqlite-server
spec:
  replicas: 1          # SQLite is single-writer; do NOT scale to multiple replicas
  selector:
    matchLabels:
      app: sqlite-server
  template:
    metadata:
      labels:
        app: sqlite-server
    spec:
      containers:
      - name: sqlite-server
        image: sqlite-server:latest
        args:
        - serve
        - --addr=0.0.0.0:5432
        - --database=/data/db.sqlite
        - --max-conn=100
        - --wal
        - --log-level=info
        ports:
        - containerPort: 5432
        volumeMounts:
        - name: data
          mountPath: /data
        readinessProbe:
          tcpSocket:
            port: 5432
          initialDelaySeconds: 3
          periodSeconds: 5
        livenessProbe:
          tcpSocket:
            port: 5432
          initialDelaySeconds: 10
          periodSeconds: 30
        resources:
          requests:
            cpu: "100m"
            memory: "128Mi"
          limits:
            cpu: "1000m"
            memory: "512Mi"
      volumes:
      - name: data
        persistentVolumeClaim:
          claimName: sqlite-server-pvc
---
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: sqlite-server-pvc
spec:
  accessModes:
  - ReadWriteOnce   # IMPORTANT: must be RWO, not RWX
  resources:
    requests:
      storage: 10Gi
---
apiVersion: v1
kind: Service
metadata:
  name: sqlite-server
spec:
  selector:
    app: sqlite-server
  ports:
  - port: 5432
    targetPort: 5432
  type: ClusterIP
```

> ⚠️ **Important**: SQLite is a single-file database. Never run more than **one
> replica** pointing to the same `PersistentVolumeClaim`. Multiple writers will
> corrupt the database.

---

## Backup and Recovery

### Online Backup (SQLite API)

The safest way to back up a running database is to connect and use the
`.backup` command via `sqlite3` CLI:

```bash
sqlite3 /var/lib/sqlite-server/prod.db ".backup /backup/prod-$(date +%Y%m%d-%H%M%S).db"
```

This uses SQLite's online backup API, which is safe while the server is running.

### File Copy Backup

When the server is **not running** (or after a clean shutdown):

```bash
# Stop the server
systemctl stop sqlite-server

# Copy the database file and WAL (both files together)
cp /var/lib/sqlite-server/prod.db    /backup/prod-$(date +%Y%m%d).db
cp /var/lib/sqlite-server/prod.db-wal /backup/prod-$(date +%Y%m%d).db-wal 2>/dev/null || true
cp /var/lib/sqlite-server/prod.db-shm /backup/prod-$(date +%Y%m%d).db-shm 2>/dev/null || true

# Restart
systemctl start sqlite-server
```

> **Warning**: Copying the `.db` file while the server is running without using
> the backup API may produce a corrupt backup if a write is in progress.

### Automated Daily Backup (cron)

```bash
# /etc/cron.d/sqlite-server-backup
0 2 * * * sqlite-server sqlite3 /var/lib/sqlite-server/prod.db \
  ".backup /backup/prod-$(date +\%Y\%m\%d).db" \
  && find /backup -name 'prod-*.db' -mtime +30 -delete
```

### Point-in-Time Restore

SQLite does not support WAL-shipping or continuous archiving like PostgreSQL.
The closest equivalent is:

1. Take frequent full backups (e.g. every hour via `sqlite3 .backup`)
2. Restore the most recent backup before the failure point:

```bash
systemctl stop sqlite-server
cp /backup/prod-20240115-0200.db /var/lib/sqlite-server/prod.db
systemctl start sqlite-server
```

---

## Security Hardening

### TLS Configuration

Generate a self-signed certificate for development:

```bash
openssl req -x509 -newkey rsa:4096 -nodes \
  -keyout server.key \
  -out server.crt \
  -days 365 \
  -subj "/CN=sqlite-server"
```

For production, use a certificate from Let's Encrypt or your CA.

Start the server with TLS:

```bash
./sqlite-server serve \
  --ssl-cert /etc/ssl/certs/server.crt \
  --ssl-key  /etc/ssl/private/server.key \
  --addr 0.0.0.0:5432
```

Clients connect with `sslmode=require`:

```bash
psql "host=myserver port=5432 user=myuser dbname=mydb sslmode=require"
```

### Network Restrictions

**Bind to localhost only** (recommended for applications on the same host):

```bash
./sqlite-server serve --addr 127.0.0.1:5432 ...
```

**Firewall** (allow only specific IPs):

```bash
# iptables: allow only the application server
iptables -A INPUT -p tcp --dport 5432 -s 10.0.1.5 -j ACCEPT
iptables -A INPUT -p tcp --dport 5432 -j DROP

# ufw
ufw allow from 10.0.1.5 to any port 5432
```

### File Permissions

The database file should only be readable/writable by the sqlite-server user:

```bash
chown sqlite-server:sqlite-server /var/lib/sqlite-server/prod.db
chmod 600 /var/lib/sqlite-server/prod.db
```

The binary should not be writable by the service user:

```bash
chown root:root /usr/local/bin/sqlite-server
chmod 755 /usr/local/bin/sqlite-server
```

---

## Monitoring

### Health check endpoint

Use `pg_isready` (from the PostgreSQL client tools) to check if the server is
accepting connections:

```bash
pg_isready -h 127.0.0.1 -p 5432
# output: 127.0.0.1:5432 - accepting connections (exit code 0)
# or: 127.0.0.1:5432 - no response (exit code 2)
```

Use this in Docker / Kubernetes health checks (see examples above).

### Log monitoring

```bash
# systemd — follow logs
journalctl -u sqlite-server -f

# Docker
docker logs -f sqlite-server

# Filter for errors
journalctl -u sqlite-server | grep '"level":"error"'
```

Log fields (JSON format when `--log-level info` or higher):

| Field | Description |
|-------|-------------|
| `time` | RFC3339 timestamp |
| `level` | `debug`, `info`, `warn`, `error` |
| `msg` | Human-readable message |
| `addr` | Client address |
| `sql` | Translated SQL string (debug level only) |
| `duration_ms` | Query execution time |
| `rows` | Number of rows returned |
| `err` | Error message (if any) |

### Metrics (future)

Prometheus metrics are not yet implemented. To measure performance manually:

```bash
# Query latency with psql timing
psql -h 127.0.0.1 -U user -d db -c "\timing" -c "SELECT COUNT(*) FROM events"
```

---

## Capacity Limits

| Resource | Limit | Notes |
|----------|-------|-------|
| Database file size | 281 TB | SQLite theoretical max |
| Rows per table | Unlimited | Practical limit: billions |
| Columns per table | 2000 | SQLite default |
| Tables per database | Unlimited | |
| Concurrent connections | `--max-conn` (default 100) | Semaphore-limited |
| Concurrent readers | Unlimited | WAL mode |
| Concurrent writers | 1 | Single writer goroutine |
| Query string length | 1 GB | `SQLITE_MAX_SQL_LENGTH` |
| Write throughput | ~50K rows/sec | With batching and transactions |
| Read throughput | ~500K rows/sec | Simple indexed selects |

---

## When to Use sqlite-server vs. PostgreSQL

### Use sqlite-server when:

- You want a **zero-dependency embedded database** that speaks the PostgreSQL protocol
- Your application is **single-server** (no multi-master, no replicas needed)
- **Read-heavy** workload (analytics, reporting, read-mostly APIs)
- **Development and testing** — easy to start, no installation required
- **Edge deployments** — small binaries, runs on minimal hardware
- **Prototype / MVP** — iterate quickly without managing a Postgres cluster

### Use PostgreSQL when:

- You need **true multi-server replication** (streaming, logical, Patroni)
- You need **row-level security** and fine-grained permissions
- You need **advanced PostgreSQL features**: partitioning, publications, foreign tables, extensions (PostGIS, pgvector, etc.)
- Your workload is **write-heavy** with many concurrent writers
- You need **LISTEN/NOTIFY** or logical replication
- You have **compliance requirements** (audit logging, point-in-time recovery with WAL archiving)
- Your database will exceed **hundreds of GB** and needs parallel query execution
