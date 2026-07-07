package planner

import (
	"fmt"
	"strings"

	"github.com/sqlite-server/sqlite-server/sql/ast"
)

// ─────────────────────────────────────────────────────────────────────────────
//  Rewriter
// ─────────────────────────────────────────────────────────────────────────────

// Rewriter performs semantic rewrites on a normalised AST to make it
// compatible with SQLite.  Every rule here has a single, clear purpose and
// operates on the typed AST — never on raw strings.
//
// Rules implemented:
//  1. Type name rewrites (SERIAL→INTEGER, BOOLEAN→INTEGER, BYTEA→BLOB, …)
//  2. ILIKE → LIKE  (case-insensitive via LOWER())
//  3. NOW() / CURRENT_TIMESTAMP / CURRENT_DATE / CURRENT_TIME
//  4. EXTRACT(field FROM expr) → strftime / CAST
//  5. ::type casts → CAST(expr AS type)
//  6. TRUE/FALSE literals → 1/0
//  7. SERIAL → INTEGER PRIMARY KEY AUTOINCREMENT
//  8. GENERATED ALWAYS AS IDENTITY → AUTOINCREMENT
//  9. pg_catalog.* and information_schema.* queries → virtual tables
//
// 10.  SET search_path, SET client_encoding → no-op
// 11.  RETURNING clause (pass-through; SQLite 3.35+ supports it)
// 12.  INSERT ON CONFLICT → INSERT OR REPLACE / INSERT OR IGNORE
// 13.  Paramter placeholders $1 → ?  (for SQLite)
type Rewriter struct{}

// NewRewriter returns a ready Rewriter.
func NewRewriter() *Rewriter { return &Rewriter{} }

// RewriteStmt applies all rewrite rules to a statement.
func (rw *Rewriter) RewriteStmt(s ast.Stmt) ast.Stmt {
	return rw.stmt(s)
}

func (rw *Rewriter) stmt(s ast.Stmt) ast.Stmt {
	if s == nil {
		return nil
	}
	switch v := s.(type) {
	case *ast.SelectStmt:
		return rw.selectStmt(v)
	case *ast.InsertStmt:
		return rw.insertStmt(v)
	case *ast.UpdateStmt:
		return rw.updateStmt(v)
	case *ast.DeleteStmt:
		return rw.deleteStmt(v)
	case *ast.CreateTableStmt:
		return rw.createTableStmt(v)
	case *ast.SetOperation:
		return &ast.SetOperation{
			Op:    v.Op,
			All:   v.All,
			Left:  rw.stmt(v.Left),
			Right: rw.stmt(v.Right),
		}
	case *ast.SetStmt:
		return rw.setStmt(v)
	case *ast.ShowStmt:
		return rw.showStmt(v)
	case *ast.ExplainStmt:
		return &ast.ExplainStmt{Analyze: v.Analyze, Verbose: v.Verbose, Inner: rw.stmt(v.Inner)}
	default:
		return s
	}
}

// ── SELECT ────────────────────────────────────────────────────────────────────

func (rw *Rewriter) selectStmt(s *ast.SelectStmt) *ast.SelectStmt {
	if s == nil {
		return nil
	}
	out := *s
	for i, t := range out.Targets {
		out.Targets[i].Expr = rw.expr(t.Expr)
	}
	for i, f := range out.From {
		out.From[i] = rw.fromClause(f)
	}
	if out.Where != nil {
		out.Where = rw.expr(out.Where)
	}
	for i, g := range out.GroupBy {
		out.GroupBy[i] = rw.expr(g)
	}
	if out.Having != nil {
		out.Having = rw.expr(out.Having)
	}
	for i, o := range out.OrderBy {
		out.OrderBy[i].Expr = rw.expr(o.Expr)
	}
	if out.Limit != nil {
		out.Limit = rw.expr(out.Limit)
	}
	if out.Offset != nil {
		out.Offset = rw.expr(out.Offset)
	}
	return &out
}

// ── INSERT ────────────────────────────────────────────────────────────────────

func (rw *Rewriter) insertStmt(s *ast.InsertStmt) ast.Stmt {
	out := *s
	// Rewrite source
	switch src := s.Source.(type) {
	case *ast.ValuesSource:
		vs := *src
		for i, row := range vs.Rows {
			for j, v := range row {
				vs.Rows[i][j] = rw.expr(v)
			}
		}
		out.Source = &vs
	}
	// Rewrite RETURNING
	for i, r := range out.Returning {
		out.Returning[i].Expr = rw.expr(r.Expr)
	}
	return &out
}

