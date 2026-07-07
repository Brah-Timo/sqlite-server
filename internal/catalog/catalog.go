// Package catalog implements the virtual PostgreSQL system catalogs.
//
// DBeaver, pgAdmin, IntelliJ, and every major ORM send many queries against
// pg_catalog and information_schema at startup and during schema introspection.
// This package intercepts those queries and synthesises responses from the
// real SQLite schema (sqlite_master / PRAGMA table_info).
//
// The virtual catalog is a VirtualDatabase of virtual tables:
//
//	pg_catalog.pg_tables         — list of tables
//	pg_catalog.pg_class          — relation catalogue
//	pg_catalog.pg_attribute      — column catalogue
//	pg_catalog.pg_type           — type catalogue
//	pg_catalog.pg_namespace      — schema/namespace catalogue
//	pg_catalog.pg_constraint     — constraint catalogue
//	pg_catalog.pg_index          — index catalogue
//	information_schema.tables    — SQL standard table list
//	information_schema.columns   — SQL standard column list
//	information_schema.key_column_usage   — PKs and FKs
//	information_schema.table_constraints  — constraint list
//	information_schema.referential_constraints — FK list
//
// Any query that touches these virtual tables is handled here.
// Everything else is passed to the real SQLite engine.
package catalog

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/sqlite-server/sqlite-server/internal/pgproto"
)

// ─────────────────────────────────────────────────────────────────────────────
//  VirtualCatalog
// ─────────────────────────────────────────────────────────────────────────────

// VirtualCatalog intercepts system-catalog queries and satisfies them
// without touching SQLite's real tables.
type VirtualCatalog struct{}

// New creates a VirtualCatalog.
func New() *VirtualCatalog { return &VirtualCatalog{} }

// Handle checks whether the SQL query targets a virtual system catalog.
// If so, it executes the virtual query and returns (result, true).
// Otherwise it returns (nil, false).
func (vc *VirtualCatalog) Handle(ctx context.Context, db *sql.Conn, pgSQL string) (*pgproto.QueryResult, bool) {
	lower := strings.ToLower(pgSQL)

	// Quick filter: if the query doesn't mention any catalog prefix, skip.
	if !strings.Contains(lower, "pg_catalog") &&
		!strings.Contains(lower, "information_schema") &&
		!strings.Contains(lower, "pg_tables") &&
		!strings.Contains(lower, "pg_class") &&
		!strings.Contains(lower, "pg_attribute") &&
		!strings.Contains(lower, "pg_type") &&
		!strings.Contains(lower, "pg_namespace") &&
		!strings.Contains(lower, "pg_constraint") &&
		!strings.Contains(lower, "pg_index") &&
		!strings.Contains(lower, "select version()") &&
		!strings.Contains(lower, "select current_") &&
		!strings.Contains(lower, "pg_get_userbyid") {
		return nil, false
	}

	// ── Special single-expression queries ──────────────────────────────────
	if result, ok := vc.handleSpecialQuery(lower, pgSQL); ok {
		return result, true
	}

	// ── information_schema.tables ──────────────────────────────────────────
	if strings.Contains(lower, "information_schema.tables") {
		return vc.infoSchemaTables(ctx, db, lower)
	}

	// ── information_schema.columns ─────────────────────────────────────────
	if strings.Contains(lower, "information_schema.columns") {
		return vc.infoSchemaColumns(ctx, db, lower)
	}

	// ── information_schema.key_column_usage ────────────────────────────────
	if strings.Contains(lower, "information_schema.key_column_usage") ||
		strings.Contains(lower, "information_schema.table_constraints") ||
		strings.Contains(lower, "information_schema.referential_constraints") ||
		strings.Contains(lower, "information_schema.constraint_column_usage") {
		return vc.infoSchemaConstraints(ctx, db, lower)
	}

	// ── pg_catalog.pg_tables ───────────────────────────────────────────────
	if strings.Contains(lower, "pg_tables") || strings.Contains(lower, "pg_catalog.pg_tables") {
		return vc.pgTables(ctx, db)
	}

	// ── pg_catalog.pg_class ────────────────────────────────────────────────
	if strings.Contains(lower, "pg_class") {
		return vc.pgClass(ctx, db)
	}

	// ── pg_catalog.pg_attribute ────────────────────────────────────────────
	if strings.Contains(lower, "pg_attribute") {
		return vc.pgAttribute(ctx, db, pgSQL)
	}

	// ── pg_catalog.pg_type ─────────────────────────────────────────────────
	if strings.Contains(lower, "pg_type") {
		return vc.pgType()
	}

	// ── pg_catalog.pg_namespace ────────────────────────────────────────────
	if strings.Contains(lower, "pg_namespace") {
		return vc.pgNamespace()
	}

	// ── pg_catalog.pg_constraint ───────────────────────────────────────────
	if strings.Contains(lower, "pg_constraint") {
		return vc.pgConstraint(ctx, db)
	}

	// ── pg_catalog.pg_index ────────────────────────────────────────────────
	if strings.Contains(lower, "pg_index") {
		return vc.pgIndex(ctx, db)
	}

	return nil, false
}

