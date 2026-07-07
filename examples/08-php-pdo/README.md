# Example 08 — PHP + PDO (pdo_pgsql)

**Application**: Content Management System (CMS)  
**Language**: PHP 8.1+  
**Driver**: PHP `pdo_pgsql` extension (built-in)

## What It Demonstrates

- `PDO` with `pgsql` DSN connecting to sqlite-server's PostgreSQL wire protocol
- `PDO::ATTR_ERRMODE => PDO::ERRMODE_EXCEPTION` — exceptions on error
- Named placeholders `:param` in `prepare()` + `execute()`
- `PDO::lastInsertId()` to get AUTOINCREMENT IDs
- `fetchAll(PDO::FETCH_ASSOC)` — results as associative arrays
- `bindValue()` with explicit `PDO::PARAM_INT` type
- Transaction: `beginTransaction()` / `commit()` / `rollBack()`
- LIKE search with `LOWER()` for case-insensitive matching
- `LIMIT :limit OFFSET :offset` pagination
- Complex JOIN queries for articles + authors + categories
- PHP 8.1+ named arguments: `get_published_articles($db, page: 1, per_page: 3)`
- Many-to-many tag system (`article_tags` junction table)

## Prerequisites

- PHP 8.1 or newer
- `pdo_pgsql` extension enabled
- sqlite-server running on port 5432

## Check PHP Extensions

```bash
php -m | grep pdo
# Should show: pdo, pdo_pgsql
```

## Run

```bash
php app.php
```

## PowerShell

```powershell
# Check if pdo_pgsql is available
php -m | Select-String "pdo"

# Run the example
php app.php
```

## Enable pdo_pgsql on Windows

Edit `php.ini`:
```ini
; Find and uncomment this line:
extension=pdo_pgsql

; Also ensure:
extension=pgsql
```

Find your `php.ini` location:
```powershell
php --ini
```

## Enable pdo_pgsql on Linux (Ubuntu/Debian)

```bash
sudo apt-get install php8.1-pgsql
sudo phpenmod pdo_pgsql
sudo systemctl restart php8.1-fpm   # if using FPM
```

## Enable pdo_pgsql on macOS (Homebrew)

```bash
brew install php
brew install postgresql
pecl install pdo_pgsql
```

## Start sqlite-server first

```bash
# Linux / macOS
./sqlite-server --addr 127.0.0.1:5432 --no-auth -- cms.db

# Windows PowerShell
.\sqlite-server.exe --addr 127.0.0.1:5432 --no-auth -- cms.db
```

## Connection Config

Edit the constants at the top of `app.php`:

```php
define('DB_DSN',  'pgsql:host=127.0.0.1;port=5432;dbname=cms');
define('DB_USER', 'admin');
define('DB_PASS', 'secret');
```

## Expected Output

```
Content Management System — sqlite-server PHP PDO Example
===========================================================

Setting up schema...
Schema ready.

─────────────────────────────────────────────────────────────────
  8. Published Articles (page 1, 3 per page)
─────────────────────────────────────────────────────────────────
  title                                      author_name    category_name  views
  ─────────────────────────────────────────────────────────────────────────────
  Quantum Computing Breakthroughs of 2024    Dave Brown     Science          612
  Startup Funding in 2025: What Investors …  Carol White    Business         745
  Building Scalable APIs with Go             Bob Smith      Technology       980

─────────────────────────────────────────────────────────────────
  12. Top Articles by Views
─────────────────────────────────────────────────────────────────
  #1  The Rise of Large Language Models           1520 views  (Alice Johnson)
  #2  Building Scalable APIs with Go               980 views  (Bob Smith)
  ...
```

## No Composer Required

This example has zero PHP package dependencies. It uses only PHP's built-in `pdo_pgsql` extension. The `composer.json` documents the requirements but you don't need to run `composer install`.
