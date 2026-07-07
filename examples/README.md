# sqlite-server — Code Examples in 10 Languages

This directory contains **10 full, working examples** demonstrating how to connect to `sqlite-server` from different programming languages.

Every example connects via the **PostgreSQL wire protocol** — the same driver you would use for a real PostgreSQL database. No special sqlite-server SDK is needed.

---

## Quick Start

Start sqlite-server before running any example:

```bash
# Linux / macOS
./sqlite-server --addr 127.0.0.1:5432 --no-auth -- demo.db

# Windows PowerShell
.\sqlite-server.exe --addr 127.0.0.1:5432 --no-auth -- demo.db
```

---

## Examples Index

| # | Language | Application | Driver / Library | Run Command |
|---|----------|-------------|-----------------|-------------|
| 01 | **Go** | User Management | `database/sql` + `lib/pq` | `go run main.go` |
| 02 | **Python** | Inventory System | `psycopg2` | `python app.py` |
| 03 | **Node.js** | Blog Platform | `pg` (node-postgres) | `node app.js` |
| 04 | **TypeScript** | Task Manager | `pg` + `@types/pg` | `npx ts-node app.ts` |
| 05 | **Java** | E-Commerce | JDBC (`postgresql-42.x`) | `mvn compile exec:java` |
| 06 | **C#** | HR System | `Npgsql` 8.0 | `dotnet run` |
| 07 | **Rust** | Analytics Dashboard | `tokio-postgres` | `cargo run` |
| 08 | **PHP** | CMS | `PDO pdo_pgsql` | `php app.php` |
| 09 | **Ruby** | Library System | `pg` gem | `ruby app.rb` |
| 10 | **Python** | School System | `SQLAlchemy` 2.0 ORM | `python app.py` |

---

## Example Details

### 01 — Go (`database/sql` + `lib/pq`)
**Directory**: `01-go-basic/`  
**Application**: User Management  
**Key features**: Standard `database/sql` interface, `lib/pq` driver, transactions, aggregates, prepared statements, row scanning into structs.

```bash
cd 01-go-basic
go run main.go
```

---

### 02 — Python (`psycopg2`)
**Directory**: `02-python-psycopg2/`  
**Application**: Inventory Management System  
**Key features**: `psycopg2.connect()`, `DictCursor` for dict-style rows, `executemany()` for batch insert, context manager (`with conn`), `%s` placeholders.

```bash
cd 02-python-psycopg2
pip install -r requirements.txt
python app.py
```

---

### 03 — Node.js (`pg` Pool)
**Directory**: `03-nodejs-pg/`  
**Application**: Blog Platform  
**Key features**: `pg.Pool`, repository pattern with classes, `async/await`, ILIKE via `LOWER(...) LIKE`, complex JOINs, error handling.

```bash
cd 03-nodejs-pg
npm install
node app.js
```

---

### 04 — TypeScript (`pg` + types)
**Directory**: `04-typescript-pg/`  
**Application**: Task Management System  
**Key features**: Typed interfaces, generic `query<T>()` / `queryOne<T>()`, typed repositories, `UpdateTaskInput` with optional fields, `transaction(client)`, `TaskWithDetails` view type.

```bash
cd 04-typescript-pg
npm install
npx ts-node app.ts
# or: npm run build && npm start
```

---

### 05 — Java (JDBC)
**Directory**: `05-java-jdbc/`  
**Application**: E-Commerce System  
**Key features**: `DriverManager.getConnection()`, `PreparedStatement`, `ResultSet` → Java records, `executeBatch()` for bulk inserts, `Savepoint` for nested transactions, BigDecimal for money.

```bash
cd 05-java-jdbc
mvn compile exec:java
# or: mvn package && java -jar target/...-jar-with-dependencies.jar
```

---

### 06 — C# (Npgsql)
**Directory**: `06-csharp-npgsql/`  
**Application**: HR Management System  
**Key features**: `NpgsqlConnection` async, `await using`, C# records, `BeginTransactionAsync()` + `CommitAsync()`, `RETURNING *`, primary constructor repos (C# 12), star-rating display.

