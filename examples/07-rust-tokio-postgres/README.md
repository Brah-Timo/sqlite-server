# Example 07 — Rust + tokio-postgres

**Application**: Analytics & Metrics Dashboard  
**Language**: Rust (2021 edition)  
**Driver**: `tokio-postgres` 0.7

## What It Demonstrates

- Async PostgreSQL client with Tokio runtime (`#[tokio::main]`)
- Typed row extraction: `row.get::<_, i64>("column")`
- Prepared statements with `client.prepare()` — reused for bulk inserts
- `client.transaction()` → `tx.commit()` / `tx.rollback()`
- Struct-based domain models (Website, PageView, Event, stats reports)
- Bulk INSERT using a single prepared statement executed many times
- Time-series aggregation with `STRFTIME('%Y-%m-%d', created_at)`
- Multi-site comparison queries
- CLI bar chart visualization with Unicode block characters
- Error handling with `Box<dyn Error>` propagated via `?`
- No-TLS connection with `NoTls`

## Prerequisites

- Rust 1.75+ (2021 edition)
- Cargo
- sqlite-server running on port 5432

## Run

```bash
# Debug build and run
cargo run

# Optimized release build
cargo run --release
```

## PowerShell

```powershell
# Run in debug mode
cargo run

# Build optimized binary
cargo build --release

# Run the compiled binary
.\target\release\analytics-dashboard.exe
```

## Cross-Compile to Windows (from Linux)

```bash
# Add Windows target
rustup target add x86_64-pc-windows-gnu
sudo apt-get install gcc-mingw-w64-x86-64

# Build
cargo build --release --target x86_64-pc-windows-gnu

# Output:
# target/x86_64-pc-windows-gnu/release/analytics-dashboard.exe
```

## Start sqlite-server first

```bash
# Linux / macOS
./sqlite-server --addr 127.0.0.1:5432 --no-auth -- analytics.db

# Windows PowerShell
.\sqlite-server.exe --addr 127.0.0.1:5432 --no-auth -- analytics.db
```

## Connection String

Edit `DB_URL` constant in `src/main.rs`:

```rust
const DB_URL: &str =
    "host=127.0.0.1 port=5432 user=admin password=secret dbname=analytics";
```

## Expected Output

```
Analytics Dashboard — sqlite-server tokio-postgres Example
============================================================

Connected successfully.
Setting up schema...
Schema ready.

─────────────────────────────────────────────────────────────────
  4. Overall Stats for acme.com
─────────────────────────────────────────────────────────────────
  Total Page Views  : 20
  Unique Sessions   : 10
  Avg Time on Page  : 138.3s

─────────────────────────────────────────────────────────────────
  6. Traffic by Country
─────────────────────────────────────────────────────────────────
  US               4  ████████████████████
  GB               2  ██████████
  CA               3  ███████████████
  ...

─────────────────────────────────────────────────────────────────
  9. Top Conversion Events
─────────────────────────────────────────────────────────────────
  Event             Count   Total Value   Avg Value
  ──────────────────────────────────────────────────
  purchase              3       697.00      232.33
  signup                3         0.00        0.00
  plan_view             3       247.00       82.33
```

## Why tokio-postgres?

`tokio-postgres` is the low-level, zero-overhead PostgreSQL driver for Rust. It maps 1:1 to the wire protocol, making it ideal for understanding exactly what's happening. For a higher-level ORM experience with Rust, consider `sqlx` (also works with sqlite-server via the PostgreSQL driver).
