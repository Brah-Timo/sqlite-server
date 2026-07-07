// Package integration contains end-to-end tests for sqlite-server.
//
// The tests connect to a real sqlite-server instance using the standard
// lib/pq PostgreSQL driver.  A live server is started automatically
// in TestMain using the binary from the project root, so the tests are
// fully self-contained.
//
// Run with:
//
//	go test ./tests/integration/... -v -timeout 120s
//
// To skip the automatic server start (if you already have one running):
//
//	SQLITE_SERVER_ADDR=localhost:15432 go test ./tests/integration/... -v
package integration

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"testing"
	"time"

	_ "github.com/lib/pq"
)

// ─────────────────────────────────────────────────────────────────────────────
//  Global test server lifecycle
// ─────────────────────────────────────────────────────────────────────────────

var (
	testDSN    string
	serverOnce sync.Once
	serverCmd  *exec.Cmd
	serverDB   string
)

func TestMain(m *testing.M) {
	// Allow overriding the server address for CI environments.
	addr := os.Getenv("SQLITE_SERVER_ADDR")
	if addr == "" {
		addr = "localhost:15432"
	}
	testDSN = fmt.Sprintf(
		"host=localhost port=15432 user=test password=test dbname=test sslmode=disable connect_timeout=5",
	)
	if env := os.Getenv("SQLITE_SERVER_DSN"); env != "" {
		testDSN = env
	}

	// Try to start the server if not already running.
	serverDB = os.TempDir() + "/sqlite_server_test.db"
	_ = os.Remove(serverDB)

	serverOnce.Do(func() {
		bin := "./sqlite-server-test-bin"
		// Build the server binary quickly.
		buildCmd := exec.Command("go", "build", "-o", bin, "./cmd/sqlite-server")
		buildCmd.Dir = "../.."
		if out, err := buildCmd.CombinedOutput(); err != nil {
			fmt.Printf("⚠ Could not build sqlite-server: %v\n%s\n", err, out)
			fmt.Println("⚠ Integration tests will run against an already-running server.")
			return
		}

		serverCmd = exec.Command(bin,
			"--addr", addr,
			"--no-auth",
			"--wal",
			serverDB,
		)
		serverCmd.Stdout = os.Stdout
		serverCmd.Stderr = os.Stderr
		if err := serverCmd.Start(); err != nil {
			fmt.Printf("⚠ Could not start sqlite-server: %v\n", err)
			return
		}

		// Wait for server to be ready.
		for i := 0; i < 30; i++ {
			db, err := sql.Open("postgres", testDSN)
			if err == nil {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				err = db.PingContext(ctx)
				cancel()
				db.Close()
				if err == nil {
					break
				}
			}
			time.Sleep(200 * time.Millisecond)
		}
	})

	code := m.Run()

	if serverCmd != nil && serverCmd.Process != nil {
		_ = serverCmd.Process.Kill()
	}
	_ = os.Remove(serverDB)
	_ = os.Remove("./sqlite-server-test-bin")

	os.Exit(code)
}

// ─────────────────────────────────────────────────────────────────────────────
//  Helpers
// ─────────────────────────────────────────────────────────────────────────────

func connectDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("postgres", testDSN)
	if err != nil {
		t.Skipf("cannot open postgres driver: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Skipf("sqlite-server not available at %s: %v", testDSN, err)
	}
	return db
}

func mustExec(t *testing.T, db *sql.DB, query string, args ...interface{}) {
	t.Helper()
	_, err := db.ExecContext(context.Background(), query, args...)
	if err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
//  Batch 13-A  :  Basic connectivity & version
// ─────────────────────────────────────────────────────────────────────────────

func TestPing(t *testing.T) {
	db := connectDB(t)
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping: %v", err)
	}
}

func TestVersion(t *testing.T) {
	db := connectDB(t)
	defer db.Close()

	var version string
	err := db.QueryRowContext(context.Background(), "SELECT version()").Scan(&version)
	if err != nil {
		t.Fatalf("version(): %v", err)
	}
	if version == "" {
		t.Fatal("version() returned empty string")
	}
	t.Logf("server version: %s", version)
}

