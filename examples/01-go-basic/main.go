// Example 01 — Go (database/sql + lib/pq)
//
// Full CRUD example using the standard Go database/sql interface
// with the lib/pq PostgreSQL driver.
//
// Prerequisites:
//   go mod init example-go-basic
//   go get github.com/lib/pq
//
// Run sqlite-server first:
//   ./sqlite-server --no-auth -- /tmp/demo.db
//
// Then run this example:
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
	CreatedAt time.Time
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
		log.Fatalf("ping: %v\n  Is sqlite-server running? ./sqlite-server --no-auth -- /tmp/demo.db", err)
	}
	fmt.Println("✓ Connected to sqlite-server")

	// ── Create table ──────────────────────────────────────────────────────────
	_, err = db.Exec(`DROP TABLE IF EXISTS users`)
	must(err, "drop table")

	_, err = db.Exec(`
		CREATE TABLE users (
			id         SERIAL PRIMARY KEY,
			name       TEXT    NOT NULL,
			email      TEXT    NOT NULL UNIQUE,
			age        INTEGER NOT NULL DEFAULT 0,
			active     BOOLEAN NOT NULL DEFAULT TRUE,
			created_at TIMESTAMP DEFAULT NOW()
		)
	`)
	must(err, "create table")
	fmt.Println("✓ Table 'users' created")

	// ── INSERT ────────────────────────────────────────────────────────────────
	seeds := []struct {
		name  string
		email string
		age   int
	}{
		{"Alice Johnson", "alice@example.com", 30},
		{"Bob Smith", "bob@example.com", 25},
		{"Carol White", "carol@example.com", 35},
		{"Dave Brown", "dave@example.com", 28},
		{"Eve Davis", "eve@example.com", 22},
	}

	for _, s := range seeds {
		var id int
		err = db.QueryRow(
			`INSERT INTO users (name, email, age) VALUES ($1, $2, $3) RETURNING id`,
			s.name, s.email, s.age,
		).Scan(&id)
		must(err, "insert")
		fmt.Printf("  + Inserted user id=%d  name=%q\n", id, s.name)
	}

	// ── SELECT all ────────────────────────────────────────────────────────────
	fmt.Println("\n── All users ────────────────────────────────")
	rows, err := db.Query(`SELECT id, name, email, age, active FROM users ORDER BY id`)
	must(err, "select")
	defer rows.Close()

	for rows.Next() {
		var u User
		must(rows.Scan(&u.ID, &u.Name, &u.Email, &u.Age, &u.Active), "scan")
		fmt.Printf("  [%d] %-20s  %-28s  age=%-3d  active=%v\n",
			u.ID, u.Name, u.Email, u.Age, u.Active)
	}
	must(rows.Err(), "rows")

	// ── SELECT with WHERE + parameter ─────────────────────────────────────────
	fmt.Println("\n── Users older than 27 ──────────────────────")
	rows2, err := db.Query(`SELECT id, name, age FROM users WHERE age > $1 ORDER BY age DESC`, 27)
	must(err, "select where")
	defer rows2.Close()
	for rows2.Next() {
		var id, age int
		var name string
		must(rows2.Scan(&id, &name, &age), "scan")
		fmt.Printf("  [%d] %s  (age %d)\n", id, name, age)
	}

	// ── UPDATE ────────────────────────────────────────────────────────────────
	res, err := db.Exec(`UPDATE users SET active = FALSE WHERE age < $1`, 25)
	must(err, "update")
	affected, _ := res.RowsAffected()
	fmt.Printf("\n── Updated %d rows (active=false where age<25)\n", affected)

	// ── TRANSACTION ───────────────────────────────────────────────────────────
	fmt.Println("\n── Transaction: bulk insert ─────────────────")
	tx, err := db.Begin()
	must(err, "begin tx")

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
	must(tx.Commit(), "commit")
	fmt.Println("  ✓ Transaction committed")

	// ── COUNT ─────────────────────────────────────────────────────────────────
	var total int
	must(db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&total), "count")
	fmt.Printf("\n── Total users: %d\n", total)

	// ── DELETE ────────────────────────────────────────────────────────────────
	res, err = db.Exec(`DELETE FROM users WHERE name = $1`, "Heidi")
	must(err, "delete")
	deleted, _ := res.RowsAffected()
	fmt.Printf("── Deleted %d user(s) named Heidi\n", deleted)

	// ── AGGREGATE ─────────────────────────────────────────────────────────────
	var minAge, maxAge int
	var avgAge float64
	must(db.QueryRow(`SELECT MIN(age), MAX(age), AVG(age) FROM users`).Scan(&minAge, &maxAge, &avgAge), "agg")
	fmt.Printf("── Age stats: min=%d  max=%d  avg=%.1f\n", minAge, maxAge, avgAge)

	// ── DROP ──────────────────────────────────────────────────────────────────
	_, _ = db.Exec(`DROP TABLE users`)
	fmt.Println("\n✓ Done — table dropped")
}

func must(err error, label string) {
	if err != nil {
		log.Fatalf("%s: %v", label, err)
	}
}