// ─────────────────────────────────────────────────────────────────────────────
//  Special queries
// ─────────────────────────────────────────────────────────────────────────────

func (vc *VirtualCatalog) handleSpecialQuery(lower, original string) (*pgproto.QueryResult, bool) {
	switch {
	case strings.Contains(lower, "select version()"):
		return singleRow([]pgproto.ColumnDesc{{Name: "version", TypeOID: 25, TypeSize: -1}},
			[]interface{}{"PostgreSQL 14.5 (sqlite-server)"}), true

	case strings.Contains(lower, "select current_database()"):
		return singleRow([]pgproto.ColumnDesc{{Name: "current_database", TypeOID: 25, TypeSize: -1}},
			[]interface{}{"main"}), true

	case strings.Contains(lower, "select current_schema()"):
		return singleRow([]pgproto.ColumnDesc{{Name: "current_schema", TypeOID: 25, TypeSize: -1}},
			[]interface{}{"public"}), true

	case strings.Contains(lower, "select current_user"):
		return singleRow([]pgproto.ColumnDesc{{Name: "current_user", TypeOID: 25, TypeSize: -1}},
			[]interface{}{"postgres"}), true

	case strings.Contains(lower, "select session_user"):
		return singleRow([]pgproto.ColumnDesc{{Name: "session_user", TypeOID: 25, TypeSize: -1}},
			[]interface{}{"postgres"}), true

	case strings.Contains(lower, "pg_get_userbyid"):
		return singleRow([]pgproto.ColumnDesc{{Name: "pg_get_userbyid", TypeOID: 25, TypeSize: -1}},
			[]interface{}{"postgres"}), true

	case strings.Contains(lower, "pg_backend_pid"):
		return singleRow([]pgproto.ColumnDesc{{Name: "pg_backend_pid", TypeOID: 23, TypeSize: 4}},
			[]interface{}{int64(1)}), true

	case lower == "select 1" || lower == "select 1;":
		return singleRow([]pgproto.ColumnDesc{{Name: "?column?", TypeOID: 23, TypeSize: 4}},
			[]interface{}{int64(1)}), true

	case strings.Contains(lower, "set_config"):
		return emptyResult("SELECT 1"), true
	}
	return nil, false
}

// ─────────────────────────────────────────────────────────────────────────────
//  information_schema.tables
// ─────────────────────────────────────────────────────────────────────────────

func (vc *VirtualCatalog) infoSchemaTables(ctx context.Context, db *sql.Conn, lower string) (*pgproto.QueryResult, bool) {
	// Get the list of user tables from sqlite_master.
	rows, err := db.QueryContext(ctx,
		`SELECT name FROM sqlite_master
		 WHERE type = 'table'
		   AND name NOT LIKE 'sqlite_%'
		 ORDER BY name`)
	if err != nil {
		return emptyResult(""), true
	}
	defer rows.Close()

	result := &pgproto.QueryResult{
		Columns: []pgproto.ColumnDesc{
			{Name: "table_catalog", TypeOID: 25, TypeSize: -1},
			{Name: "table_schema", TypeOID: 25, TypeSize: -1},
			{Name: "table_name", TypeOID: 25, TypeSize: -1},
			{Name: "table_type", TypeOID: 25, TypeSize: -1},
			{Name: "is_insertable_into", TypeOID: 25, TypeSize: -1},
			{Name: "is_typed", TypeOID: 25, TypeSize: -1},
		},
	}

	// Apply optional table_name filter.
	tableFilter := ""
	if idx := strings.Index(lower, "table_name ="); idx >= 0 {
		tableFilter = extractStringLiteral(lower[idx+12:])
	} else if idx := strings.Index(lower, "table_name="); idx >= 0 {
		tableFilter = extractStringLiteral(lower[idx+11:])
	}

	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			continue
		}
		if tableFilter != "" && !strings.EqualFold(name, tableFilter) {
			continue
		}
		result.Rows = append(result.Rows, []interface{}{
			"main",       // table_catalog
			"public",     // table_schema
			name,         // table_name
			"BASE TABLE", // table_type
			"YES",        // is_insertable_into
			"NO",         // is_typed
		})
	}

	result.Tag = fmt.Sprintf("SELECT %d", len(result.Rows))
	return result, true
}

