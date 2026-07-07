// Package engine ties together the SQL planner, the virtual catalog, and the
// SQLite database driver.  It is the bridge between the wire protocol layer
// and the actual database.
package engine

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/sqlite-server/sqlite-server/internal/catalog"
	"github.com/sqlite-server/sqlite-server/internal/errors"
	"github.com/sqlite-server/sqlite-server/internal/pgproto"
	"github.com/sqlite-server/sqlite-server/sql/planner"
)

// ─────────────────────────────────────────────────────────────────────────────
//
//	Executor
//
// ─────────────────────────────────────────────────────────────────────────────
// Executor executes a rewritten SQL statement against a SQLite connection.
type Executor struct {
	plan    *planner.Planner
	catalog *catalog.VirtualCatalog
}

// New creates an Executor.
func New() *Executor {
	return &Executor{
		plan:    planner.New(),
		catalog: catalog.New(),
	}
}

// Rewrite rewrites a PostgreSQL SQL string into a SQLite SQL string without
// executing it.  Used during Parse (Extended Query Protocol).
func (e *Executor) Rewrite(pgSQL string) (string, error) {
	return e.plan.Rewrite(pgSQL)
}

// Execute rewrites and executes a SQL statement.
// It returns a *pgproto.QueryResult that the wire layer can serialise directly.
func (e *Executor) Execute(ctx context.Context, db *sql.Conn, pgSQL string, args []interface{}) (*pgproto.QueryResult, error) {
	// ── 1. Check for virtual catalog queries ─────────────────────────────
	if result, handled := e.catalog.Handle(ctx, db, pgSQL); handled {
		return result, nil
	}
	// ── 2. Rewrite through the AST pipeline ──────────────────────────────
	sqliteSQL, err := e.plan.Rewrite(pgSQL)
	if err != nil {
		return nil, err
	}
	// ── 3. Replace PostgreSQL-style $1 params with SQLite ?  ─────────────
	sqliteSQL = normalizePlaceholders(sqliteSQL)
	// ── 4. Determine statement type ──────────────────────────────────────
	cmd := commandType(sqliteSQL)
	// ── 5. Execute ────────────────────────────────────────────────────────
	switch cmd {
	case "SELECT", "EXPLAIN", "PRAGMA", "VALUES", "WITH":
		return executeQuery(ctx, db, sqliteSQL, args)
	case "INSERT", "UPDATE", "DELETE":
		return executeExec(ctx, db, sqliteSQL, args, cmd)
	case "BEGIN", "START":
		_, err := db.ExecContext(ctx, "BEGIN")
		return &pgproto.QueryResult{Tag: "BEGIN"}, translateErr(err)
	case "COMMIT", "END":
		_, err := db.ExecContext(ctx, "COMMIT")
		return &pgproto.QueryResult{Tag: "COMMIT"}, translateErr(err)
	case "ROLLBACK":
		_, err := db.ExecContext(ctx, sqliteSQL)
		return &pgproto.QueryResult{Tag: "ROLLBACK"}, translateErr(err)
	case "SAVEPOINT":
		_, err := db.ExecContext(ctx, sqliteSQL)
		return &pgproto.QueryResult{Tag: "SAVEPOINT"}, translateErr(err)
	case "RELEASE":
		_, err := db.ExecContext(ctx, sqliteSQL)
		return &pgproto.QueryResult{Tag: "RELEASE"}, translateErr(err)
	case "CREATE", "DROP", "ALTER", "TRUNCATE":
		return executeDDL(ctx, db, sqliteSQL)
	case "SET", "SHOW":
		// Already rewritten to SELECT 1 by the rewriter.
		return executeQuery(ctx, db, sqliteSQL, nil)
	default:
		// Try as a generic exec.
		res, err := db.ExecContext(ctx, sqliteSQL, args...)
		if err != nil {
			return nil, translateErr(err)
		}
		affected, _ := res.RowsAffected()
		return &pgproto.QueryResult{Tag: fmt.Sprintf("%s %d", strings.ToUpper(cmd), affected)}, nil
	}
}

// DescribeColumns returns the column descriptors for a SELECT statement without
// actually returning data.  Used by Describe(Statement) in the extended query protocol.
func (e *Executor) DescribeColumns(ctx context.Context, db *sql.Conn, sqliteSQL string) ([]pgproto.ColumnDesc, error) {
	// Execute with LIMIT 0 to get column types without reading data.
	limited := withLimit0(sqliteSQL)
	rows, err := db.QueryContext(ctx, limited)
	if err != nil {
		return nil, translateErr(err)
	}
	defer rows.Close()
	colTypes, err := rows.ColumnTypes()
	if err != nil {
		return nil, err
	}
	cols := make([]pgproto.ColumnDesc, len(colTypes))
	for i, ct := range colTypes {
		oid, size := pgproto.SQLiteTypeToOID(ct.DatabaseTypeName())
		cols[i] = pgproto.ColumnDesc{
			Name:     ct.Name(),
			TypeOID:  oid,
			TypeSize: size,
		}
	}
	return cols, nil
}

