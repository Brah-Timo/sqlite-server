# توافق SQL — SQL Compatibility Guide
## ما يعمل، ما يُترجَم، ما لا يُدعم

---

## 1. ترجمات تلقائية (Auto-Translated)

كل هذه الترجمات تحدث في `sql/planner/rewriter.go` — الكلاينت لا يشعر بها.

### أنواع البيانات

| PostgreSQL | SQLite المُولَّد | ملاحظة |
|-----------|----------------|--------|
| `SERIAL` | `INTEGER PRIMARY KEY AUTOINCREMENT` | |
| `BIGSERIAL` | `INTEGER PRIMARY KEY AUTOINCREMENT` | |
| `SMALLSERIAL` | `INTEGER PRIMARY KEY AUTOINCREMENT` | |
| `BOOLEAN` | `INTEGER` | TRUE→1, FALSE→0 |
| `BYTEA` | `BLOB` | |
| `VARCHAR(n)` | `TEXT` | SQLite TEXT affinity |
| `CHAR(n)` | `TEXT` | |
| `DOUBLE PRECISION` | `REAL` | |
| `NUMERIC(p,s)` | `NUMERIC` | |
| `TIMESTAMPTZ` | `TEXT` | SQLite لا يدعم zones |
| `JSON` | `TEXT` | مخزَّنة كنص |
| `JSONB` | `TEXT` | مخزَّنة كنص |
| `UUID` | `TEXT` | |

### دوال وتعبيرات

| PostgreSQL | SQLite المُولَّد |
|-----------|----------------|
| `NOW()` | `DATETIME('now')` |
| `CURRENT_TIMESTAMP` | `DATETIME('now')` |
| `CURRENT_DATE` | `DATE('now')` |
| `CURRENT_TIME` | `TIME('now')` |
| `name ILIKE '%x%'` | `LOWER(name) LIKE LOWER('%x%')` |
| `EXTRACT(YEAR FROM col)` | `CAST(STRFTIME('%Y', col) AS INTEGER)` |
| `EXTRACT(MONTH FROM col)` | `CAST(STRFTIME('%m', col) AS INTEGER)` |
| `EXTRACT(DAY FROM col)` | `CAST(STRFTIME('%d', col) AS INTEGER)` |
| `EXTRACT(HOUR FROM col)` | `CAST(STRFTIME('%H', col) AS INTEGER)` |
| `expr::TYPE` | `CAST(expr AS TYPE)` |
| `$1, $2, $3` | `?, ?, ?` |
| `TRUE` | `1` |
| `FALSE` | `0` |

### على مستوى الجملة

| PostgreSQL | SQLite المُولَّد |
|-----------|----------------|
| `INSERT ... ON CONFLICT DO NOTHING` | `INSERT OR IGNORE ...` |
| `INSERT ... ON CONFLICT DO UPDATE` | `INSERT OR REPLACE ...` |
| `SET client_encoding = 'UTF8'` | `SELECT 1` (no-op) |
| `SET search_path = public` | `SELECT 1` (no-op) |
| `SHOW server_version` | `SELECT '14.5'` |
| `BEGIN TRANSACTION` | `BEGIN` |
| `START TRANSACTION` | `BEGIN` |

---

## 2. أمثلة كاملة مقارنة

### CREATE TABLE

```sql
-- PostgreSQL (ما يرسله العميل)
CREATE TABLE users (
    id         SERIAL PRIMARY KEY,
    username   VARCHAR(50) NOT NULL UNIQUE,
    email      TEXT NOT NULL,
    age        INTEGER,
    score      DOUBLE PRECISION DEFAULT 0.0,
    active     BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMP DEFAULT NOW()
);

-- SQLite (ما يُنفَّذ فعلياً)
CREATE TABLE users (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    username   TEXT NOT NULL UNIQUE,
    email      TEXT NOT NULL,
    age        INTEGER,
    score      REAL DEFAULT 0.0,
    active     INTEGER DEFAULT 1,
    created_at TEXT DEFAULT DATETIME('now')
);
```

### INSERT مع RETURNING

```sql
-- PostgreSQL
INSERT INTO users (username, email) VALUES ($1, $2) RETURNING id, created_at;

-- SQLite (SQLite 3.35+ يدعم RETURNING)
INSERT INTO users (username, email) VALUES (?, ?) RETURNING id, created_at;
```

### Upsert (ON CONFLICT)

```sql
-- PostgreSQL
INSERT INTO counters (key, value)
VALUES ($1, 1)
ON CONFLICT (key) DO UPDATE SET value = counters.value + 1;

-- SQLite
INSERT OR REPLACE INTO counters (key, value)
VALUES (?, 1);
-- ملاحظة: DO UPDATE يُترجَم إلى INSERT OR REPLACE
-- (قد تختلف الدلالات الدقيقة)
```

### ILIKE

```sql
-- PostgreSQL
SELECT * FROM products WHERE name ILIKE '%widget%';

-- SQLite
SELECT * FROM products WHERE LOWER(name) LIKE LOWER('%widget%');
```

### EXTRACT

```sql
-- PostgreSQL
SELECT EXTRACT(YEAR FROM created_at) as year,
       EXTRACT(MONTH FROM created_at) as month,
       COUNT(*) as total
FROM orders
GROUP BY 1, 2;

-- SQLite
SELECT CAST(STRFTIME('%Y', created_at) AS INTEGER) as year,
       CAST(STRFTIME('%m', created_at) AS INTEGER) as month,
       COUNT(*) as total
FROM orders
GROUP BY 1, 2;
```

### Type Cast (`::`)