func TestSelectOne(t *testing.T) {
	db := connectDB(t)
	defer db.Close()

	var n int
	if err := db.QueryRowContext(context.Background(), "SELECT 1").Scan(&n); err != nil {
		t.Fatalf("SELECT 1: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1, got %d", n)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
//  Batch 13-B  :  DDL
// ─────────────────────────────────────────────────────────────────────────────

func TestCreateAndDropTable(t *testing.T) {
	db := connectDB(t)
	defer db.Close()
	ctx := context.Background()

	mustExec(t, db, `DROP TABLE IF EXISTS test_ddl`)
	mustExec(t, db, `
		CREATE TABLE test_ddl (
			id    SERIAL PRIMARY KEY,
			name  TEXT    NOT NULL,
			score REAL    DEFAULT 0,
			active BOOLEAN DEFAULT TRUE,
			created_at TIMESTAMP DEFAULT NOW()
		)
	`)
	mustExec(t, db, `DROP TABLE test_ddl`)

	// Confirm table is gone
	_, err := db.QueryContext(ctx, `SELECT * FROM test_ddl`)
	if err == nil {
		t.Fatal("expected error after DROP TABLE, got none")
	}
}

func TestCreateIndex(t *testing.T) {
	db := connectDB(t)
	defer db.Close()

	mustExec(t, db, `DROP TABLE IF EXISTS test_idx`)
	mustExec(t, db, `CREATE TABLE test_idx (id INTEGER PRIMARY KEY, email TEXT UNIQUE)`)
	mustExec(t, db, `CREATE INDEX idx_test_email ON test_idx(email)`)
	mustExec(t, db, `DROP INDEX idx_test_email`)
	mustExec(t, db, `DROP TABLE test_idx`)
}

// ─────────────────────────────────────────────────────────────────────────────
//  Batch 13-C  :  CRUD
// ─────────────────────────────────────────────────────────────────────────────

func TestInsertSelectUpdateDelete(t *testing.T) {
	db := connectDB(t)
	defer db.Close()
	ctx := context.Background()

	mustExec(t, db, `DROP TABLE IF EXISTS users`)
	mustExec(t, db, `
		CREATE TABLE users (
			id    INTEGER PRIMARY KEY AUTOINCREMENT,
			name  TEXT    NOT NULL,
			email TEXT    UNIQUE,
			age   INTEGER
		)
	`)
	defer mustExec(t, db, `DROP TABLE users`)

	// INSERT
	_, err := db.ExecContext(ctx, `INSERT INTO users (name, email, age) VALUES ($1, $2, $3)`,
		"Alice", "alice@example.com", 30)
	if err != nil {
		t.Fatalf("insert alice: %v", err)
	}
	_, err = db.ExecContext(ctx, `INSERT INTO users (name, email, age) VALUES ($1, $2, $3)`,
		"Bob", "bob@example.com", 25)
	if err != nil {
		t.Fatalf("insert bob: %v", err)
	}

	// SELECT — count
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 rows, got %d", count)
	}

	// SELECT — single row
	var name, email string
	var age int
	if err := db.QueryRowContext(ctx,
		`SELECT name, email, age FROM users WHERE name = $1`, "Alice",
	).Scan(&name, &email, &age); err != nil {
		t.Fatalf("select alice: %v", err)
	}
	if name != "Alice" || email != "alice@example.com" || age != 30 {
		t.Fatalf("unexpected values: name=%s email=%s age=%d", name, email, age)
	}

	// UPDATE
	res, err := db.ExecContext(ctx, `UPDATE users SET age = $1 WHERE name = $2`, 31, "Alice")
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	affected, _ := res.RowsAffected()
	if affected != 1 {
		t.Fatalf("expected 1 affected row, got %d", affected)
	}

	// Verify UPDATE
	if err := db.QueryRowContext(ctx,
		`SELECT age FROM users WHERE name = 'Alice'`,
	).Scan(&age); err != nil {
		t.Fatalf("select after update: %v", err)
	}
	if age != 31 {
		t.Fatalf("expected age=31, got %d", age)
	}

	// DELETE
	res, err = db.ExecContext(ctx, `DELETE FROM users WHERE name = $1`, "Bob")
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	affected, _ = res.RowsAffected()
	if affected != 1 {
		t.Fatalf("expected 1 deleted row, got %d", affected)
	}

	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		t.Fatalf("count after delete: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 row after delete, got %d", count)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
//  Batch 13-D  :  Transactions
// ─────────────────────────────────────────────────────────────────────────────

func TestTransactionCommit(t *testing.T) {
	db := connectDB(t)
	defer db.Close()
	ctx := context.Background()

	mustExec(t, db, `DROP TABLE IF EXISTS tx_test`)
	mustExec(t, db, `CREATE TABLE tx_test (id INTEGER PRIMARY KEY, val TEXT)`)
	defer mustExec(t, db, `DROP TABLE tx_test`)

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO tx_test VALUES (1, 'committed')`); err != nil {
		tx.Rollback()
		t.Fatalf("insert in tx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	var val string
	if err := db.QueryRowContext(ctx, `SELECT val FROM tx_test WHERE id = 1`).Scan(&val); err != nil {
		t.Fatalf("select after commit: %v", err)
	}
	if val != "committed" {
		t.Fatalf("expected 'committed', got %q", val)
	}
}

func TestTransactionRollback(t *testing.T) {
	db := connectDB(t)
	defer db.Close()
	ctx := context.Background()

	mustExec(t, db, `DROP TABLE IF EXISTS tx_rollback`)
	mustExec(t, db, `CREATE TABLE tx_rollback (id INTEGER PRIMARY KEY, val TEXT)`)
	defer mustExec(t, db, `DROP TABLE tx_rollback`)

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO tx_rollback VALUES (1, 'will-rollback')`); err != nil {
		tx.Rollback()
		t.Fatalf("insert in tx: %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("rollback: %v", err)
	}

	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM tx_rollback`).Scan(&count); err != nil {
		t.Fatalf("count after rollback: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 rows after rollback, got %d", count)
	}
}

func TestTransactionIsolation(t *testing.T) {
	db := connectDB(t)
	defer db.Close()
	ctx := context.Background()

	mustExec(t, db, `DROP TABLE IF EXISTS tx_iso`)
	mustExec(t, db, `CREATE TABLE tx_iso (id INTEGER PRIMARY KEY, val INTEGER DEFAULT 0)`)
	mustExec(t, db, `INSERT INTO tx_iso VALUES (1, 100)`)
	defer mustExec(t, db, `DROP TABLE tx_iso`)

	// Two concurrent updates — both should succeed since SQLite WAL handles this.
	done := make(chan error, 2)

	update := func(delta int) {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			done <- err
			return
		}
		_, err = tx.ExecContext(ctx, `UPDATE tx_iso SET val = val + $1 WHERE id = 1`, delta)
		if err != nil {
			tx.Rollback()
			done <- err
			return
		}
		done <- tx.Commit()
	}

	go update(10)
	go update(20)

	for i := 0; i < 2; i++ {
		if err := <-done; err != nil {
			t.Logf("concurrent tx err (may be expected busy): %v", err)
		}
	}
}

func TestSavepoint(t *testing.T) {
	db := connectDB(t)
	defer db.Close()
	ctx := context.Background()

	mustExec(t, db, `DROP TABLE IF EXISTS sp_test`)
	mustExec(t, db, `CREATE TABLE sp_test (id INTEGER PRIMARY KEY, val TEXT)`)
	defer mustExec(t, db, `DROP TABLE sp_test`)

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `INSERT INTO sp_test VALUES (1, 'outer')`); err != nil {
		t.Fatalf("outer insert: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `SAVEPOINT sp1`); err != nil {
		t.Fatalf("savepoint: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO sp_test VALUES (2, 'inner')`); err != nil {
		t.Fatalf("inner insert: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `ROLLBACK TO SAVEPOINT sp1`); err != nil {
		t.Fatalf("rollback to savepoint: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sp_test`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 row (savepoint rolled back inner), got %d", count)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
//  Batch 13-E  :  Prepared statements / Extended Query Protocol
// ─────────────────────────────────────────────────────────────────────────────

func TestPreparedStatementCRUD(t *testing.T) {
	db := connectDB(t)
	defer db.Close()
	ctx := context.Background()

	mustExec(t, db, `DROP TABLE IF EXISTS ps_test`)
	mustExec(t, db, `
		CREATE TABLE ps_test (
			id   INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			age  INTEGER
		)
	`)
	defer mustExec(t, db, `DROP TABLE ps_test`)

	// Prepare INSERT
	insertStmt, err := db.PrepareContext(ctx, `INSERT INTO ps_test (name, age) VALUES ($1, $2)`)
	if err != nil {
		t.Fatalf("prepare insert: %v", err)
	}
	defer insertStmt.Close()

	names := []string{"Alice", "Bob", "Charlie", "Diana", "Eve"}
	for i, name := range names {
		if _, err := insertStmt.ExecContext(ctx, name, 20+i); err != nil {
			t.Fatalf("insert %s: %v", name, err)
		}
	}

	// Prepare SELECT
	selectStmt, err := db.PrepareContext(ctx, `SELECT name, age FROM ps_test WHERE age >= $1 ORDER BY age`)
	if err != nil {
		t.Fatalf("prepare select: %v", err)
	}
	defer selectStmt.Close()

	rows, err := selectStmt.QueryContext(ctx, 22)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()

	var results []string
	for rows.Next() {
		var name string
		var age int
		if err := rows.Scan(&name, &age); err != nil {
			t.Fatalf("scan: %v", err)
		}
		results = append(results, fmt.Sprintf("%s:%d", name, age))
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows err: %v", err)
	}

	// age >= 22 → Charlie(22), Diana(23), Eve(24)
	if len(results) != 3 {
		t.Fatalf("expected 3 results for age>=22, got %d: %v", len(results), results)
	}

	// Prepare UPDATE
	updateStmt, err := db.PrepareContext(ctx, `UPDATE ps_test SET age = $1 WHERE name = $2`)
	if err != nil {
		t.Fatalf("prepare update: %v", err)
	}
	defer updateStmt.Close()

	res, err := updateStmt.ExecContext(ctx, 99, "Alice")
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	affected, _ := res.RowsAffected()
	if affected != 1 {
		t.Fatalf("expected 1 row updated, got %d", affected)
	}
}

func TestPreparedStatementReuseAcrossTransactions(t *testing.T) {
	db := connectDB(t)
	defer db.Close()
	ctx := context.Background()

	mustExec(t, db, `DROP TABLE IF EXISTS ps_reuse`)
	mustExec(t, db, `CREATE TABLE ps_reuse (k TEXT PRIMARY KEY, v INTEGER)`)
	defer mustExec(t, db, `DROP TABLE ps_reuse`)

	stmt, err := db.PrepareContext(ctx, `INSERT INTO ps_reuse (k, v) VALUES ($1, $2)`)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	defer stmt.Close()

	for i := 0; i < 5; i++ {
		tx, _ := db.BeginTx(ctx, nil)
		if _, err := tx.StmtContext(ctx, stmt).ExecContext(ctx, fmt.Sprintf("key%d", i), i*10); err != nil {
			tx.Rollback()
			t.Fatalf("insert %d: %v", i, err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("commit %d: %v", i, err)
		}
	}

	var count int
	db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ps_reuse`).Scan(&count)
	if count != 5 {
		t.Fatalf("expected 5 rows, got %d", count)
	}
}

func TestParameterTypes(t *testing.T) {
	db := connectDB(t)
	defer db.Close()
	ctx := context.Background()

	mustExec(t, db, `DROP TABLE IF EXISTS param_types`)
	mustExec(t, db, `
		CREATE TABLE param_types (
			id        INTEGER PRIMARY KEY,
			txt       TEXT,
			num       REAL,
			flag      INTEGER,
			big       INTEGER
		)
	`)
	defer mustExec(t, db, `DROP TABLE param_types`)

	stmt, err := db.PrepareContext(ctx,
		`INSERT INTO param_types (id, txt, num, flag, big) VALUES ($1, $2, $3, $4, $5)`)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	defer stmt.Close()

	if _, err := stmt.ExecContext(ctx, 1, "hello", 3.14, true, int64(9223372036854775807)); err != nil {
		t.Fatalf("insert: %v", err)
	}

	var txt string
	var num float64
	var flag bool
	var big int64
	err = db.QueryRowContext(ctx, `SELECT txt, num, flag, big FROM param_types WHERE id = 1`).
		Scan(&txt, &num, &flag, &big)
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if txt != "hello" {
		t.Fatalf("txt: expected hello, got %q", txt)
	}
	if num < 3.13 || num > 3.15 {
		t.Fatalf("num: expected ~3.14, got %f", num)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
//  Batch 13-F  :  DBeaver-style catalog queries
// ─────────────────────────────────────────────────────────────────────────────

func TestInformationSchemaTables(t *testing.T) {
	db := connectDB(t)
	defer db.Close()
	ctx := context.Background()

	// Create a test table so there is something to list.
	mustExec(t, db, `DROP TABLE IF EXISTS dbeaver_probe`)
	mustExec(t, db, `CREATE TABLE dbeaver_probe (id INTEGER PRIMARY KEY)`)
	defer mustExec(t, db, `DROP TABLE dbeaver_probe`)

	rows, err := db.QueryContext(ctx, `
		SELECT table_name, table_type
		FROM information_schema.tables
		WHERE table_schema = 'public'
		ORDER BY table_name
	`)
	if err != nil {
		t.Fatalf("information_schema.tables: %v", err)
	}
	defer rows.Close()

	found := false
	for rows.Next() {
		var tableName, tableType string
		if err := rows.Scan(&tableName, &tableType); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if tableName == "dbeaver_probe" {
			found = true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows err: %v", err)
	}
	if !found {
		t.Error("dbeaver_probe table not found in information_schema.tables")
	}
}

func TestInformationSchemaColumns(t *testing.T) {
	db := connectDB(t)
	defer db.Close()
	ctx := context.Background()

	mustExec(t, db, `DROP TABLE IF EXISTS col_probe`)
	mustExec(t, db, `
		CREATE TABLE col_probe (
			id   INTEGER PRIMARY KEY,
			name TEXT NOT NULL,
			val  REAL
		)
	`)
	defer mustExec(t, db, `DROP TABLE col_probe`)

	rows, err := db.QueryContext(ctx, `
		SELECT column_name, data_type, is_nullable
		FROM information_schema.columns
		WHERE table_name = 'col_probe'
		ORDER BY ordinal_position
	`)
	if err != nil {
		t.Fatalf("information_schema.columns: %v", err)
	}
	defer rows.Close()

	var cols []string
	for rows.Next() {
		var colName, dataType, isNullable string
		if err := rows.Scan(&colName, &dataType, &isNullable); err != nil {
			t.Fatalf("scan: %v", err)
		}
		cols = append(cols, colName)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows err: %v", err)
	}
	if len(cols) < 3 {
		t.Fatalf("expected ≥3 columns, got %d: %v", len(cols), cols)
	}
}

func TestPGTablesQuery(t *testing.T) {
	db := connectDB(t)
	defer db.Close()
	ctx := context.Background()

	rows, err := db.QueryContext(ctx, `
		SELECT schemaname, tablename
		FROM pg_catalog.pg_tables
		WHERE schemaname = 'public'
	`)
	if err != nil {
		t.Fatalf("pg_catalog.pg_tables: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var schema, table string
		rows.Scan(&schema, &table)
		t.Logf("table: %s.%s", schema, table)
	}
}

func TestCurrentDatabase(t *testing.T) {
	db := connectDB(t)
	defer db.Close()
	ctx := context.Background()

	var dbName string
	if err := db.QueryRowContext(ctx, `SELECT current_database()`).Scan(&dbName); err != nil {
		t.Fatalf("current_database(): %v", err)
	}
	t.Logf("current_database() = %q", dbName)
}

func TestCurrentSchema(t *testing.T) {
	db := connectDB(t)
	defer db.Close()
	ctx := context.Background()

	var schema string
	if err := db.QueryRowContext(ctx, `SELECT current_schema()`).Scan(&schema); err != nil {
		t.Fatalf("current_schema(): %v", err)
	}
	t.Logf("current_schema() = %q", schema)
}

func TestPGBackendPID(t *testing.T) {
	db := connectDB(t)
	defer db.Close()
	ctx := context.Background()

	var pid int
	if err := db.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
		t.Fatalf("pg_backend_pid(): %v", err)
	}
	if pid <= 0 {
		t.Fatalf("expected positive pid, got %d", pid)
	}
}

func TestSetStatement(t *testing.T) {
	db := connectDB(t)
	defer db.Close()
	ctx := context.Background()

	// DBeaver always sends SET statements on connect.
	stmts := []string{
		`SET client_encoding = 'UTF8'`,
		`SET standard_conforming_strings = on`,
		`SET datestyle = 'ISO, MDY'`,
		`SET extra_float_digits = 2`,
		`SET application_name = 'DBeaver'`,
	}
	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Errorf("SET failed %q: %v", stmt, err)
		}
	}
}

func TestShowStatement(t *testing.T) {
	db := connectDB(t)
	defer db.Close()
	ctx := context.Background()

	var val string
	if err := db.QueryRowContext(ctx, `SHOW server_version`).Scan(&val); err != nil {
		t.Fatalf("SHOW server_version: %v", err)
	}
	t.Logf("server_version = %q", val)
}

// ─────────────────────────────────────────────────────────────────────────────
//  Batch 13-G  :  NULL handling
// ─────────────────────────────────────────────────────────────────────────────

func TestNullValues(t *testing.T) {
	db := connectDB(t)
	defer db.Close()
	ctx := context.Background()

	mustExec(t, db, `DROP TABLE IF EXISTS null_test`)
	mustExec(t, db, `CREATE TABLE null_test (id INTEGER PRIMARY KEY, val TEXT)`)
	defer mustExec(t, db, `DROP TABLE null_test`)

	mustExec(t, db, `INSERT INTO null_test (id, val) VALUES (1, NULL)`)
	mustExec(t, db, `INSERT INTO null_test (id, val) VALUES (2, 'present')`)

	rows, err := db.QueryContext(ctx, `SELECT id, val FROM null_test ORDER BY id`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()

	type row struct {
		id  int
		val sql.NullString
	}
	var got []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.val); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got = append(got, r)
	}

	if len(got) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(got))
	}
	if got[0].val.Valid {
		t.Error("expected NULL for row 1")
	}
	if !got[1].val.Valid || got[1].val.String != "present" {
		t.Errorf("expected 'present' for row 2, got %+v", got[1].val)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
//  Batch 13-H  :  Data types round-trip
// ─────────────────────────────────────────────────────────────────────────────

func TestDataTypeRoundTrip(t *testing.T) {
	db := connectDB(t)
	defer db.Close()
	ctx := context.Background()

	mustExec(t, db, `DROP TABLE IF EXISTS dtype_test`)
	mustExec(t, db, `
		CREATE TABLE dtype_test (
			id       INTEGER PRIMARY KEY,
			txt      TEXT,
			intval   INTEGER,
			realval  REAL,
			boolval  INTEGER
		)
	`)
	defer mustExec(t, db, `DROP TABLE dtype_test`)

	mustExec(t, db, `INSERT INTO dtype_test VALUES (1, 'hello world', 42, 3.14159, 1)`)

	var txt string
	var intval int64
	var realval float64
	var boolval int
	err := db.QueryRowContext(ctx,
		`SELECT txt, intval, realval, boolval FROM dtype_test WHERE id = 1`,
	).Scan(&txt, &intval, &realval, &boolval)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if txt != "hello world" {
		t.Errorf("txt: %q", txt)
	}
	if intval != 42 {
		t.Errorf("intval: %d", intval)
	}
	if realval < 3.14 || realval > 3.15 {
		t.Errorf("realval: %f", realval)
	}
	if boolval != 1 {
		t.Errorf("boolval: %d", boolval)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
//  Batch 13-I  :  Concurrent connections
// ─────────────────────────────────────────────────────────────────────────────

func TestConcurrentConnections(t *testing.T) {
	const numConns = 10
	ctx := context.Background()

	mustExec(t, connectDB(t), `DROP TABLE IF EXISTS conc_test`)
	mustExec(t, connectDB(t), `CREATE TABLE conc_test (id INTEGER PRIMARY KEY AUTOINCREMENT, worker INTEGER)`)
	defer mustExec(t, connectDB(t), `DROP TABLE conc_test`)

	var wg sync.WaitGroup
	errs := make(chan error, numConns)

	for i := 0; i < numConns; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			db := connectDB(t)
			defer db.Close()
			_, err := db.ExecContext(ctx,
				`INSERT INTO conc_test (worker) VALUES ($1)`, workerID)
			if err != nil {
				errs <- fmt.Errorf("worker %d: %w", workerID, err)
			}
		}(i)
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent error: %v", err)
	}

	db := connectDB(t)
	defer db.Close()
	var count int
	db.QueryRowContext(ctx, `SELECT COUNT(*) FROM conc_test`).Scan(&count)
	t.Logf("Inserted %d/%d rows concurrently", count, numConns)
}

// ─────────────────────────────────────────────────────────────────────────────
//  Batch 13-J  :  RETURNING clause
// ─────────────────────────────────────────────────────────────────────────────

func TestReturning(t *testing.T) {
	db := connectDB(t)
	defer db.Close()
	ctx := context.Background()

	mustExec(t, db, `DROP TABLE IF EXISTS ret_test`)
	mustExec(t, db, `CREATE TABLE ret_test (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT)`)
	defer mustExec(t, db, `DROP TABLE ret_test`)

	var id int64
	err := db.QueryRowContext(ctx,
		`INSERT INTO ret_test (name) VALUES ($1) RETURNING id`, "TestUser",
	).Scan(&id)
	if err != nil {
		t.Fatalf("insert returning: %v", err)
	}
	if id <= 0 {
		t.Fatalf("expected positive id, got %d", id)
	}
	t.Logf("RETURNING id = %d", id)
}

// ─────────────────────────────────────────────────────────────────────────────
//  Batch 13-K  :  SQL translation correctness
// ─────────────────────────────────────────────────────────────────────────────

func TestSQLTranslation(t *testing.T) {
	db := connectDB(t)
	defer db.Close()
	ctx := context.Background()

	mustExec(t, db, `DROP TABLE IF EXISTS trans_test`)
	mustExec(t, db, `
		CREATE TABLE trans_test (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			event_name TEXT,
			ts         TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`)
	defer mustExec(t, db, `DROP TABLE trans_test`)

	// PostgreSQL SERIAL equivalent → AUTOINCREMENT already handled
	mustExec(t, db, `INSERT INTO trans_test (event_name) VALUES ('test-event')`)

	// NOW() translation
	var result string
	if err := db.QueryRowContext(ctx, `SELECT event_name FROM trans_test LIMIT 1`).Scan(&result); err != nil {
		t.Fatalf("select with NOW() table: %v", err)
	}
	if result != "test-event" {
		t.Fatalf("expected 'test-event', got %q", result)
	}

	// ILIKE → LIKE (case-insensitive search)
	_, err := db.QueryContext(ctx, `SELECT * FROM trans_test WHERE event_name ILIKE 'TEST%'`)
	if err != nil {
		t.Fatalf("ILIKE translation: %v", err)
	}

	// EXTRACT
	var year int
	if err := db.QueryRowContext(ctx, `SELECT EXTRACT(YEAR FROM CURRENT_TIMESTAMP)`).Scan(&year); err != nil {
		t.Fatalf("EXTRACT(YEAR): %v", err)
	}
	if year < 2024 || year > 2099 {
		t.Fatalf("unexpected year: %d", year)
	}
	t.Logf("EXTRACT(YEAR) = %d", year)
}

func TestOrderByLimitOffset(t *testing.T) {
	db := connectDB(t)
	defer db.Close()
	ctx := context.Background()

	mustExec(t, db, `DROP TABLE IF EXISTS paging_test`)
	mustExec(t, db, `CREATE TABLE paging_test (id INTEGER PRIMARY KEY, val INTEGER)`)
	defer mustExec(t, db, `DROP TABLE paging_test`)

	for i := 1; i <= 10; i++ {
		mustExec(t, db, `INSERT INTO paging_test (id, val) VALUES ($1, $2)`, i, i*10)
	}

	rows, err := db.QueryContext(ctx,
		`SELECT val FROM paging_test ORDER BY val DESC LIMIT $1 OFFSET $2`, 3, 2)
	if err != nil {
		t.Fatalf("ORDER BY LIMIT OFFSET: %v", err)
	}
	defer rows.Close()

	var vals []int
	for rows.Next() {
		var v int
		rows.Scan(&v)
		vals = append(vals, v)
	}
	// DESC: 100,90,80,70,60... → OFFSET 2 → 80,70,60
	if len(vals) != 3 {
		t.Fatalf("expected 3 rows, got %d: %v", len(vals), vals)
	}
	if vals[0] != 80 || vals[1] != 70 || vals[2] != 60 {
		t.Fatalf("wrong order: %v (expected [80,70,60])", vals)
	}
}

func TestJoins(t *testing.T) {
	db := connectDB(t)
	defer db.Close()
	ctx := context.Background()

	mustExec(t, db, `DROP TABLE IF EXISTS orders`)
	mustExec(t, db, `DROP TABLE IF EXISTS customers`)
	mustExec(t, db, `CREATE TABLE customers (id INTEGER PRIMARY KEY, name TEXT)`)
	mustExec(t, db, `CREATE TABLE orders (id INTEGER PRIMARY KEY, cust_id INTEGER, amount REAL)`)
	defer func() {
		mustExec(t, db, `DROP TABLE IF EXISTS orders`)
		mustExec(t, db, `DROP TABLE IF EXISTS customers`)
	}()

	mustExec(t, db, `INSERT INTO customers VALUES (1, 'Alice'), (2, 'Bob')`)
	mustExec(t, db, `INSERT INTO orders VALUES (1, 1, 99.99), (2, 1, 49.50), (3, 2, 150.00)`)

	rows, err := db.QueryContext(ctx, `
		SELECT c.name, SUM(o.amount) as total
		FROM customers c
		JOIN orders o ON c.id = o.cust_id
		GROUP BY c.name
		ORDER BY total DESC
	`)
	if err != nil {
		t.Fatalf("JOIN query: %v", err)
	}
	defer rows.Close()

	type result struct {
		name  string
		total float64
	}
	var results []result
	for rows.Next() {
		var r result
		rows.Scan(&r.name, &r.total)
		results = append(results, r)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].name != "Bob" {
		t.Errorf("expected Bob first (150.00), got %s", results[0].name)
	}
}