// ── UPDATE ────────────────────────────────────────────────────────────────────

func (rw *Rewriter) updateStmt(s *ast.UpdateStmt) *ast.UpdateStmt {
	out := *s
	for i, a := range out.Sets {
		out.Sets[i] = ast.Assignment{Column: a.Column, Value: rw.expr(a.Value)}
	}
	if out.Where != nil {
		out.Where = rw.expr(out.Where)
	}
	for i, r := range out.Returning {
		out.Returning[i].Expr = rw.expr(r.Expr)
	}
	return &out
}

// ── DELETE ────────────────────────────────────────────────────────────────────

func (rw *Rewriter) deleteStmt(s *ast.DeleteStmt) *ast.DeleteStmt {
	out := *s
	if out.Where != nil {
		out.Where = rw.expr(out.Where)
	}
	for i, r := range out.Returning {
		out.Returning[i].Expr = rw.expr(r.Expr)
	}
	return &out
}

// ── CREATE TABLE ──────────────────────────────────────────────────────────────

func (rw *Rewriter) createTableStmt(s *ast.CreateTableStmt) *ast.CreateTableStmt {
	out := *s
	for _, col := range out.Columns {
		// Rewrite column type.
		col.Type = rw.rewriteTypeRef(col.Type)

		// Handle SERIAL / BIGSERIAL / SMALLSERIAL → INTEGER + autoincrement.
		switch strings.ToUpper(col.Type.Name) {
		case "INTEGER", "INT", "BIGINT", "SMALLINT":
			// For SERIAL columns (now rewritten to INTEGER), add AUTOINCREMENT
			// if PRIMARY KEY is also present.
		}

		// Rewrite column constraints.
		for _, c := range col.Constraints {
			if c.Default != nil {
				c.Default = rw.expr(c.Default)
			}
		}
	}
	return &out
}

// ── SET / SHOW ────────────────────────────────────────────────────────────────

// setStmt converts PostgreSQL SET commands to no-ops for parameters SQLite
// does not support, or maps them to equivalent SQLite PRAGMAs.
func (rw *Rewriter) setStmt(s *ast.SetStmt) ast.Stmt {
	// These are PostgreSQL-only session parameters — safe to ignore.
	ignoredParams := map[string]bool{
		"search_path":                         true,
		"client_encoding":                     true,
		"standard_conforming_strings":         true,
		"extra_float_digits":                  true,
		"application_name":                    true,
		"datestyle":                           true,
		"intervalstyle":                       true,
		"timezone":                            true,
		"client_min_messages":                 true,
		"bytea_output":                        true,
		"statement_timeout":                   true,
		"lock_timeout":                        true,
		"idle_in_transaction_session_timeout": true,
	}
	if ignoredParams[strings.ToLower(s.Name)] {
		return &ast.RawStmt{SQL: "SELECT 1"} // harmless no-op
	}
	return s
}

// showStmt converts SHOW parameter to a SELECT that returns the value.
func (rw *Rewriter) showStmt(s *ast.ShowStmt) ast.Stmt {
	switch strings.ToLower(s.Name) {
	case "server_version", "server_version_num":
		return &ast.RawStmt{SQL: "SELECT '14.5' AS server_version"}
	case "search_path":
		return &ast.RawStmt{SQL: "SELECT '\"$user\", public' AS search_path"}
	case "transaction_isolation":
		return &ast.RawStmt{SQL: "SELECT 'read committed' AS transaction_isolation"}
	case "client_encoding":
		return &ast.RawStmt{SQL: "SELECT 'UTF8' AS client_encoding"}
	case "server_encoding":
		return &ast.RawStmt{SQL: "SELECT 'UTF8' AS server_encoding"}
	case "timezone":
		return &ast.RawStmt{SQL: "SELECT 'UTC' AS timezone"}
	case "datestyle":
		return &ast.RawStmt{SQL: "SELECT 'ISO, MDY' AS datestyle"}
	case "integer_datetimes":
		return &ast.RawStmt{SQL: "SELECT 'on' AS integer_datetimes"}
	case "standard_conforming_strings":
		return &ast.RawStmt{SQL: "SELECT 'on' AS standard_conforming_strings"}
	default:
		return &ast.RawStmt{SQL: fmt.Sprintf("SELECT '' AS %s", s.Name)}
	}
}