```sql
-- PostgreSQL
SELECT '42'::INTEGER, '3.14'::FLOAT, '2024-01-01'::DATE;

-- SQLite
SELECT CAST('42' AS INTEGER), CAST('3.14' AS REAL), CAST('2024-01-01' AS TEXT);
```

---

## 3. الكتالوج الافتراضي (Virtual Catalog)

هذه الجداول/الدوال تعمل بدون وجودها فعلياً في SQLite:

### دوال النظام

```sql
-- تُرجع "PostgreSQL 14.5 on SQLite (sqlite-server)"
SELECT version();

-- تُرجع "main"
SELECT current_database();

-- تُرجع "public"
SELECT current_schema();

-- تُرجع رقم عشوائي (PID الجلسة)
SELECT pg_backend_pid();

-- تُرجع اسم المستخدم
SELECT current_user;
SELECT session_user;
```

### information_schema.tables

```sql
-- يُعيد جميع الجداول في قاعدة البيانات
SELECT table_name, table_type
FROM information_schema.tables
WHERE table_schema = 'public'
ORDER BY table_name;
```

### information_schema.columns

```sql
-- يُعيد أعمدة جدول معين
SELECT column_name, data_type, is_nullable, column_default
FROM information_schema.columns
WHERE table_name = 'users'
ORDER BY ordinal_position;
```

### pg_catalog.pg_tables

```sql
-- DBeaver يستخدم هذا للاستعراض
SELECT tablename, tableowner
FROM pg_catalog.pg_tables
WHERE schemaname = 'public';
```

### pg_catalog.pg_class

```sql
SELECT relname, relkind, relnamespace
FROM pg_catalog.pg_class
WHERE relkind IN ('r', 'v', 'i');
-- r=جدول, v=view, i=index
```

---

## 4. الميزات المدعومة مباشرة

هذه تعمل بدون ترجمة (SQLite يدعمها):

```sql
-- CTEs
WITH orders_summary AS (
    SELECT user_id, SUM(total) as total_spent
    FROM orders
    GROUP BY user_id
)
SELECT u.name, os.total_spent
FROM users u
JOIN orders_summary os ON u.id = os.user_id;

-- Window Functions (SQLite 3.25+)
SELECT name, salary,
       ROW_NUMBER() OVER (ORDER BY salary DESC) as rank,
       AVG(salary) OVER () as avg_salary
FROM employees;

-- RETURNING (SQLite 3.35+)
DELETE FROM items WHERE id = 5 RETURNING *;
UPDATE users SET active = 0 WHERE last_login < '2023-01-01' RETURNING id, email;

-- Subqueries
SELECT * FROM users WHERE id IN (
    SELECT DISTINCT user_id FROM orders WHERE total > 100
);

-- CASE WHEN
SELECT name,
       CASE WHEN score > 90 THEN 'A'
            WHEN score > 80 THEN 'B'
            ELSE 'C' END as grade
FROM students;

-- LIKE / GLOB
SELECT * FROM files WHERE name LIKE '%.pdf';
SELECT * FROM files WHERE name GLOB '*.pdf';  -- SQLite-specific

-- JSON functions (SQLite 3.38+)
SELECT json_extract(data, '$.name') FROM records;

-- Aggregate functions
SELECT COUNT(*), SUM(amount), AVG(amount), MIN(amount), MAX(amount)
FROM transactions;
```

---

## 5. القيود (Limitations)

### لا يُدعم

| الميزة | السبب |
|--------|-------|
| `LISTEN / NOTIFY` | ليست في SQLite |
| `CREATE FUNCTION` | ليست في SQLite |
| `CREATE PROCEDURE` | ليست في SQLite |
| `SEQUENCES` (مستقلة) | SQLite لا تدعم sequences |
| `TRIGGERS` على pg schema | ممكنة على SQLite لكن pg syntax مختلف |
| `CREATE EXTENSION` | ليست في SQLite |
| `COPY FROM STDIN` | جزئياً |
| `Multiple databases` | ملف SQLite واحد فقط |
| `SCHEMAS متعددة` | schema واحد (public) |
| `pg_dump restore` | DDL من pg_dump قد يحتوي على syntax غير مدعوم |
| `ARRAY types` | لا يوجد array natively |
| `HSTORE` | ليست في SQLite |
| `GENERATED columns` | محدود |
| `PARTITION BY` (DDL) | ليست في SQLite |

### تختلف دلالياً

| الجانب | PostgreSQL | sqlite-server |
|--------|-----------|--------------|
| Case sensitivity | أسماء الجداول case-insensitive | نفس الشيء في SQLite |
| NULL ordering | NULL first/last قابل للضبط | SQLite: NULLs أصغر من كل القيم |
| String comparison | locale-aware collation | binary بشكل افتراضي |
| Integer overflow | رفع exception | silent wrapping |
| Timezone | كاملة | لا يوجد دعم timezone |

---

## 6. أنواع بيانات PostgreSQL → SQLite OID Mapping

| PostgreSQL | OID | SQLite affinity |
|-----------|-----|----------------|
| boolean | 16 | INTEGER (0/1) |
| bytea | 17 | BLOB |
| smallint | 21 | INTEGER |
| integer | 23 | INTEGER |
| bigint | 20 | INTEGER |
| real | 700 | REAL |
| double precision | 701 | REAL |
| text | 25 | TEXT |
| varchar | 1043 | TEXT |
| date | 1082 | TEXT |
| time | 1083 | TEXT |
| timestamp | 1114 | TEXT |
| timestamptz | 1184 | TEXT |
| uuid | 2950 | TEXT |
| json | 114 | TEXT |
| jsonb | 3802 | TEXT |
| numeric | 1700 | NUMERIC |
