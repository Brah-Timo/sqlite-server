# Example 09 — Ruby + pg gem

**Application**: Library Management System  
**Language**: Ruby 3.2+  
**Driver**: `pg` gem (libpq-based PostgreSQL driver)

## What It Demonstrates

- `PG::Connection.open` block — auto-close connection
- `exec_params(sql, [params])` — parameterized queries, no SQL injection
- `exec(sql)` — raw DDL and unparameterized queries
- `conn.transaction { |c| ... }` — automatic rollback on exception
- `conn.prepare('name', sql)` + `conn.exec_prepared('name', [params])` — named prepared statements
- Module-based repository pattern (`MemberRepo`, `BookRepo`, `LoanRepo`, etc.)
- Ruby `Struct` with `keyword_init: true` for typed entities
- `PG::Result#to_a` and `#map` for row iteration
- Fine calculation and automatic issuance on overdue return
- `JULIANDAY()` for date difference in SQLite-compatible SQL
- `STRFTIME('%Y-%m', ...)` for monthly grouping
- `COALESCE` for nullable fallback values
- Full-text case-insensitive search with `LOWER() LIKE`

## Prerequisites

- Ruby 3.1+
- `libpq` installed (PostgreSQL client libraries)
- `pg` gem
- sqlite-server running on port 5432

## Install

```bash
gem install pg

# Or with Bundler:
bundle install
```

## Run

```bash
ruby app.rb

# Or with Bundler:
bundle exec ruby app.rb
```

## PowerShell

```powershell
# Install gem
gem install pg

# Run
ruby app.rb
```

## Install libpq on various platforms

### Ubuntu/Debian
```bash
sudo apt-get install libpq-dev
gem install pg
```

### macOS (Homebrew)
```bash
brew install postgresql
gem install pg -- --with-pg-config=$(brew --prefix postgresql)/bin/pg_config
```

### Windows
```powershell
# Download PostgreSQL installer from https://www.postgresql.org/download/windows/
# Install with "Command Line Tools" option checked
# Then:
gem install pg -- --with-pg-config="C:\Program Files\PostgreSQL\16\bin\pg_config.exe"
```

## Start sqlite-server first

```bash
# Linux / macOS
./sqlite-server --addr 127.0.0.1:5432 --no-auth -- library.db

# Windows PowerShell
.\sqlite-server.exe --addr 127.0.0.1:5432 --no-auth -- library.db
```

## Connection Config

Edit `DB_CONFIG` at the top of `app.rb`:

```ruby
DB_CONFIG = {
  host:     '127.0.0.1',
  port:     5432,
  user:     'admin',
  password: 'secret',
  dbname:   'library'
}.freeze
```

## Expected Output

```
Library Management System — sqlite-server Ruby pg Example
==========================================================

Setting up schema...
Schema ready.

─────────────────────────────────────────────────────────────────
  3. Add Books to Catalog
─────────────────────────────────────────────────────────────────
  Title                                          Genre         Year  Copies
  ──────────────────────────────────────────────────────────────────────────
  The Lord of the Rings                          Fantasy       1954       3
  Nineteen Eighty-Four                           Dystopian     1949       4
  ...

─────────────────────────────────────────────────────────────────
  10. Most Borrowed Books
─────────────────────────────────────────────────────────────────
  title                               author          genre    loan_count
  ──────────────────────────────────────────────────────────────────────
  Harry Potter and the Sorcerer's...  J.K. Rowling    Fantasy  2
  The Count of Monte Cristo           Alexandre Dumas Adventure  2
  ...
```