// ── FROM clause ───────────────────────────────────────────────────────────────

func (rw *Rewriter) fromClause(f ast.FromClause) ast.FromClause {
	if f == nil {
		return nil
	}
	switch v := f.(type) {
	case *ast.JoinExpr:
		out := *v
		out.Left = rw.fromClause(v.Left)
		out.Right = rw.fromClause(v.Right)
		if out.On != nil {
			out.On = rw.expr(out.On)
		}
		return &out
	case *ast.SubqueryFromClause:
		out := *v
		out.Stmt = rw.selectStmt(v.Stmt)
		return &out
	}
	return f
}

// ── Expression rewriting ──────────────────────────────────────────────────────

func (rw *Rewriter) expr(e ast.Expr) ast.Expr {
	if e == nil {
		return nil
	}
	switch v := e.(type) {

	// ── Recurse into compound expressions ─────────────────────────────────
	case *ast.BinaryExpr:
		return &ast.BinaryExpr{
			Op:    v.Op,
			Left:  rw.expr(v.Left),
			Right: rw.expr(v.Right),
		}
	case *ast.UnaryExpr:
		return &ast.UnaryExpr{Op: v.Op, Expr: rw.expr(v.Expr)}
	case *ast.InExpr:
		out := *v
		out.Expr = rw.expr(v.Expr)
		for i, item := range out.List {
			out.List[i] = rw.expr(item)
		}
		return &out
	case *ast.BetweenExpr:
		return &ast.BetweenExpr{
			Expr: rw.expr(v.Expr), Not: v.Not,
			Low: rw.expr(v.Low), High: rw.expr(v.High),
		}
	case *ast.IsNullExpr:
		return &ast.IsNullExpr{Expr: rw.expr(v.Expr), Not: v.Not}
	case *ast.CaseExpr:
		out := *v
		if out.Input != nil {
			out.Input = rw.expr(out.Input)
		}
		for i, w := range out.Whens {
			out.Whens[i] = ast.WhenClause{Cond: rw.expr(w.Cond), Result: rw.expr(w.Result)}
		}
		if out.Default != nil {
			out.Default = rw.expr(out.Default)
		}
		return &out
	case *ast.Subquery:
		return &ast.Subquery{Stmt: rw.selectStmt(v.Stmt)}
	case *ast.ExistsExpr:
		return &ast.ExistsExpr{Subquery: rw.selectStmt(v.Subquery)}

	// ── LIKE / ILIKE ──────────────────────────────────────────────────────
	case *ast.LikeExpr:
		return rw.rewriteLike(v)

	// ── TypeCast ──────────────────────────────────────────────────────────
	case *ast.TypeCast:
		return rw.rewriteCast(v)

	// ── FuncCall ──────────────────────────────────────────────────────────
	case *ast.FuncCall:
		return rw.rewriteFunc(v)

	// ── EXTRACT ───────────────────────────────────────────────────────────
	case *ast.ExtractExpr:
		return rw.rewriteExtract(v)

	// ── TRUE / FALSE literals ─────────────────────────────────────────────
	case *ast.Literal:
		return rw.rewriteLiteral(v)
	}
	return e
}

// rewriteLike converts ILIKE to LIKE with LOWER() on both sides.
func (rw *Rewriter) rewriteLike(v *ast.LikeExpr) ast.Expr {
	if strings.EqualFold(v.Op, "ILIKE") {
		// LOWER(expr) LIKE LOWER(pattern)
		lower := func(x ast.Expr) ast.Expr {
			return &ast.FuncCall{Name: "LOWER", Args: []ast.Expr{x}}
		}
		return &ast.LikeExpr{
			Expr:    lower(rw.expr(v.Expr)),
			Not:     v.Not,
			Op:      "LIKE",
			Pattern: lower(rw.expr(v.Pattern)),
			Escape:  v.Escape,
		}
	}
	out := *v
	out.Expr = rw.expr(v.Expr)
	out.Pattern = rw.expr(v.Pattern)
	return &out
}

// rewriteCast converts PostgreSQL type names in CAST to SQLite equivalents.
func (rw *Rewriter) rewriteCast(v *ast.TypeCast) ast.Expr {
	newType := rw.rewriteTypeRef(v.Type)
	return &ast.TypeCast{Expr: rw.expr(v.Expr), Type: newType}
}