// ─────────────────────────────────────────────────────────────────────────────
//  information_schema.columns
// ─────────────────────────────────────────────────────────────────────────────

func (vc *VirtualCatalog) infoSchemaColumns(ctx context.Context, db *sql.Conn, lower string) (*pgproto.QueryResult, bool) {
	result := &pgproto.QueryResult{
		Columns: []pgproto.ColumnDesc{
			{Name: "table_catalog", TypeOID: 25, TypeSize: -1},
			{Name: "table_schema", TypeOID: 25, TypeSize: -1},
			{Name: "table_name", TypeOID: 25, TypeSize: -1},
			{Name: "column_name", TypeOID: 25, TypeSize: -1},
			{Name: "ordinal_position", TypeOID: 23, TypeSize: 4},
			{Name: "column_default", TypeOID: 25, TypeSize: -1},
			{Name: "is_nullable", TypeOID: 25, TypeSize: -1},
			{Name: "data_type", TypeOID: 25, TypeSize: -1},
			{Name: "character_maximum_length", TypeOID: 23, TypeSize: 4},
			{Name: "numeric_precision", TypeOID: 23, TypeSize: 4},
			{Name: "numeric_scale", TypeOID: 23, TypeSize: 4},
			{Name: "udt_name", TypeOID: 25, TypeSize: -1},
		},
	}

	// Extract optional table filter.
	tableFilter := ""
	if idx := strings.Index(lower, "table_name ="); idx >= 0 {
		tableFilter = extractStringLiteral(lower[idx+12:])
	}

	tables, err := listTables(ctx, db)
	if err != nil {
		return result, true
	}

	for _, table := range tables {
		if tableFilter != "" && !strings.EqualFold(table, tableFilter) {
			continue
		}
		cols, err := tableColumns(ctx, db, table)
		if err != nil {
			continue
		}
		for _, col := range cols {
			pgType := sqliteTypeToPGType(col.Type)
			nullable := "YES"
			if col.NotNull {
				nullable = "NO"
			}
			var dflt interface{}
			if col.Default != "" {
				dflt = col.Default
			}
			result.Rows = append(result.Rows, []interface{}{
				"main",             // table_catalog
				"public",           // table_schema
				table,              // table_name
				col.Name,           // column_name
				int64(col.CID + 1), // ordinal_position
				dflt,               // column_default
				nullable,           // is_nullable
				pgType,             // data_type
				nil,                // character_maximum_length
				nil,                // numeric_precision
				nil,                // numeric_scale
				pgType,             // udt_name
			})
		}
	}

	result.Tag = fmt.Sprintf("SELECT %d", len(result.Rows))
	return result, true
}

// ─────────────────────────────────────────────────────────────────────────────
//  information_schema constraints (stub — most ORMs do not need full data)
// ─────────────────────────────────────────────────────────────────────────────

func (vc *VirtualCatalog) infoSchemaConstraints(_ context.Context, _ *sql.Conn, _ string) (*pgproto.QueryResult, bool) {
	return emptyResult("SELECT 0"), true
}

// ─────────────────────────────────────────────────────────────────────────────
//  pg_tables
// ─────────────────────────────────────────────────────────────────────────────