```bash
cd 06-csharp-npgsql
dotnet run
# Self-contained exe: dotnet publish -c Release -r win-x64 --self-contained true
```

---

### 07 — Rust (`tokio-postgres`)
**Directory**: `07-rust-tokio-postgres/`  
**Application**: Analytics & Metrics Dashboard  
**Key features**: Tokio async runtime, `#[tokio::main]`, `client.prepare()` reused for bulk inserts, transaction, typed row extraction, time-series with `STRFTIME`, ASCII bar charts.

```bash
cd 07-rust-tokio-postgres
cargo run
# Release: cargo run --release
```

---

### 08 — PHP (`PDO pdo_pgsql`)
**Directory**: `08-php-pdo/`  
**Application**: Content Management System  
**Key features**: `PDO` with `pgsql` DSN, named `:param` placeholders, `lastInsertId()`, `beginTransaction()` / `commit()` / `rollBack()`, `bindValue()` with `PDO::PARAM_INT`, pagination with `LIMIT/OFFSET`.

```bash
cd 08-php-pdo
php app.php
```

---

### 09 — Ruby (`pg` gem)
**Directory**: `09-ruby-pg/`  
**Application**: Library Management System  
**Key features**: `PG::Connection.open` block, `exec_params()` for safety, `conn.transaction {}` auto-rollback, `conn.prepare()` + `exec_prepared()`, Ruby `Struct` with `keyword_init`, modules as repositories, fine calculation on overdue returns.

```bash
cd 09-ruby-pg
gem install pg    # or: bundle install
ruby app.rb
```

---

### 10 — Python (`SQLAlchemy` 2.0 ORM)
**Directory**: `10-python-sqlalchemy/`  
**Application**: School Management System  
**Key features**: `DeclarativeBase`, `Mapped[]` type annotations, `relationship()` + `back_populates`, many-to-many with `secondary` table, `selectinload()` / `joinedload()`, ORM `select()`, `func.count/avg/sum`, `and_()` / `or_()`, `@property`.

```bash
cd 10-python-sqlalchemy
pip install -r requirements.txt
python app.py
```

---

## Connection Parameters

All examples default to the same configuration. Edit the constants at the top of each file:

| Parameter | Default |
|-----------|---------|
| Host      | `127.0.0.1` |
| Port      | `5432` |
| Username  | `admin` |
| Password  | `secret` |
| Database  | varies per example |

Start sqlite-server with `--no-auth` to skip password verification (any user/password accepted).

---

## Dependency Summary

| # | Language | What to Install |
|---|----------|----------------|
| 01 | Go | `go get github.com/lib/pq` (auto via `go.mod`) |
| 02 | Python | `pip install psycopg2-binary` |
| 03 | Node.js | `npm install pg` |
| 04 | TypeScript | `npm install pg @types/pg ts-node typescript` |
| 05 | Java | Maven downloads `org.postgresql:postgresql:42.7.2` |
| 06 | C# | `dotnet restore` (Npgsql 8.0.3) |
| 07 | Rust | `cargo build` (tokio-postgres 0.7) |
| 08 | PHP | Built-in `pdo_pgsql` extension (enable in php.ini) |
| 09 | Ruby | `gem install pg` (needs libpq) |
| 10 | Python | `pip install sqlalchemy psycopg2-binary` |

---

## SQL Compatibility Notes

sqlite-server translates PostgreSQL SQL to SQLite automatically. These translations are transparent to your application code:

| PostgreSQL | SQLite (internal) |
|------------|-------------------|
| `SERIAL` / `BIGSERIAL` | `INTEGER AUTOINCREMENT` |
| `BOOLEAN` | `INTEGER` (0/1) |
| `NOW()` | `DATETIME('now')` |
| `ILIKE` | `LOWER() LIKE LOWER()` |
| `$1, $2` params | `?` params |
| `EXTRACT(year FROM ...)` | `STRFTIME('%Y', ...)` |
| `::cast` syntax | `CAST(... AS ...)` |
| `ON CONFLICT DO NOTHING` | `INSERT OR IGNORE` |

See `docs/06-sql-compatibility.md` for the complete translation reference.