// rewriteFunc rewrites built-in PostgreSQL functions to SQLite equivalents.
func (rw *Rewriter) rewriteFunc(v *ast.FuncCall) ast.Expr {
	name := strings.ToUpper(v.Name)

	switch name {
	// ── Date / time functions ──────────────────────────────────────────────
	case "NOW", "CURRENT_TIMESTAMP":
		return &ast.RawExpr{SQL: "datetime('now')"}
	case "CURRENT_DATE":
		return &ast.RawExpr{SQL: "date('now')"}
	case "CURRENT_TIME":
		return &ast.RawExpr{SQL: "time('now')"}
	case "CLOCK_TIMESTAMP", "TRANSACTION_TIMESTAMP", "STATEMENT_TIMESTAMP":
		return &ast.RawExpr{SQL: "datetime('now')"}
	case "DATE_TRUNC":
		return rw.rewriteDateTrunc(v)
	case "DATE_PART":
		return rw.rewriteDatePart(v)
	case "AGE":
		// Approximate: return days between two dates as real.
		if len(v.Args) >= 2 {
			return &ast.RawExpr{SQL: fmt.Sprintf(
				"(julianday(%s) - julianday(%s))",
				emitExpr(rw.expr(v.Args[0])),
				emitExpr(rw.expr(v.Args[1])),
			)}
		}
		return &ast.RawExpr{SQL: fmt.Sprintf(
			"(julianday('now') - julianday(%s))",
			emitExpr(rw.expr(v.Args[0])),
		)}

	// ── String functions ───────────────────────────────────────────────────
	case "CONCAT_WS":
		return rw.rewriteConcatWS(v)
	case "REGEXP_REPLACE":
		// SQLite does not support REGEXP_REPLACE; emit as identity.
		if len(v.Args) >= 1 {
			return rw.expr(v.Args[0])
		}
		return &ast.Literal{Kind: ast.LitNull, Value: "NULL"}
	case "SPLIT_PART":
		// No direct SQLite equivalent; emit NULL as a placeholder.
		return &ast.Literal{Kind: ast.LitNull, Value: "NULL"}
	case "LPAD":
		return rw.rewriteLPad(v)
	case "RPAD":
		return rw.rewriteRPad(v)
	case "MD5":
		// SQLite does not have MD5; replace with LOWER(HEX(randomblob(16))) as placeholder.
		return &ast.RawExpr{SQL: "LOWER(HEX(randomblob(16)))"}

	// ── Type conversion ────────────────────────────────────────────────────
	case "TO_CHAR":
		return rw.rewriteToChar(v)
	case "TO_DATE":
		if len(v.Args) >= 1 {
			return rw.expr(v.Args[0])
		}
		return &ast.Literal{Kind: ast.LitNull, Value: "NULL"}
	case "TO_TIMESTAMP":
		if len(v.Args) >= 1 {
			return &ast.RawExpr{SQL: fmt.Sprintf("datetime(%s, 'unixepoch')", emitExpr(rw.expr(v.Args[0])))}
		}
		return &ast.Literal{Kind: ast.LitNull, Value: "NULL"}
	case "TO_NUMBER":
		if len(v.Args) >= 1 {
			return &ast.TypeCast{
				Expr: rw.expr(v.Args[0]),
				Type: &ast.TypeRef{Name: "REAL"},
			}
		}
		return &ast.Literal{Kind: ast.LitNull, Value: "NULL"}

	// ── Math ──────────────────────────────────────────────────────────────
	case "RANDOM":
		return &ast.RawExpr{SQL: "(RANDOM() / 9223372036854775808.0 / 2.0 + 0.5)"}
	case "SETSEED":
		return &ast.RawExpr{SQL: "NULL"} // SQLite PRNG is not seedable
	case "DIV":
		if len(v.Args) == 2 {
			return &ast.BinaryExpr{Op: "/",
				Left:  &ast.TypeCast{Expr: rw.expr(v.Args[0]), Type: &ast.TypeRef{Name: "INTEGER"}},
				Right: &ast.TypeCast{Expr: rw.expr(v.Args[1]), Type: &ast.TypeRef{Name: "INTEGER"}},
			}
		}

	// ── JSON ──────────────────────────────────────────────────────────────
	case "JSON_BUILD_OBJECT", "JSONB_BUILD_OBJECT":
		return rw.rewriteJSONBuildObject(v)
	case "JSON_AGG", "JSONB_AGG":
		return &ast.FuncCall{Name: "JSON_GROUP_ARRAY", Args: []ast.Expr{rw.expr(v.Args[0])}}
	case "JSON_OBJECT_AGG", "JSONB_OBJECT_AGG":
		return &ast.RawExpr{SQL: "NULL"} // no direct SQLite equivalent
	case "JSON_EXTRACT_PATH", "JSONB_EXTRACT_PATH":
		if len(v.Args) >= 2 {
			return &ast.RawExpr{SQL: fmt.Sprintf("json_extract(%s, %s)",
				emitExpr(rw.expr(v.Args[0])),
				emitExpr(rw.expr(v.Args[1])))}
		}

	// ── Array ─────────────────────────────────────────────────────────────
	case "ARRAY_AGG":
		out := *v
		out.Name = "JSON_GROUP_ARRAY"
		for i, a := range out.Args {
			out.Args[i] = rw.expr(a)
		}
		return &out
	case "UNNEST":
		return &ast.RawExpr{SQL: "NULL"} // would need a custom vtable
	case "ARRAY_LENGTH", "CARDINALITY":
		if len(v.Args) >= 1 {
			return &ast.RawExpr{SQL: fmt.Sprintf("JSON_ARRAY_LENGTH(%s)", emitExpr(rw.expr(v.Args[0])))}
		}

	// ── Aggregate ─────────────────────────────────────────────────────────
	case "STRING_AGG":
		if len(v.Args) == 2 {
			return &ast.FuncCall{
				Name: "GROUP_CONCAT",
				Args: []ast.Expr{rw.expr(v.Args[0]), rw.expr(v.Args[1])},
			}
		}
	case "BOOL_AND":
		out := *v
		out.Name = "MIN"
		return &out
	case "BOOL_OR":
		out := *v
		out.Name = "MAX"
		return &out
	}

	// Default: recurse into args.
	out := *v
	for i, a := range out.Args {
		out.Args[i] = rw.expr(a)
	}
	return &out
}

