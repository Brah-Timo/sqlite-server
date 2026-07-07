# SQL Compatibility Reference

sqlite-server speaks the **PostgreSQL Wire Protocol v3** and automatically translates
a large subset of PostgreSQL SQL into SQLite-compatible SQL before execution.
This document is the definitive reference for what works, what is translated, and
what is not supported.

---

## Table of Contents

1. [Translation Philosophy](#translation-philosophy)
2. [Supported Statements](#supported-statements)
3. [Data Types](#data-types)
4. [Type Casting (`::`)](#type-casting-)
5. [Functions](#functions)
6. [Operators](#operators)
7. [Query Clauses](#query-clauses)
8. [Transactions](#transactions)
9. [Prepared Statements](#prepared-statements)
10. [Virtual Catalog Tables](#virtual-catalog-tables)
    - [pg_catalog](#pg_catalog)
    - [information_schema](#information_schema)
11. [Session / Configuration Statements](#session--configuration-statements)
12. [Limitations](#limitations)
13. [Unsupported Features](#unsupported-features)

---

## Translation Philosophy

The rewriter in `sql/planner` uses an AST-based approach:

1. Parse PostgreSQL SQL → AST
2. Walk the AST and replace PG-specific nodes with SQLite-compatible equivalents
3. Emit the rewritten SQL string → send to `modernc.org/sqlite`

If a construct cannot be translated it is either silently absorbed (e.g. `SET TIME ZONE`)
or returns a clear `42601 syntax_error` with a hint.

---

## Supported Statements

| Statement | Support level | Notes |
|-----------|--------------|-------|
| `SELECT` | ✅ Full | Including CTEs, subqueries, JOINs, window functions |
| `INSERT` | ✅ Full | Including `RETURNING` clause |
| `UPDATE` | ✅ Full | Including `RETURNING` clause |
| `DELETE` | ✅ Full | Including `RETURNING` clause |
| `CREATE TABLE` | ✅ Full | `SERIAL`/`BIGSERIAL` translated to `INTEGER` |
| `CREATE TABLE IF NOT EXISTS` | ✅ Full | |
| `DROP TABLE` | ✅ Full | |
| `DROP TABLE IF EXISTS` | ✅ Full | |
| `CREATE INDEX` | ✅ Full | |
| `CREATE UNIQUE INDEX` | ✅ Full | |
| `DROP INDEX` | ✅ Full | |
| `ALTER TABLE ADD COLUMN` | ✅ Full | |
| `BEGIN` / `START TRANSACTION` | ✅ Full | See [Transactions](#transactions) |
| `COMMIT` | ✅ Full | |
| `ROLLBACK` | ✅ Full | |
| `SAVEPOINT` | ✅ Full | |
| `ROLLBACK TO SAVEPOINT` | ✅ Full | |
| `RELEASE SAVEPOINT` | ✅ Full | |
| `SET` | ⚠️ Absorbed | See [Session Statements](#session--configuration-statements) |
| `SHOW` | ⚠️ Virtual | Returns synthetic values |
| `EXPLAIN` | ❌ Not supported | |
| `COPY` | ❌ Not supported | |
| `LISTEN` / `NOTIFY` | ❌ Not supported | |
| `CREATE FUNCTION` | ❌ Not supported | |
| `CREATE TRIGGER` | ❌ Not supported | |
| `CREATE VIEW` | ⚠️ Partial | Basic views work; complex PG-specific syntax may fail |

---

## Data Types

### Column types

| PostgreSQL type | Translated to | Notes |
|----------------|--------------|-------|
| `SMALLINT` | `INTEGER` | |
| `INTEGER` / `INT` | `INTEGER` | |
| `BIGINT` | `INTEGER` | SQLite INTEGER is 64-bit |
| `SERIAL` | `INTEGER` | Auto-increment via SQLite `AUTOINCREMENT` semantics |
| `BIGSERIAL` | `INTEGER` | Same as SERIAL |
| `SMALLSERIAL` | `INTEGER` | Same as SERIAL |
| `REAL` / `FLOAT4` | `REAL` | |
| `DOUBLE PRECISION` / `FLOAT8` | `REAL` | |
| `NUMERIC(p,s)` / `DECIMAL` | `NUMERIC` | |
| `TEXT` | `TEXT` | |
| `VARCHAR(n)` | `TEXT` | Length constraint not enforced |
| `CHAR(n)` | `TEXT` | Padding not added |
| `BOOLEAN` / `BOOL` | `INTEGER` | `TRUE` → `1`, `FALSE` → `0` |
| `BYTEA` | `BLOB` | |
| `TIMESTAMP` | `TEXT` | ISO-8601 format |
| `TIMESTAMPTZ` | `TEXT` | Timezone info stripped |
| `DATE` | `TEXT` | ISO-8601 format |
| `TIME` | `TEXT` | |
| `INTERVAL` | `TEXT` | |
| `UUID` | `TEXT` | |
| `JSON` | `TEXT` | |
| `JSONB` | `TEXT` | No indexing |
| `ARRAY` types | ❌ Not supported | |

### Boolean literals

```sql
-- PostgreSQL
SELECT TRUE, FALSE, 't'::boolean, 'f'::boolean

-- Translated to SQLite
SELECT 1, 0, 1, 0
```

---

## Type Casting (`::`)

The PostgreSQL `::` cast operator is translated to `CAST(… AS …)`:

```sql
-- PostgreSQL
SELECT '42'::INTEGER
SELECT '3.14'::REAL
SELECT '2024-01-01'::DATE
SELECT '{"key":"val"}'::JSONB
SELECT NOW()::DATE

-- Translated to SQLite
SELECT CAST('42' AS INTEGER)
SELECT CAST('3.14' AS REAL)
SELECT CAST('2024-01-01' AS TEXT)
SELECT CAST('{"key":"val"}' AS TEXT)
SELECT CAST(datetime('now') AS TEXT)
```

---

## Functions

### Date / Time

| PostgreSQL function | SQLite equivalent | Notes |
|--------------------|------------------|-------|
| `NOW()` | `datetime('now')` | |
| `CURRENT_TIMESTAMP` | `datetime('now')` | |
| `CURRENT_DATE` | `date('now')` | |
| `CURRENT_TIME` | `time('now')` | |
| `CLOCK_TIMESTAMP()` | `datetime('now')` | |
| `TRANSACTION_TIMESTAMP()` | `datetime('now')` | |
| `EXTRACT(YEAR FROM x)` | `CAST(strftime('%Y', x) AS INTEGER)` | |
| `EXTRACT(MONTH FROM x)` | `CAST(strftime('%m', x) AS INTEGER)` | |
| `EXTRACT(DAY FROM x)` | `CAST(strftime('%d', x) AS INTEGER)` | |
| `EXTRACT(HOUR FROM x)` | `CAST(strftime('%H', x) AS INTEGER)` | |
| `EXTRACT(MINUTE FROM x)` | `CAST(strftime('%M', x) AS INTEGER)` | |
| `EXTRACT(SECOND FROM x)` | `CAST(strftime('%S', x) AS INTEGER)` | |
| `EXTRACT(EPOCH FROM x)` | `CAST(strftime('%s', x) AS INTEGER)` | |
| `DATE_TRUNC('day', x)` | `date(x)` | Only 'day' supported |
| `AGE(ts)` | ❌ Not supported | |
| `MAKE_DATE(y, m, d)` | ❌ Not supported | |
| `TO_CHAR(ts, fmt)` | ❌ Not supported | |
| `TO_DATE(str, fmt)` | ❌ Not supported | |

### String

| PostgreSQL function | SQLite equivalent | Notes |
|--------------------|------------------|-------|
| `LENGTH(s)` | `LENGTH(s)` | Same |
| `LOWER(s)` | `LOWER(s)` | Same |
| `UPPER(s)` | `UPPER(s)` | Same |
| `TRIM(s)` | `TRIM(s)` | Same |
| `LTRIM(s)` | `LTRIM(s)` | Same |
| `RTRIM(s)` | `RTRIM(s)` | Same |
| `SUBSTRING(s, n, l)` | `SUBSTR(s, n, l)` | |
| `SUBSTR(s, n, l)` | `SUBSTR(s, n, l)` | Same |
| `POSITION(x IN s)` | `INSTR(s, x)` | |
| `REPLACE(s, a, b)` | `REPLACE(s, a, b)` | Same |
| `SPLIT_PART(s, d, n)` | ❌ Not supported | |
| `REGEXP_REPLACE` | ❌ Not supported | |
| `FORMAT(fmt, …)` | ❌ Not supported | Use `printf` instead |
| `CONCAT(a, b)` | `(a \|\| b)` | |
| `REPEAT(s, n)` | ❌ Not supported | |

### Aggregate

| Function | Support |
|----------|---------|
| `COUNT(*)` | ✅ |
| `COUNT(col)` | ✅ |
| `SUM(col)` | ✅ |
| `AVG(col)` | ✅ |
| `MIN(col)` | ✅ |
| `MAX(col)` | ✅ |
| `STRING_AGG(col, sep)` | ✅ → `GROUP_CONCAT(col, sep)` |
| `ARRAY_AGG(col)` | ❌ Not supported |
| `JSON_AGG(col)` | ❌ Not supported |

### System / Session

| PostgreSQL function | Returns |
|--------------------|---------|
| `version()` | `"sqlite-server <version>"` |
| `current_database()` | database name from config |
| `current_schema()` | `"public"` |
| `current_user` | connected username |
| `session_user` | connected username |
| `pg_get_userbyid(oid)` | username or `"unknown"` |
| `pg_backend_pid()` | connection PID |
| `pg_postmaster_start_time()` | server start time |

---

## Operators

### Comparison

| PostgreSQL | SQLite | Notes |
|------------|--------|-------|
| `=` | `=` | Same |
| `<>` / `!=` | `<>` | Same |
| `<`, `<=`, `>`, `>=` | Same | Same |
| `IS NULL` | `IS NULL` | Same |
| `IS NOT NULL` | `IS NOT NULL` | Same |
| `BETWEEN` | `BETWEEN` | Same |
| `IN (...)` | `IN (...)` | Same |
| `LIKE` | `LIKE` | Case-insensitive in SQLite by default |
| `ILIKE` | `LIKE` | ILIKE → LIKE (SQLite LIKE is already case-insensitive) |
| `NOT LIKE` | `NOT LIKE` | Same |
| `NOT ILIKE` | `NOT LIKE` | |
| `SIMILAR TO` | ❌ Not supported | |

### String concatenation

```sql
-- PostgreSQL
SELECT 'hello' || ' ' || 'world'

-- SQLite: same syntax works
SELECT 'hello' || ' ' || 'world'
```

### JSON operators (`->`, `->>`)

SQLite does not have native JSON operators. Queries using `->` or `->>`
**are not translated** and will return a syntax error. Use SQLite's
`json_extract()` function instead:

```sql
-- Does NOT work
SELECT data->>'name' FROM items

-- Use this instead
SELECT json_extract(data, '$.name') FROM items
```

---

## Query Clauses

| Clause | Support | Notes |
|--------|---------|-------|
| `WHERE` | ✅ Full | |
| `ORDER BY` | ✅ Full | |
| `LIMIT` | ✅ Full | |
| `OFFSET` | ✅ Full | |
| `GROUP BY` | ✅ Full | |
| `HAVING` | ✅ Full | |
| `DISTINCT` | ✅ Full | |
| `JOIN` (INNER, LEFT, RIGHT, FULL) | ✅ Full | |
| `CROSS JOIN` | ✅ Full | |
| `NATURAL JOIN` | ✅ Full | |
| `UNION` / `UNION ALL` | ✅ Full | |
| `INTERSECT` | ✅ Full | |
| `EXCEPT` | ✅ Full | |
| `WITH` (CTE) | ✅ Full | |
| `WITH RECURSIVE` | ✅ Full | |
| `WINDOW` functions | ✅ Full | SQLite supports window functions |
| `RETURNING` | ✅ Full | |
| `FOR UPDATE` / `FOR SHARE` | ⚠️ Absorbed | Locking hints stripped (WAL handles concurrency) |
| `FETCH FIRST n ROWS ONLY` | ✅ → `LIMIT n` | |
| `LATERAL` | ❌ Not supported | |
| `TABLESAMPLE` | ❌ Not supported | |

---

## Transactions

All PostgreSQL transaction syntax is supported:

```sql
BEGIN;                          -- or: BEGIN WORK, START TRANSACTION
  INSERT INTO t VALUES (1);
  SAVEPOINT sp1;
  INSERT INTO t VALUES (2);
  ROLLBACK TO SAVEPOINT sp1;   -- undo INSERT 2
  INSERT INTO t VALUES (3);
COMMIT;                         -- or: COMMIT WORK, COMMIT TRANSACTION
```

```sql
-- Rollback entire transaction
BEGIN;
  DELETE FROM t WHERE id = 99;
ROLLBACK;                       -- or: ROLLBACK WORK
```

Transaction isolation level syntax (`BEGIN ISOLATION LEVEL READ COMMITTED`) is
**absorbed** (no-op). SQLite's WAL mode provides snapshot isolation which is
similar to PostgreSQL's `READ COMMITTED`.

---

## Prepared Statements

PostgreSQL `$N` positional parameters are translated to SQLite `?` parameters:

```sql
-- Application sends (via extended query protocol):
SELECT * FROM users WHERE id = $1 AND active = $2

-- Translated to:
SELECT * FROM users WHERE id = ? AND active = ?
-- Parameters: [42, true]
```

All standard client libraries (`lib/pq`, `psycopg2`, `pg` for Node.js) use
prepared statements automatically when you pass parameters separately from the
query string.

---

## Virtual Catalog Tables

sqlite-server intercepts queries to `pg_catalog` and `information_schema` and
returns synthetic data derived from the live SQLite schema (via `PRAGMA table_list`
and `PRAGMA table_info`).

### pg_catalog

| Table / View | Support | Notes |
|--------------|---------|-------|
| `pg_catalog.pg_type` | ✅ Synthetic | Common OIDs: bool, int4, text, float8, timestamp, uuid, … |
| `pg_catalog.pg_class` | ✅ Synthetic | One row per user table |
| `pg_catalog.pg_namespace` | ✅ Synthetic | Single row: `public` |
| `pg_catalog.pg_attribute` | ✅ Synthetic | One row per column of each table |
| `pg_catalog.pg_attrdef` | ✅ Synthetic | Default values |
| `pg_catalog.pg_index` | ✅ Synthetic | Indexes from `PRAGMA index_list` |
| `pg_catalog.pg_constraint` | ✅ Synthetic | PRIMARY KEY, UNIQUE, NOT NULL |
| `pg_catalog.pg_description` | ✅ Empty | No comments (always returns 0 rows) |
| `pg_catalog.pg_database` | ✅ Synthetic | Single row with current database name |
| `pg_catalog.pg_roles` | ✅ Synthetic | Single row with connected user |
| `pg_catalog.pg_settings` | ✅ Partial | A handful of common settings |
| `pg_catalog.pg_proc` | ⚠️ Minimal | Only functions referenced by DBeaver |
| `pg_catalog.pg_trigger` | ✅ Empty | Always returns 0 rows |
| `pg_catalog.pg_views` | ✅ Synthetic | Views from SQLite schema |
| `pg_catalog.pg_sequences` | ✅ Empty | No native sequences |

### information_schema

| Table / View | Support |
|--------------|---------|
| `information_schema.tables` | ✅ Synthetic — user tables only (`table_schema = 'public'`) |
| `information_schema.columns` | ✅ Synthetic — column names, ordinal positions, data types |
| `information_schema.table_constraints` | ✅ Synthetic — PRIMARY KEY, UNIQUE |
| `information_schema.key_column_usage` | ✅ Synthetic |
| `information_schema.referential_constraints` | ✅ Empty |
| `information_schema.views` | ✅ Synthetic |
| `information_schema.routines` | ✅ Empty |
| `information_schema.schemata` | ✅ Synthetic — single row: `public` |

---

## Session / Configuration Statements

These statements are **absorbed** (accepted, no error, but no effect):

```sql
SET TIME ZONE 'UTC';
SET TIME ZONE 'America/New_York';
SET search_path TO public;
SET client_encoding TO 'UTF8';
SET standard_conforming_strings = on;
SET extra_float_digits = 3;
SET application_name = 'myapp';
```

These `SHOW` statements return **synthetic values**:

```sql
SHOW search_path;            -- returns "public"
SHOW server_version;         -- returns "14.0"  (compatibility string)
SHOW client_encoding;        -- returns "UTF8"
SHOW standard_conforming_strings;  -- returns "on"
SHOW TimeZone;               -- returns "UTC"
SHOW max_connections;        -- returns configured --max-conn value
```

---

## Limitations

| Limitation | Reason |
|------------|--------|
| No `ARRAY` type | SQLite has no native array storage |
| No `HSTORE` | PostgreSQL-specific extension |
| No `CIDR` / `INET` | Network address types |
| No full-text search (`@@`) | SQLite FTS is different API |
| No `PostGIS` / geometric types | Extension |
| `VARCHAR(n)` length not enforced | SQLite ignores length constraints on TEXT |
| `CHECK` constraints accepted but not always enforced | Depends on SQLite version |
| Foreign keys require `PRAGMA foreign_keys = ON` | Not enabled by default |
| No `DOMAIN` types | SQLite has no domain concept |
| `RETURNING` requires SQLite 3.35+ | Included in `modernc.org/sqlite v1.33+` |
| No `LISTEN` / `NOTIFY` | No pub-sub mechanism |
| No logical replication | SQLite has no WAL-shipping |
| No `pg_hba.conf` | Authentication is all-or-nothing (`--no-auth`) |
| No role-based access control | All connections have the same permissions |
| No tablespaces | SQLite is a single file |
| No schemas (namespaces) | Everything is in `main` / `public` |

---

## Unsupported Features

The following will return an error:

- `CREATE EXTENSION` — SQLite has no extension mechanism
- `EXPLAIN [ANALYZE]` — not intercepted
- `COPY FROM / TO` — bulk file import/export
- `\copy` — psql-specific meta-command
- `CREATE SEQUENCE` / `ALTER SEQUENCE` — no native sequence object
- `ALTER TABLE DROP COLUMN` — SQLite limitation (requires table rebuild)
- `ALTER TABLE RENAME COLUMN` — SQLite 3.25+ supports this; planner does not yet translate it
- `GRANT` / `REVOKE` — no access control
- Stored procedures and functions
- Triggers (at the SQL level)
- Recursive CTEs referencing themselves more than once per row