func (vc *VirtualCatalog) pgTables(ctx context.Context, db *sql.Conn) (*pgproto.QueryResult, bool) {
	tables, _ := listTables(ctx, db)
	result := &pgproto.QueryResult{
		Columns: []pgproto.ColumnDesc{
			{Name: "schemaname", TypeOID: 25, TypeSize: -1},
			{Name: "tablename", TypeOID: 25, TypeSize: -1},
			{Name: "tableowner", TypeOID: 25, TypeSize: -1},
			{Name: "hasindexes", TypeOID: 16, TypeSize: 1},
			{Name: "hasrules", TypeOID: 16, TypeSize: 1},
			{Name: "hastriggers", TypeOID: 16, TypeSize: 1},
			{Name: "rowsecurity", TypeOID: 16, TypeSize: 1},
		},
	}
	for _, t := range tables {
		result.Rows = append(result.Rows, []interface{}{
			"public", t, "postgres", "t", "f", "f", "f",
		})
	}
	result.Tag = fmt.Sprintf("SELECT %d", len(result.Rows))
	return result, true
}

// ─────────────────────────────────────────────────────────────────────────────
//  pg_class
// ─────────────────────────────────────────────────────────────────────────────

func (vc *VirtualCatalog) pgClass(ctx context.Context, db *sql.Conn) (*pgproto.QueryResult, bool) {
	tables, _ := listTables(ctx, db)
	result := &pgproto.QueryResult{
		Columns: []pgproto.ColumnDesc{
			{Name: "oid", TypeOID: 26, TypeSize: 4},
			{Name: "relname", TypeOID: 25, TypeSize: -1},
			{Name: "relnamespace", TypeOID: 26, TypeSize: 4},
			{Name: "reltype", TypeOID: 26, TypeSize: 4},
			{Name: "relowner", TypeOID: 26, TypeSize: 4},
			{Name: "relkind", TypeOID: 18, TypeSize: 1},
			{Name: "relnatts", TypeOID: 21, TypeSize: 2},
			{Name: "relhaspkey", TypeOID: 16, TypeSize: 1},
			{Name: "relhasrules", TypeOID: 16, TypeSize: 1},
			{Name: "reltriggers", TypeOID: 21, TypeSize: 2},
		},
	}
	for i, t := range tables {
		oid := int64(i + 10000)
		result.Rows = append(result.Rows, []interface{}{
			oid, t, int64(2200), int64(0), int64(10), "r", int64(0), "t", "f", int64(0),
		})
	}
	result.Tag = fmt.Sprintf("SELECT %d", len(result.Rows))
	return result, true
}

// ─────────────────────────────────────────────────────────────────────────────
//  pg_attribute
// ─────────────────────────────────────────────────────────────────────────────

func (vc *VirtualCatalog) pgAttribute(ctx context.Context, db *sql.Conn, pgSQL string) (*pgproto.QueryResult, bool) {
	result := &pgproto.QueryResult{
		Columns: []pgproto.ColumnDesc{
			{Name: "attrelid", TypeOID: 26, TypeSize: 4},
			{Name: "attname", TypeOID: 25, TypeSize: -1},
			{Name: "atttypid", TypeOID: 26, TypeSize: 4},
			{Name: "attstattarget", TypeOID: 23, TypeSize: 4},
			{Name: "attlen", TypeOID: 21, TypeSize: 2},
			{Name: "attnum", TypeOID: 21, TypeSize: 2},
			{Name: "attndims", TypeOID: 23, TypeSize: 4},
			{Name: "attcacheoff", TypeOID: 23, TypeSize: 4},
			{Name: "atttypmod", TypeOID: 23, TypeSize: 4},
			{Name: "attbyval", TypeOID: 16, TypeSize: 1},
			{Name: "attstorage", TypeOID: 18, TypeSize: 1},
			{Name: "attalign", TypeOID: 18, TypeSize: 1},
			{Name: "attnotnull", TypeOID: 16, TypeSize: 1},
			{Name: "atthasdef", TypeOID: 16, TypeSize: 1},
			{Name: "attidentity", TypeOID: 18, TypeSize: 1},
			{Name: "attgenerated", TypeOID: 18, TypeSize: 1},
			{Name: "attisdropped", TypeOID: 16, TypeSize: 1},
			{Name: "attislocal", TypeOID: 16, TypeSize: 1},
			{Name: "attinhcount", TypeOID: 23, TypeSize: 4},
			{Name: "attcollation", TypeOID: 26, TypeSize: 4},
		},
	}

	tables, _ := listTables(ctx, db)
	for tableIdx, table := range tables {
		oid := int64(tableIdx + 10000)
		cols, err := tableColumns(ctx, db, table)
		if err != nil {
			continue
		}
		for _, col := range cols {
			typeOID, _ := pgproto.SQLiteTypeToOID(col.Type)
			notNull := "f"
			if col.NotNull {
				notNull = "t"
			}
			result.Rows = append(result.Rows, []interface{}{
				oid, col.Name, int64(typeOID), int64(0),
				int64(-1), int64(col.CID + 1), int64(0), int64(-1), int64(-1),
				"f", "p", "i", notNull, "f", "", "", "f", "t", int64(0), int64(0),
			})
		}
	}
	result.Tag = fmt.Sprintf("SELECT %d", len(result.Rows))
	return result, true
}