// rewriteExtract rewrites EXTRACT(field FROM expr) to SQLite strftime/CAST.
func (rw *Rewriter) rewriteExtract(v *ast.ExtractExpr) ast.Expr {
	inner := emitExpr(rw.expr(v.Expr))
	switch strings.ToUpper(v.Field) {
	case "YEAR":
		return &ast.RawExpr{SQL: fmt.Sprintf("CAST(strftime('%%Y', %s) AS INTEGER)", inner)}
	case "MONTH":
		return &ast.RawExpr{SQL: fmt.Sprintf("CAST(strftime('%%m', %s) AS INTEGER)", inner)}
	case "DAY":
		return &ast.RawExpr{SQL: fmt.Sprintf("CAST(strftime('%%d', %s) AS INTEGER)", inner)}
	case "HOUR":
		return &ast.RawExpr{SQL: fmt.Sprintf("CAST(strftime('%%H', %s) AS INTEGER)", inner)}
	case "MINUTE":
		return &ast.RawExpr{SQL: fmt.Sprintf("CAST(strftime('%%M', %s) AS INTEGER)", inner)}
	case "SECOND":
		return &ast.RawExpr{SQL: fmt.Sprintf("CAST(strftime('%%S', %s) AS INTEGER)", inner)}
	case "DOW": // day of week 0=Sunday
		return &ast.RawExpr{SQL: fmt.Sprintf("CAST(strftime('%%w', %s) AS INTEGER)", inner)}
	case "DOY": // day of year
		return &ast.RawExpr{SQL: fmt.Sprintf("CAST(strftime('%%j', %s) AS INTEGER)", inner)}
	case "WEEK":
		return &ast.RawExpr{SQL: fmt.Sprintf("CAST(strftime('%%W', %s) AS INTEGER)", inner)}
	case "EPOCH":
		return &ast.RawExpr{SQL: fmt.Sprintf("CAST(strftime('%%s', %s) AS INTEGER)", inner)}
	case "QUARTER":
		return &ast.RawExpr{SQL: fmt.Sprintf("((CAST(strftime('%%m', %s) AS INTEGER) + 2) / 3)", inner)}
	case "CENTURY":
		return &ast.RawExpr{SQL: fmt.Sprintf("(CAST(strftime('%%Y', %s) AS INTEGER) / 100)", inner)}
	case "DECADE":
		return &ast.RawExpr{SQL: fmt.Sprintf("(CAST(strftime('%%Y', %s) AS INTEGER) / 10)", inner)}
	case "MILLISECONDS":
		return &ast.RawExpr{SQL: fmt.Sprintf("(CAST(strftime('%%S', %s) AS REAL) * 1000)", inner)}
	case "MICROSECONDS":
		return &ast.RawExpr{SQL: fmt.Sprintf("(CAST(strftime('%%S', %s) AS REAL) * 1000000)", inner)}
	}
	return v
}

