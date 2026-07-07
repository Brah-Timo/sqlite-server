// Example 01 — Go (database/sql + lib/pq)
//
// Full CRUD example using the standard Go database/sql interface
// with the lib/pq PostgreSQL driver connecting to sqlite-server.
//
// sqlite-server compatibility:
//   - INTEGER PRIMARY KEY AUTOINCREMENT  (not SERIAL)
//   - TEXT DEFAULT (DATETIME('now'))     (not TIMESTAMP DEFAULT NOW())
//   - INTEGER 0/1                        (not BOOLEAN)
//   - One statement per Exec()           (no multi-statement strings)
//
// Prerequisites:
//   go mod tidy
//
// Run sqlite-server first:
//   ./sqlite-server --no-auth -- users.db
//
// Then run:
//   go run main.go
package main

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	_ "github.com/lib/pq"
)

// User represents a row in the users table.
type User struct {
	ID        int
	Name      string
	Email     string
	Age       int
	Active    bool
	CreatedAt string
}

const dsn = "host=localhost port=5432 user=test password=test dbname=test sslmode=disable connect_timeout=5"

func main() {
	// ── Connect ───────────────────────────────────────────────────────────────
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		log.Fatalf("open: %v", err)
	}
	defer db.Close()

	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)

	if err := db.Ping(); err != nil {
		log.Fatalf("ping: %v\n  Is sqlite-server running? ./sqlite-server --no-auth -- users.db", err)
	}
	fmt.Println("✓ Connected to sqlite-server")

	// ── Create table ──────────────────────────────────────────────────────────
	// Note: INTEGER PRIMARY KEY AUTOINCREMENT, not SERIAL
	//       TEXT DEFAULT (DATETIME('now')), not TIMESTAMP DEFAULT NOW()
	//       INTEGER for active, not BOOLEAN
	must(db.Exec(`DROP TABLE IF EXISTS users`), "drop table")

	must(db.Exec(`
		CREATE TABLE users (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			name       TEXT    NOT NULL,
			email      TEXT    NOT NULL UNIQUE,
			age        INTEGER NOT NULL DEFAULT 0,
			active     INTEGER NOT NULL DEFAULT 1,
			created_at TEXT    DEFAULT (DATETIME('now'))
		)
	`), "create table")
	fmt.Println("✓ Table 'users' created")

	// ── INSERT ────────────────────────────────────────────────────────────────
	seeds := []struct {
		name  string
		email string
		age   int
	}{
		{"Alice Johnson", "alice@example.com", 30},
		{"Bob Smith",     "bob@example.com",   25},
		{"Carol White",   "carol@example.com", 35},
		{"Dave Brown",    "dave@example.com",  28},
		{"Eve Davis",     "eve@example.com",   22},
	}

	for _, s := range seeds {
		var id int
		err = db.QueryRow(
			`INSERT INTO users (name, email, age) VALUES ($1, $2, $3) RETURNING id`,
			s.name, s.email, s.age,
		).Scan(&id)
		must2(err, "insert")
		fmt.Printf("  + Inserted user id=%d  name=%q\n", id, s.name)
	}

	// ── SELECT all ────────────────────────────────────────────────────────────
	fmt.Println("\n── All users ────────────────────────────────")
	rows, err := db.Query(`SELECT id, name, email, age, active FROM users ORDER BY id`)
	must2(err, "select")
	defer rows.Close()

	for rows.Next() {
		var u User
		var active int // sqlite stores as INTEGER
		must2(rows.Scan(&u.ID, &u.Name, &u.Email, &u.Age, &active), "scan")
		u.Active = active == 1
		fmt.Printf("  [%d] %-20s  %-28s  age=%-3d  active=%v\n",
			u.ID, u.Name, u.Email, u.Age, u.Active)
	}
	must2(rows.Err(), "rows")

	// ── SELECT with WHERE + parameter ─────────────────────────────────────────
	fmt.Println("\n── Users older than 27 ──────────────────────")
	rows2, err := db.Query(
		`SELECT id, name, age FROM users WHERE age > $1 ORDER BY age DESC`, 27,
	)
	must2(err, "select where")
	defer rows2.Close()
	for rows2.Next() {
		var id, age int
		var name string
		must2(rows2.Scan(&id, &name, &age), "scan")
		fmt.Printf("  [%d] %s  (age %d)\n", id, name, age)
	}

	// ── UPDATE ────────────────────────────────────────────────────────────────
	res, err := db.Exec(`UPDATE users SET active = 0 WHERE age < $1`, 25)
	must2(err, "update")
	affected, _ := res.RowsAffected()
	fmt.Printf("\n── Updated %d row(s) (active=0 where age<25)\n", affected)

	// ── TRANSACTION ───────────────────────────────────────────────────────────
	fmt.Println("\n── Transaction: bulk insert ─────────────────")
	tx, err := db.Begin()
	must2(err, "begin tx")

	extraUsers := []string{"Frank", "Grace", "Heidi"}
	for i, name := range extraUsers {
		email := fmt.Sprintf("%s@example.com", name)
		_, err = tx.Exec(
			`INSERT INTO users (name, email, age) VALUES ($1, $2, $3)`,
			name, email, 20+i,
		)
		if err != nil {
			tx.Rollback()
			log.Fatalf("insert in tx: %v", err)
		}
		fmt.Printf("  + tx: inserted %s\n", name)
	}
	must2(tx.Commit(), "commit")
	fmt.Println("  ✓ Transaction committed")

	// ── COUNT ─────────────────────────────────────────────────────────────────
	var total int
	must2(db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&total), "count")
	fmt.Printf("\n── Total users: %d\n", total)

	// ── DELETE ────────────────────────────────────────────────────────────────
	res, err = db.Exec(`DELETE FROM users WHERE name = $1`, "Heidi")
	must2(err, "delete")
	deleted, _ := res.RowsAffected()
	fmt.Printf("── Deleted %d user(s) named 'Heidi'\n", deleted)

	// ── AGGREGATE ─────────────────────────────────────────────────────────────
	var minAge, maxAge int
	var avgAge float64
	must2(
		db.QueryRow(`SELECT MIN(age), MAX(age), AVG(age) FROM users`).
			Scan(&minAge, &maxAge, &avgAge),
		"agg",
	)
	fmt.Printf("── Age stats: min=%d  max=%d  avg=%.1f\n", minAge, maxAge, avgAge)

	// ── Prepared statement ────────────────────────────────────────────────────
	fmt.Println("\n── Prepared statement: find active users ────")
	stmt, err := db.Prepare(`SELECT id, name, age FROM users WHERE active = $1 ORDER BY age`)
	must2(err, "prepare")
	defer stmt.Close()

	activeRows, err := stmt.Query(1)
	must2(err, "prepared query")
	defer activeRows.Close()
	for activeRows.Next() {
		var id, age int
		var name string
		must2(activeRows.Scan(&id, &name, &age), "scan")
		fmt.Printf("  [%d] %-20s  age=%d\n", id, name, age)
	}

	// ── DROP ──────────────────────────────────────────────────────────────────
	must(db.Exec(`DROP TABLE IF EXISTS users`), "drop")
	fmt.Println("\n✓ Done — table dropped")
}

func must(res sql.Result, label string) {
	// used when we only care about errors, not result
}

func must2(err error, label string) {
	if err != nil {
		log.Fatalf("%s: %v", label, err)
	}
}