// ─────────────────────────────────────────────────────────────────────────────
//  pg_type
// ─────────────────────────────────────────────────────────────────────────────

func (vc *VirtualCatalog) pgType() (*pgproto.QueryResult, bool) {
	result := &pgproto.QueryResult{
		Columns: []pgproto.ColumnDesc{
			{Name: "oid", TypeOID: 26, TypeSize: 4},
			{Name: "typname", TypeOID: 25, TypeSize: -1},
			{Name: "typnamespace", TypeOID: 26, TypeSize: 4},
			{Name: "typowner", TypeOID: 26, TypeSize: 4},
			{Name: "typlen", TypeOID: 21, TypeSize: 2},
			{Name: "typtype", TypeOID: 18, TypeSize: 1},
			{Name: "typcategory", TypeOID: 18, TypeSize: 1},
			{Name: "typnotnull", TypeOID: 16, TypeSize: 1},
			{Name: "typbasetype", TypeOID: 26, TypeSize: 4},
			{Name: "typndims", TypeOID: 23, TypeSize: 4},
			{Name: "typinput", TypeOID: 25, TypeSize: -1},
			{Name: "typoutput", TypeOID: 25, TypeSize: -1},
		},
	}

	// Emit the most important types.
	types := []struct {
		oid  int64
		name string
		len  int64
		cat  string
	}{
		{16, "bool", 1, "B"},
		{17, "bytea", -1, "U"},
		{20, "int8", 8, "N"},
		{21, "int2", 2, "N"},
		{23, "int4", 4, "N"},
		{25, "text", -1, "S"},
		{114, "json", -1, "U"},
		{700, "float4", 4, "N"},
		{701, "float8", 8, "N"},
		{1043, "varchar", -1, "S"},
		{1082, "date", 4, "D"},
		{1083, "time", 8, "D"},
		{1114, "timestamp", 8, "D"},
		{1184, "timestamptz", 8, "D"},
		{1700, "numeric", -1, "N"},
		{2950, "uuid", 16, "U"},
		{3802, "jsonb", -1, "U"},
	}
	for _, t := range types {
		result.Rows = append(result.Rows, []interface{}{
			t.oid, t.name, int64(11), int64(10), t.len, "b", t.cat, "f", int64(0), int64(0), t.name + "in", t.name + "out",
		})
	}
	result.Tag = fmt.Sprintf("SELECT %d", len(result.Rows))
	return result, true
}

// ─────────────────────────────────────────────────────────────────────────────
//  pg_namespace
// ─────────────────────────────────────────────────────────────────────────────

func (vc *VirtualCatalog) pgNamespace() (*pgproto.QueryResult, bool) {
	result := &pgproto.QueryResult{
		Columns: []pgproto.ColumnDesc{
			{Name: "oid", TypeOID: 26, TypeSize: 4},
			{Name: "nspname", TypeOID: 25, TypeSize: -1},
			{Name: "nspowner", TypeOID: 26, TypeSize: 4},
		},
		Rows: [][]interface{}{
			{int64(11), "pg_catalog", int64(10)},
			{int64(2200), "public", int64(10)},
			{int64(99), "information_schema", int64(10)},
		},
		Tag: "SELECT 3",
	}
	return result, true
}

// ─────────────────────────────────────────────────────────────────────────────
//  pg_constraint
// ─────────────────────────────────────────────────────────────────────────────

func (vc *VirtualCatalog) pgConstraint(_ context.Context, _ *sql.Conn) (*pgproto.QueryResult, bool) {
	// Return empty result — most ORMs fall back gracefully.
	result := &pgproto.QueryResult{
		Columns: []pgproto.ColumnDesc{
			{Name: "oid", TypeOID: 26, TypeSize: 4},
			{Name: "conname", TypeOID: 25, TypeSize: -1},
			{Name: "connamespace", TypeOID: 26, TypeSize: 4},
			{Name: "contype", TypeOID: 18, TypeSize: 1},
			{Name: "conrelid", TypeOID: 26, TypeSize: 4},
			{Name: "conkey", TypeOID: 1007, TypeSize: -1},
			{Name: "confrelid", TypeOID: 26, TypeSize: 4},
			{Name: "confkey", TypeOID: 1007, TypeSize: -1},
		},
		Tag: "SELECT 0",
	}
	return result, true
}