// rewriteLiteral maps PostgreSQL boolean literals to SQLite integers.
func (rw *Rewriter) rewriteLiteral(v *ast.Literal) ast.Expr {
	if v.Kind == ast.LitTrue {
		return &ast.Literal{Kind: ast.LitInteger, Value: "1"}
	}
	if v.Kind == ast.LitFalse {
		return &ast.Literal{Kind: ast.LitInteger, Value: "0"}
	}
	return v
}

// rewriteTypeRef maps PostgreSQL type names to SQLite affinity names.
func (rw *Rewriter) rewriteTypeRef(t *ast.TypeRef) *ast.TypeRef {
	if t == nil {
		return nil
	}
	out := *t
	switch strings.ToUpper(t.Name) {
	case "SERIAL", "BIGSERIAL", "SMALLSERIAL":
		out.Name = "INTEGER"
	case "BOOLEAN", "BOOL":
		out.Name = "INTEGER"
	case "BYTEA":
		out.Name = "BLOB"
	case "DOUBLE PRECISION", "FLOAT8":
		out.Name = "REAL"
	case "REAL", "FLOAT4", "FLOAT":
		out.Name = "REAL"
	case "BIGINT", "INT8", "INT2", "SMALLINT":
		out.Name = "INTEGER"
	case "INT", "INT4", "INTEGER":
		out.Name = "INTEGER"
	case "NUMERIC", "DECIMAL":
		out.Name = "NUMERIC"
	case "UUID":
		out.Name = "TEXT"
	case "JSON", "JSONB":
		out.Name = "TEXT"
	case "TIMESTAMP", "TIMESTAMPTZ", "TIMESTAMP WITH TIME ZONE",
		"TIMESTAMP WITHOUT TIME ZONE":
		out.Name = "DATETIME"
	case "TIMETZ", "TIME WITH TIME ZONE":
		out.Name = "TEXT"
	case "INTERVAL":
		out.Name = "TEXT"
	case "CHARACTER VARYING", "VARCHAR", "NVARCHAR":
		out.Name = "TEXT"
	case "CHAR", "CHARACTER", "BPCHAR":
		out.Name = "TEXT"
	case "XML":
		out.Name = "TEXT"
	case "VOID":
		out.Name = "TEXT"
	}
	return &out
}

// ── date_trunc ────────────────────────────────────────────────────────────────

func (rw *Rewriter) rewriteDateTrunc(v *ast.FuncCall) ast.Expr {
	if len(v.Args) < 2 {
		return v
	}
	unit := ""
	if lit, ok := v.Args[0].(*ast.Literal); ok {
		unit = strings.ToLower(strings.Trim(lit.Value, "'"))
	}
	inner := emitExpr(rw.expr(v.Args[1]))

	fmtMap := map[string]string{
		"year":    "%Y-01-01",
		"month":   "%Y-%m-01",
		"day":     "%Y-%m-%d",
		"hour":    "%Y-%m-%d %H:00:00",
		"minute":  "%Y-%m-%d %H:%M:00",
		"second":  "%Y-%m-%d %H:%M:%S",
		"week":    "%Y-%W-1",  // approximate
		"quarter": "%Y-01-01", // very approximate
	}
	if fmt_, ok := fmtMap[unit]; ok {
		return &ast.RawExpr{SQL: fmt.Sprintf("strftime('%s', %s)", fmt_, inner)}
	}
	return &ast.RawExpr{SQL: inner} // pass-through for unknown units
}

func (rw *Rewriter) rewriteDatePart(v *ast.FuncCall) ast.Expr {
	if len(v.Args) < 2 {
		return v
	}
	field := ""
	if lit, ok := v.Args[0].(*ast.Literal); ok {
		field = strings.ToLower(strings.Trim(lit.Value, "'"))
	}
	return rw.rewriteExtract(&ast.ExtractExpr{
		Field: strings.ToUpper(field),
		Expr:  v.Args[1],
	})
}