// ─────────────────────────────────────────────────────────────────────────────
//
//	Internal execution helpers
//
// ─────────────────────────────────────────────────────────────────────────────
// executeQuery runs a SELECT-like statement and returns rows.
func executeQuery(ctx context.Context, db *sql.Conn, sqliteSQL string, args []interface{}) (*pgproto.QueryResult, error) {
	rows, err := db.QueryContext(ctx, sqliteSQL, args...)
	if err != nil {
		return nil, translateErr(err)
	}
	defer rows.Close()
	// ── Column metadata ───────────────────────────────────────────────────
	colTypes, err := rows.ColumnTypes()
	if err != nil {
		return nil, err
	}
	cols := make([]pgproto.ColumnDesc, len(colTypes))
	for i, ct := range colTypes {
		oid, size := pgproto.SQLiteTypeToOID(ct.DatabaseTypeName())
		cols[i] = pgproto.ColumnDesc{
			Name:     ct.Name(),
			TypeOID:  oid,
			TypeSize: size,
		}
	}
	// ── Rows ──────────────────────────────────────────────────────────────
	var resultRows [][]interface{}
	for rows.Next() {
		vals := make([]interface{}, len(cols))
		ptrs := make([]interface{}, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, translateErr(err)
		}
		// Convert any []byte fields to string for text protocol.
		for i, v := range vals {
			if b, ok := v.([]byte); ok {
				vals[i] = string(b)
			}
		}
		resultRows = append(resultRows, vals)
	}
	if err := rows.Err(); err != nil {
		return nil, translateErr(err)
	}
	return &pgproto.QueryResult{
		Columns: cols,
		Rows:    resultRows,
		Tag:     fmt.Sprintf("SELECT %d", len(resultRows)),
	}, nil
}

// executeExec runs INSERT/UPDATE/DELETE and returns a tag.
func executeExec(ctx context.Context, db *sql.Conn, sqliteSQL string, args []interface{}, cmd string) (*pgproto.QueryResult, error) {
	// Check for RETURNING clause — execute as a query.
	upper := strings.ToUpper(sqliteSQL)
	if strings.Contains(upper, " RETURNING ") {
		return executeQuery(ctx, db, sqliteSQL, args)
	}
	res, err := db.ExecContext(ctx, sqliteSQL, args...)
	if err != nil {
		return nil, translateErr(err)
	}
	affected, _ := res.RowsAffected()
	lastID, _ := res.LastInsertId()
	var tag string
	switch cmd {
	case "INSERT":
		tag = fmt.Sprintf("INSERT 0 %d", affected)
		_ = lastID
	case "UPDATE":
		tag = fmt.Sprintf("UPDATE %d", affected)
	case "DELETE":
		tag = fmt.Sprintf("DELETE %d", affected)
	default:
		tag = fmt.Sprintf("%s %d", cmd, affected)
	}
	return &pgproto.QueryResult{Tag: tag, RowsAffected: affected}, nil
}

// executeDDL runs CREATE/DROP/ALTER/TRUNCATE.
func executeDDL(ctx context.Context, db *sql.Conn, sqliteSQL string) (*pgproto.QueryResult, error) {
	_, err := db.ExecContext(ctx, sqliteSQL)
	if err != nil {
		return nil, translateErr(err)
	}
	cmd := commandType(sqliteSQL)
	return &pgproto.QueryResult{Tag: strings.ToUpper(cmd)}, nil
}

// ─────────────────────────────────────────────────────────────────────────────
//
//	Helper utilities
//
// ─────────────────────────────────────────────────────────────────────────────
// commandType extracts the first word (command) from a SQL string.
func commandType(sql string) string {
	sql = strings.TrimSpace(sql)
	idx := strings.IndexFunc(sql, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == '('
	})
	if idx < 0 {
		return strings.ToUpper(sql)
	}
	return strings.ToUpper(sql[:idx])
}

// normalizePlaceholders replaces PostgreSQL $1, $2, … with SQLite ? placeholders.
// It respects single-quoted strings and double-quoted identifiers.
func normalizePlaceholders(sql string) string {
	var out strings.Builder
	inStr := false
	inIdent := false
	i := 0
	for i < len(sql) {
		ch := sql[i]
		if inStr {
			out.WriteByte(ch)
			if ch == '\'' {
				if i+1 < len(sql) && sql[i+1] == '\'' {
					out.WriteByte(sql[i+1])
					i += 2
					continue
				}
				inStr = false
			}
			i++
			continue
		}
		if inIdent {
			out.WriteByte(ch)
			if ch == '"' {
				inIdent = false
			}
			i++
			continue
		}
		if ch == '\'' {
			inStr = true
			out.WriteByte(ch)
			i++
			continue
		}
		if ch == '"' {
			inIdent = true
			out.WriteByte(ch)
			i++
			continue
		}
		if ch == '$' && i+1 < len(sql) && sql[i+1] >= '1' && sql[i+1] <= '9' {
			// Consume $N
			j := i + 1
			for j < len(sql) && sql[j] >= '0' && sql[j] <= '9' {
				j++
			}
			out.WriteByte('?')
			i = j
			continue
		}
		out.WriteByte(ch)
		i++
	}
	return out.String()
}

// withLimit0 appends LIMIT 0 to a SELECT for dry-run column discovery.
func withLimit0(sql string) string {
	trimmed := strings.TrimRight(strings.TrimSpace(sql), ";")
	return trimmed + " LIMIT 0"
}

// translateErr converts a SQLite driver error to a *errors.PGError.
func translateErr(err error) error {
	if err == nil {
		return nil
	}
	return errors.TranslateSQLiteError(err)
}
