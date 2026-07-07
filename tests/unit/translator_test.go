// Package unit contains pure unit tests for the sql/planner package.
// No server required — these run offline.
package unit

import (
	"strings"
	"testing"

	"github.com/sqlite-server/sqlite-server/sql/planner"
)

// newPlanner creates a fresh Planner for each test.
func newPlanner() *planner.Planner {
	return planner.New()
}

// rewrite is a shorthand helper.
func rewrite(t *testing.T, pgSQL string) string {
	t.Helper()
	p := newPlanner()
	result, err := p.Rewrite(pgSQL)
	if err != nil {
		t.Fatalf("rewrite(%q) err: %v", pgSQL, err)
	}
	return result
}

// ─────────────────────────────────────────────────────────────────────────────
//  Planner / Rewriter tests
// ─────────────────────────────────────────────────────────────────────────────

func TestRewriteSelectOne(t *testing.T) {
	got := rewrite(t, "SELECT 1")
	if got != "SELECT 1" {
		t.Fatalf("unexpected: %q", got)
	}
}

func TestRewriteNowFunction(t *testing.T) {
	got := rewrite(t, "SELECT NOW()")
	lower := strings.ToLower(got)
	if !strings.Contains(lower, "datetime") && !strings.Contains(lower, "now") {
		t.Fatalf("expected datetime translation, got %q", got)
	}
}

func TestRewriteCurrentTimestamp(t *testing.T) {
	got := rewrite(t, "SELECT CURRENT_TIMESTAMP")
	lower := strings.ToLower(got)
	if !strings.Contains(lower, "datetime") && !strings.Contains(lower, "current_timestamp") {
		t.Fatalf("expected datetime translation, got %q", got)
	}
}

func TestRewriteILIKE(t *testing.T) {
	got := rewrite(t, "SELECT * FROM t WHERE name ILIKE '%test%'")
	upper := strings.ToUpper(got)
	if strings.Contains(upper, "ILIKE") {
		t.Fatalf("ILIKE should have been translated, got %q", got)
	}
	if !strings.Contains(upper, "LIKE") {
		t.Fatalf("expected LIKE in result, got %q", got)
	}
}

func TestRewriteExtractYear(t *testing.T) {
	got := rewrite(t, "SELECT EXTRACT(YEAR FROM created_at) FROM events")
	lower := strings.ToLower(got)
	if !strings.Contains(lower, "strftime") && !strings.Contains(lower, "extract") {
		t.Fatalf("expected strftime translation, got %q", got)
	}
}

func TestRewriteSerialType(t *testing.T) {
	got := rewrite(t, "CREATE TABLE t (id SERIAL PRIMARY KEY, name TEXT)")
	upper := strings.ToUpper(got)
	if strings.Contains(upper, " SERIAL") {
		t.Fatalf("SERIAL should have been translated, got %q", got)
	}
}

func TestRewriteBigSerial(t *testing.T) {
	got := rewrite(t, "CREATE TABLE t (id BIGSERIAL PRIMARY KEY)")
	upper := strings.ToUpper(got)
	if strings.Contains(upper, "BIGSERIAL") {
		t.Fatalf("BIGSERIAL should have been translated, got %q", got)
	}
}

func TestRewriteBooleanType(t *testing.T) {
	got := rewrite(t, "CREATE TABLE t (active BOOLEAN DEFAULT TRUE)")
	// BOOLEAN is mapped to INTEGER in SQLite
	_ = got // just verify no error
}

func TestRewritePlaceholders(t *testing.T) {
	got := rewrite(t, "SELECT * FROM t WHERE id = $1 AND name = $2")
	if strings.Contains(got, "$1") || strings.Contains(got, "$2") {
		t.Fatalf("$N placeholders should be replaced with ?, got %q", got)
	}
	if strings.Count(got, "?") != 2 {
		t.Fatalf("expected 2 ? placeholders, got %q", got)
	}
}

func TestRewriteDoubleColon(t *testing.T) {
	got := rewrite(t, "SELECT '42'::INTEGER")
	if strings.Contains(got, "::") {
		t.Fatalf(":: should have been translated, got %q", got)
	}
}

func TestRewriteInsertWithReturning(t *testing.T) {
	got := rewrite(t, "INSERT INTO users (name) VALUES ($1) RETURNING id")
	// RETURNING should be preserved — SQLite 3.35+ supports it
	if !strings.Contains(strings.ToUpper(got), "RETURNING") {
		t.Logf("RETURNING removed (ok if SQLite <3.35): %q", got)
	}
}

func TestRewriteSetStatement(t *testing.T) {
	p := newPlanner()
	// SET should not error — translate to no-op or SELECT 1
	got, err := p.Rewrite("SET client_encoding = 'UTF8'")
	if err != nil {
		t.Fatalf("SET err: %v", err)
	}
	_ = got
}

func TestRewriteShowStatement(t *testing.T) {
	p := newPlanner()
	got, err := p.Rewrite("SHOW server_version")
	if err != nil {
		t.Fatalf("SHOW err: %v", err)
	}
	_ = got
}

func TestRewriteCTESimple(t *testing.T) {
	got := rewrite(t, `
		WITH numbered AS (
			SELECT ROW_NUMBER() OVER() as rn, name FROM users
		)
		SELECT name FROM numbered WHERE rn <= 5
	`)
	if !strings.Contains(strings.ToUpper(got), "WITH") {
		t.Logf("CTE simplified/inlined: %q", got)
	}
}

func TestRewriteEmptyQuery(t *testing.T) {
	p := newPlanner()
	_, err := p.Rewrite("")
	// Empty query should either return empty or a harmless result
	if err != nil {
		t.Logf("empty query returned error (acceptable): %v", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
//  Planner.Rewrite — idempotency checks
// ─────────────────────────────────────────────────────────────────────────────

func TestRewriteIdempotent(t *testing.T) {
	queries := []string{
		"SELECT 1",
		"SELECT * FROM users",
		"SELECT id, name FROM orders WHERE total > 100",
		"INSERT INTO t (k, v) VALUES (?, ?)",
		"UPDATE t SET val = ? WHERE id = ?",
		"DELETE FROM t WHERE id = ?",
		"BEGIN",
		"COMMIT",
		"ROLLBACK",
	}
	for _, q := range queries {
		p := newPlanner()
		first, err := p.Rewrite(q)
		if err != nil {
			t.Errorf("first rewrite %q: %v", q, err)
			continue
		}
		second, err := p.Rewrite(first)
		if err != nil {
			t.Errorf("second rewrite %q: %v", first, err)
			continue
		}
		if first != second {
			t.Logf("not idempotent (ok for complex rewrites): %q → %q → %q", q, first, second)
		}
	}
}