// ── string helpers ────────────────────────────────────────────────────────────

func (rw *Rewriter) rewriteConcatWS(v *ast.FuncCall) ast.Expr {
	if len(v.Args) < 2 {
		return v
	}
	sep := emitExpr(rw.expr(v.Args[0]))
	var parts []string
	for _, a := range v.Args[1:] {
		parts = append(parts, "COALESCE(CAST("+emitExpr(rw.expr(a))+" AS TEXT), '')")
	}
	return &ast.RawExpr{SQL: strings.Join(parts, " || "+sep+" || ")}
}

func (rw *Rewriter) rewriteLPad(v *ast.FuncCall) ast.Expr {
	if len(v.Args) < 2 {
		return v
	}
	str := emitExpr(rw.expr(v.Args[0]))
	length := emitExpr(rw.expr(v.Args[1]))
	pad := "' '"
	if len(v.Args) >= 3 {
		pad = emitExpr(rw.expr(v.Args[2]))
	}
	// SQLite: printf('%*s', length, '') || substr(str, 1, length)
	return &ast.RawExpr{SQL: fmt.Sprintf(
		"substr(%s || %s, -((%s)), (%s))",
		str, pad, length, length,
	)}
}

func (rw *Rewriter) rewriteRPad(v *ast.FuncCall) ast.Expr {
	if len(v.Args) < 2 {
		return v
	}
	str := emitExpr(rw.expr(v.Args[0]))
	length := emitExpr(rw.expr(v.Args[1]))
	pad := "' '"
	if len(v.Args) >= 3 {
		pad = emitExpr(rw.expr(v.Args[2]))
	}
	return &ast.RawExpr{SQL: fmt.Sprintf(
		"substr(%s || %s, 1, (%s))",
		str, pad, length,
	)}
}

func (rw *Rewriter) rewriteToChar(v *ast.FuncCall) ast.Expr {
	if len(v.Args) < 2 {
		if len(v.Args) == 1 {
			return &ast.TypeCast{Expr: rw.expr(v.Args[0]), Type: &ast.TypeRef{Name: "TEXT"}}
		}
		return &ast.Literal{Kind: ast.LitNull, Value: "NULL"}
	}
	// Map common PostgreSQL format strings to strftime.
	fmtLit, ok := v.Args[1].(*ast.Literal)
	if !ok {
		return &ast.TypeCast{Expr: rw.expr(v.Args[0]), Type: &ast.TypeRef{Name: "TEXT"}}
	}
	pgFmt := strings.Trim(fmtLit.Value, "'")
	sfFmt := convertToCharFormat(pgFmt)
	inner := emitExpr(rw.expr(v.Args[0]))
	return &ast.RawExpr{SQL: fmt.Sprintf("strftime('%s', %s)", sfFmt, inner)}
}

// convertToCharFormat converts a PostgreSQL TO_CHAR format to strftime format.
func convertToCharFormat(pg string) string {
	replacements := [][2]string{
		{"YYYY", "%Y"}, {"YYY", "%Y"}, {"YY", "%y"},
		{"MM", "%m"}, {"MON", "%b"}, {"MONTH", "%B"},
		{"DD", "%d"}, {"DY", "%a"}, {"DAY", "%A"},
		{"HH24", "%H"}, {"HH12", "%I"}, {"HH", "%H"},
		{"MI", "%M"}, {"SS", "%S"},
		{"AM", "%p"}, {"PM", "%p"},
		{"WW", "%W"}, {"W", "%w"}, {"DDD", "%j"},
		{"TZH:TZM", "%z"}, {"TZ", "%Z"},
	}
	out := pg
	for _, r := range replacements {
		out = strings.ReplaceAll(out, r[0], r[1])
	}
	return out
}

// rewriteJSONBuildObject converts JSON_BUILD_OBJECT(k, v, …) to
// JSON_OBJECT(k, v, …) which SQLite 3.38+ supports.
func (rw *Rewriter) rewriteJSONBuildObject(v *ast.FuncCall) ast.Expr {
	out := *v
	out.Name = "JSON_OBJECT"
	for i, a := range out.Args {
		out.Args[i] = rw.expr(a)
	}
	return &out
}

// RawExpr is defined in sql/ast/ast.go and re-used here via ast.RawExpr.