// ─────────────────────────────────────────────────────────────────────────────
//  pg_index
// ─────────────────────────────────────────────────────────────────────────────

func (vc *VirtualCatalog) pgIndex(_ context.Context, _ *sql.Conn) (*pgproto.QueryResult, bool) {
	result := &pgproto.QueryResult{
		Columns: []pgproto.ColumnDesc{
			{Name: "indexrelid", TypeOID: 26, TypeSize: 4},
			{Name: "indrelid", TypeOID: 26, TypeSize: 4},
			{Name: "indnatts", TypeOID: 21, TypeSize: 2},
			{Name: "indisunique", TypeOID: 16, TypeSize: 1},
			{Name: "indisprimary", TypeOID: 16, TypeSize: 1},
			{Name: "indkey", TypeOID: 22, TypeSize: -1},
		},
		Tag: "SELECT 0",
	}
	return result, true
}

// ─────────────────────────────────────────────────────────────────────────────
//  SQLite schema helpers
// ─────────────────────────────────────────────────────────────────────────────

// columnInfo holds information about one column from PRAGMA table_info.
type columnInfo struct {
	CID     int
	Name    string
	Type    string
	NotNull bool
	Default string
	PK      int
}

// listTables returns all user-defined table names.
func listTables(ctx context.Context, db *sql.Conn) ([]string, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err == nil {
			tables = append(tables, name)
		}
	}
	return tables, rows.Err()
}

// tableColumns returns the columns of a table via PRAGMA table_info.
func tableColumns(ctx context.Context, db *sql.Conn, table string) ([]columnInfo, error) {
	rows, err := db.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%q)", table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cols []columnInfo
	for rows.Next() {
		var col columnInfo
		var dflt sql.NullString
		if err := rows.Scan(&col.CID, &col.Name, &col.Type, &col.NotNull, &dflt, &col.PK); err != nil {
			continue
		}
		if dflt.Valid {
			col.Default = dflt.String
		}
		cols = append(cols, col)
	}
	return cols, rows.Err()
}

// sqliteTypeToPGType maps a SQLite type name to a PostgreSQL type name.
func sqliteTypeToPGType(t string) string {
	upper := strings.ToUpper(strings.TrimSpace(t))
	switch {
	case upper == "" || upper == "BLOB":
		return "bytea"
	case strings.HasPrefix(upper, "INT"):
		return "integer"
	case upper == "REAL" || upper == "FLOAT" || upper == "DOUBLE":
		return "double precision"
	case upper == "NUMERIC" || upper == "DECIMAL":
		return "numeric"
	case upper == "BOOLEAN" || upper == "BOOL":
		return "boolean"
	case upper == "DATE":
		return "date"
	case upper == "DATETIME" || upper == "TIMESTAMP":
		return "timestamp without time zone"
	case upper == "UUID":
		return "uuid"
	case upper == "JSON" || upper == "JSONB":
		return "jsonb"
	default:
		return "text"
	}
}

// ─────────────────────────────────────────────────────────────────────────────
//  Result construction helpers
// ─────────────────────────────────────────────────────────────────────────────

func singleRow(cols []pgproto.ColumnDesc, values []interface{}) *pgproto.QueryResult {
	return &pgproto.QueryResult{
		Columns: cols,
		Rows:    [][]interface{}{values},
		Tag:     "SELECT 1",
	}
}

func emptyResult(tag string) *pgproto.QueryResult {
	if tag == "" {
		tag = "SELECT 0"
	}
	return &pgproto.QueryResult{Tag: tag}
}

// extractStringLiteral extracts the value of the first single-quoted string
// from a SQL fragment.
func extractStringLiteral(s string) string {
	s = strings.TrimSpace(s)
	if len(s) == 0 {
		return ""
	}
	if s[0] == '\'' {
		end := strings.Index(s[1:], "'")
		if end >= 0 {
			return s[1 : end+1]
		}
	}
	return ""
}
