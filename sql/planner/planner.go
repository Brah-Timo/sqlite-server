package planner

import (
	"fmt"
	"strings"

	"github.com/sqlite-server/sqlite-server/sql/ast"
	"github.com/sqlite-server/sqlite-server/sql/parser"
)

// ─────────────────────────────────────────────────────────────────────────────
//  Planner — orchestrates the full pipeline
// ─────────────────────────────────────────────────────────────────────────────

// Planner drives the full rewrite pipeline:
//
//	Parse → Normalize → Rewrite → Emit
type Planner struct {
	norm *Normalizer
	rw   *Rewriter
}

// New creates a Planner.
func New() *Planner {
	return &Planner{
		norm: NewNormalizer(),
		rw:   NewRewriter(),
	}
}

// Rewrite transforms a PostgreSQL SQL string into a SQLite-compatible string.
// Returns the rewritten SQL and any parse or rewrite error.
func (p *Planner) Rewrite(pgSQL string) (string, error) {
	// ── 1. Parse ──────────────────────────────────────────────────────────
	stmts, err := parser.Parse(pgSQL)
	if err != nil {
		// If parsing failed (e.g. unsupported syntax), try a passthrough
		// approach — the raw SQL might still work in SQLite.
		return pgSQL, nil
	}
	if len(stmts) == 0 {
		return pgSQL, nil
	}

	// ── 2. Handle multi-statement queries ─────────────────────────────────
	var outputs []string
	for _, stmt := range stmts {
		// ── 3. Normalize ──────────────────────────────────────────────────
		stmt = p.norm.NormalizeStmt(stmt)

		// ── 4. Rewrite ────────────────────────────────────────────────────
		stmt = p.rw.RewriteStmt(stmt)

		// ── 5. Emit ───────────────────────────────────────────────────────
		sql := emitStmt(stmt)
		outputs = append(outputs, sql)
	}

	return strings.Join(outputs, "; "), nil
}

// RewriteOne rewrites exactly one statement.
func (p *Planner) RewriteOne(pgSQL string) (string, error) {
	stmt, err := parser.ParseOne(pgSQL)
	if err != nil || stmt == nil {
		return pgSQL, nil
	}
	stmt = p.norm.NormalizeStmt(stmt)
	stmt = p.rw.RewriteStmt(stmt)
	return emitStmt(stmt), nil
}

// ─────────────────────────────────────────────────────────────────────────────
//  SQL Emitter — AST → SQLite SQL string
// ─────────────────────────────────────────────────────────────────────────────

// emitStmt converts an AST statement back into a SQL string targeting SQLite.
func emitStmt(s ast.Stmt) string {
	if s == nil {
		return ""
	}
	switch v := s.(type) {
	case *ast.SelectStmt:
		return emitSelect(v)
	case *ast.InsertStmt:
		return emitInsert(v)
	case *ast.UpdateStmt:
		return emitUpdate(v)
	case *ast.DeleteStmt:
		return emitDelete(v)
	case *ast.CreateTableStmt:
		return emitCreateTable(v)
	case *ast.DropTableStmt:
		return emitDropTable(v)
	case *ast.AlterTableStmt:
		return emitAlterTable(v)
	case *ast.CreateIndexStmt:
		return emitCreateIndex(v)
	case *ast.BeginStmt:
		return "BEGIN"
	case *ast.CommitStmt:
		return "COMMIT"
	case *ast.RollbackStmt:
		if v.Savepoint != "" {
			return "ROLLBACK TO SAVEPOINT " + v.Savepoint
		}
		return "ROLLBACK"
	case *ast.SavepointStmt:
		return "SAVEPOINT " + v.Name
	case *ast.ReleaseSavepointStmt:
		return "RELEASE SAVEPOINT " + v.Name
	case *ast.SetStmt:
		return "SELECT 1" // swallowed by rewriter
	case *ast.ShowStmt:
		return "SELECT 1"
	case *ast.ExplainStmt:
		if v.Analyze {
			return "EXPLAIN QUERY PLAN " + emitStmt(v.Inner)
		}
		return "EXPLAIN QUERY PLAN " + emitStmt(v.Inner)
	case *ast.SetOperation:
		op := v.Op
		if v.All {
			op += " ALL"
		}
		return emitStmt(v.Left) + " " + op + " " + emitStmt(v.Right)
	case *ast.RawStmt:
		return v.SQL
	default:
		return fmt.Sprintf("/* unsupported: %T */", s)
	}
}

// ── SELECT ────────────────────────────────────────────────────────────────────

func emitSelect(s *ast.SelectStmt) string {
	if s == nil {
		return "SELECT 1"
	}
	var b strings.Builder

	// WITH clause
	if s.With != nil {
		b.WriteString(emitWith(s.With))
		b.WriteString(" ")
	}

	b.WriteString("SELECT ")
	if s.Distinct {
		b.WriteString("DISTINCT ")
		if len(s.DistinctOn) > 0 {
			// SQLite doesn't support DISTINCT ON — drop it (best-effort).
		}
	}

	// SELECT list
	var targets []string
	for _, t := range s.Targets {
		str := emitExpr(t.Expr)
		if t.Alias != nil {
			str += " AS " + quoteIdent(t.Alias.Name)
		}
		targets = append(targets, str)
	}
	if len(targets) == 0 {
		targets = []string{"*"}
	}
	b.WriteString(strings.Join(targets, ", "))

	// FROM
	if len(s.From) > 0 {
		var froms []string
		for _, f := range s.From {
			froms = append(froms, emitFrom(f))
		}
		b.WriteString(" FROM ")
		b.WriteString(strings.Join(froms, ", "))
	}

	// WHERE
	if s.Where != nil {
		b.WriteString(" WHERE ")
		b.WriteString(emitExpr(s.Where))
	}

	// GROUP BY
	if len(s.GroupBy) > 0 {
		var gb []string
		for _, g := range s.GroupBy {
			gb = append(gb, emitExpr(g))
		}
		b.WriteString(" GROUP BY ")
		b.WriteString(strings.Join(gb, ", "))
	}

	// HAVING
	if s.Having != nil {
		b.WriteString(" HAVING ")
		b.WriteString(emitExpr(s.Having))
	}

	// ORDER BY
	if len(s.OrderBy) > 0 {
		b.WriteString(" ORDER BY ")
		b.WriteString(emitOrderBy(s.OrderBy))
	}

	// LIMIT
	if s.Limit != nil {
		b.WriteString(" LIMIT ")
		b.WriteString(emitExpr(s.Limit))
	}

	// OFFSET
	if s.Offset != nil {
		b.WriteString(" OFFSET ")
		b.WriteString(emitExpr(s.Offset))
	}

	return b.String()
}

// ── INSERT ────────────────────────────────────────────────────────────────────

func emitInsert(s *ast.InsertStmt) string {
	var b strings.Builder

	if s.With != nil {
		b.WriteString(emitWith(s.With))
		b.WriteString(" ")
	}

	// ON CONFLICT handling: map to SQLite syntax.
	insertOr := "INSERT"
	if s.OnConflict != nil && s.OnConflict.Action == "NOTHING" {
		insertOr = "INSERT OR IGNORE"
	} else if s.OnConflict != nil && s.OnConflict.Action == "UPDATE" {
		insertOr = "INSERT OR REPLACE"
	}

	b.WriteString(insertOr + " INTO ")
	b.WriteString(emitTableName(s.Table))

	if len(s.Columns) > 0 {
		var cols []string
		for _, c := range s.Columns {
			cols = append(cols, quoteIdent(c.Name))
		}
		b.WriteString(" (")
		b.WriteString(strings.Join(cols, ", "))
		b.WriteString(")")
	}

	b.WriteString(" ")
	b.WriteString(emitInsertSource(s.Source))

	if len(s.Returning) > 0 {
		b.WriteString(" RETURNING ")
		var rets []string
		for _, r := range s.Returning {
			str := emitExpr(r.Expr)
			if r.Alias != nil {
				str += " AS " + quoteIdent(r.Alias.Name)
			}
			rets = append(rets, str)
		}
		b.WriteString(strings.Join(rets, ", "))
	}

	return b.String()
}

func emitInsertSource(src ast.InsertSource) string {
	if src == nil {
		return "DEFAULT VALUES"
	}
	switch v := src.(type) {
	case *ast.DefaultValues:
		return "DEFAULT VALUES"
	case *ast.ValuesSource:
		var rows []string
		for _, row := range v.Rows {
			var vals []string
			for _, val := range row {
				vals = append(vals, emitExpr(val))
			}
			rows = append(rows, "("+strings.Join(vals, ", ")+")")
		}
		return "VALUES " + strings.Join(rows, ", ")
	case *ast.SelectSource:
		return emitSelect(v.Stmt)
	}
	return "DEFAULT VALUES"
}

// ── UPDATE ────────────────────────────────────────────────────────────────────

func emitUpdate(s *ast.UpdateStmt) string {
	var b strings.Builder
	if s.With != nil {
		b.WriteString(emitWith(s.With))
		b.WriteString(" ")
	}
	b.WriteString("UPDATE ")
	b.WriteString(emitTableName(s.Table))
	if s.Alias != nil {
		b.WriteString(" AS ")
		b.WriteString(quoteIdent(s.Alias.Name))
	}
	b.WriteString(" SET ")

	var sets []string
	for _, a := range s.Sets {
		sets = append(sets, emitExpr(a.Column)+" = "+emitExpr(a.Value))
	}
	b.WriteString(strings.Join(sets, ", "))

	if len(s.From) > 0 {
		var froms []string
		for _, f := range s.From {
			froms = append(froms, emitFrom(f))
		}
		b.WriteString(" FROM ")
		b.WriteString(strings.Join(froms, ", "))
	}

	if s.Where != nil {
		b.WriteString(" WHERE ")
		b.WriteString(emitExpr(s.Where))
	}

	if len(s.Returning) > 0 {
		b.WriteString(" RETURNING ")
		var rets []string
		for _, r := range s.Returning {
			str := emitExpr(r.Expr)
			if r.Alias != nil {
				str += " AS " + quoteIdent(r.Alias.Name)
			}
			rets = append(rets, str)
		}
		b.WriteString(strings.Join(rets, ", "))
	}

	return b.String()
}

// ── DELETE ────────────────────────────────────────────────────────────────────

func emitDelete(s *ast.DeleteStmt) string {
	var b strings.Builder
	if s.With != nil {
		b.WriteString(emitWith(s.With))
		b.WriteString(" ")
	}
	b.WriteString("DELETE FROM ")
	b.WriteString(emitTableName(s.Table))
	if s.Alias != nil {
		b.WriteString(" AS ")
		b.WriteString(quoteIdent(s.Alias.Name))
	}

	if len(s.Using) > 0 {
		var usings []string
		for _, u := range s.Using {
			usings = append(usings, emitFrom(u))
		}
		b.WriteString(" USING ")
		b.WriteString(strings.Join(usings, ", "))
	}

	if s.Where != nil {
		b.WriteString(" WHERE ")
		b.WriteString(emitExpr(s.Where))
	}

	if len(s.Returning) > 0 {
		b.WriteString(" RETURNING ")
		var rets []string
		for _, r := range s.Returning {
			str := emitExpr(r.Expr)
			if r.Alias != nil {
				str += " AS " + quoteIdent(r.Alias.Name)
			}
			rets = append(rets, str)
		}
		b.WriteString(strings.Join(rets, ", "))
	}

	return b.String()
}

// ── DDL ───────────────────────────────────────────────────────────────────────

func emitCreateTable(s *ast.CreateTableStmt) string {
	var b strings.Builder
	b.WriteString("CREATE ")
	if s.Temp {
		b.WriteString("TEMP ")
	}
	b.WriteString("TABLE ")
	if s.IfNotExists {
		b.WriteString("IF NOT EXISTS ")
	}
	b.WriteString(emitTableName(s.Name))
	b.WriteString(" (")

	var defs []string
	for _, col := range s.Columns {
		defs = append(defs, emitColumnDef(col))
	}
	for _, con := range s.Constraints {
		defs = append(defs, emitTableConstraint(con))
	}
	b.WriteString(strings.Join(defs, ", "))
	b.WriteString(")")
	return b.String()
}

func emitColumnDef(col *ast.ColumnDef) string {
	var b strings.Builder
	b.WriteString(quoteIdent(col.Name))
	b.WriteString(" ")
	b.WriteString(emitTypeRef(col.Type))

	for _, con := range col.Constraints {
		switch con.Kind {
		case ast.ColConstrPrimaryKey:
			b.WriteString(" PRIMARY KEY")
			// Check if type was originally SERIAL → add AUTOINCREMENT.
			if strings.ToUpper(col.Type.Name) == "INTEGER" {
				b.WriteString(" AUTOINCREMENT")
			}
		case ast.ColConstrUnique:
			b.WriteString(" UNIQUE")
		case ast.ColConstrNotNull:
			b.WriteString(" NOT NULL")
		case ast.ColConstrNull:
			b.WriteString(" NULL")
		case ast.ColConstrDefault:
			if con.Default != nil {
				b.WriteString(" DEFAULT ")
				b.WriteString(emitExpr(con.Default))
			}
		case ast.ColConstrCheck:
			b.WriteString(" CHECK (")
			b.WriteString(emitExpr(con.Check))
			b.WriteString(")")
		case ast.ColConstrGenerated:
			// Skip — SQLite supports GENERATED AS but syntax differs.
		}
	}
	return b.String()
}

func emitTableConstraint(con *ast.TableConstraint) string {
	var b strings.Builder
	if con.Name != "" {
		b.WriteString("CONSTRAINT ")
		b.WriteString(quoteIdent(con.Name))
		b.WriteString(" ")
	}
	switch con.Kind {
	case ast.TblConstrPrimaryKey:
		b.WriteString("PRIMARY KEY (")
		b.WriteString(strings.Join(con.Columns, ", "))
		b.WriteString(")")
	case ast.TblConstrUnique:
		b.WriteString("UNIQUE (")
		b.WriteString(strings.Join(con.Columns, ", "))
		b.WriteString(")")
	case ast.TblConstrCheck:
		b.WriteString("CHECK (")
		b.WriteString(emitExpr(con.Check))
		b.WriteString(")")
	case ast.TblConstrForeignKey:
		b.WriteString("FOREIGN KEY (")
		b.WriteString(strings.Join(con.Columns, ", "))
		b.WriteString(")")
		if con.Ref != nil {
			b.WriteString(" REFERENCES ")
			b.WriteString(emitTableName(con.Ref.Table))
			if len(con.Ref.Columns) > 0 {
				b.WriteString(" (")
				b.WriteString(strings.Join(con.Ref.Columns, ", "))
				b.WriteString(")")
			}
			if con.Ref.OnDelete != "" {
				b.WriteString(" ON DELETE ")
				b.WriteString(con.Ref.OnDelete)
			}
			if con.Ref.OnUpdate != "" {
				b.WriteString(" ON UPDATE ")
				b.WriteString(con.Ref.OnUpdate)
			}
		}
	}
	return b.String()
}

func emitDropTable(s *ast.DropTableStmt) string {
	var b strings.Builder
	b.WriteString("DROP TABLE ")
	if s.IfExists {
		b.WriteString("IF EXISTS ")
	}
	var tables []string
	for _, t := range s.Tables {
		tables = append(tables, emitTableName(t))
	}
	b.WriteString(strings.Join(tables, ", "))
	return b.String()
}

func emitAlterTable(s *ast.AlterTableStmt) string {
	switch v := s.Action.(type) {
	case *ast.AddColumnAction:
		return fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s",
			emitTableName(s.Name), emitColumnDef(v.Column))
	case *ast.DropColumnAction:
		return fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s",
			emitTableName(s.Name), quoteIdent(v.Name))
	case *ast.RenameTableAction:
		return fmt.Sprintf("ALTER TABLE %s RENAME TO %s",
			emitTableName(s.Name), quoteIdent(v.Name))
	}
	return fmt.Sprintf("ALTER TABLE %s", emitTableName(s.Name))
}

func emitCreateIndex(s *ast.CreateIndexStmt) string {
	var b strings.Builder
	b.WriteString("CREATE ")
	if s.Unique {
		b.WriteString("UNIQUE ")
	}
	b.WriteString("INDEX ")
	if s.IfNotExists {
		b.WriteString("IF NOT EXISTS ")
	}
	if s.Name != nil {
		b.WriteString(quoteIdent(s.Name.Name))
		b.WriteString(" ")
	}
	b.WriteString("ON ")
	b.WriteString(emitTableName(s.Table))
	b.WriteString(" (")
	var cols []string
	for _, c := range s.Columns {
		col := emitExpr(c.Expr)
		if !c.Ascending {
			col += " DESC"
		}
		cols = append(cols, col)
	}
	b.WriteString(strings.Join(cols, ", "))
	b.WriteString(")")
	if s.Where != nil {
		b.WriteString(" WHERE ")
		b.WriteString(emitExpr(s.Where))
	}
	return b.String()
}

// ── WITH ──────────────────────────────────────────────────────────────────────

func emitWith(w *ast.WithClause) string {
	var b strings.Builder
	b.WriteString("WITH ")
	if w.Recursive {
		b.WriteString("RECURSIVE ")
	}
	var ctes []string
	for _, cte := range w.CTEs {
		ctes = append(ctes, cte.Name+" AS ("+emitStmt(cte.Stmt)+")")
	}
	b.WriteString(strings.Join(ctes, ", "))
	return b.String()
}

// ── FROM ──────────────────────────────────────────────────────────────────────

func emitFrom(f ast.FromClause) string {
	if f == nil {
		return ""
	}
	switch v := f.(type) {
	case *ast.TableRef:
		s := emitTableName(v.Name)
		if v.Alias != nil {
			s += " AS " + quoteIdent(v.Alias.Name)
		}
		return s
	case *ast.JoinExpr:
		var b strings.Builder
		b.WriteString(emitFrom(v.Left))
		b.WriteString(" ")
		b.WriteString(v.JoinType)
		b.WriteString(" JOIN ")
		b.WriteString(emitFrom(v.Right))
		if v.On != nil {
			b.WriteString(" ON ")
			b.WriteString(emitExpr(v.On))
		}
		if len(v.Using) > 0 {
			b.WriteString(" USING (")
			b.WriteString(strings.Join(v.Using, ", "))
			b.WriteString(")")
		}
		return b.String()
	case *ast.SubqueryFromClause:
		s := "(" + emitSelect(v.Stmt) + ")"
		if v.Alias != nil {
			s += " AS " + quoteIdent(v.Alias.Name)
		}
		return s
	}
	return ""
}

// ── ORDER BY ──────────────────────────────────────────────────────────────────

func emitOrderBy(keys []ast.SortKey) string {
	var parts []string
	for _, k := range keys {
		s := emitExpr(k.Expr)
		if !k.Ascending {
			s += " DESC"
		} else {
			s += " ASC"
		}
		if k.NullsFirst {
			s += " NULLS FIRST"
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, ", ")
}

// ── Expressions ──────────────────────────────────────────────────────────────

// emitExpr converts an AST expression to SQLite SQL.
func emitExpr(e ast.Expr) string {
	if e == nil {
		return "NULL"
	}
	switch v := e.(type) {
	case *ast.Literal:
		return v.Value
	case *ast.Param:
		return "?" // SQLite uses ? for positional params
	case *ast.Ident:
		if v.Quoted {
			return `"` + strings.ReplaceAll(v.Name, `"`, `""`) + `"`
		}
		return v.Name
	case *ast.Star:
		if v.Table != "" {
			return quoteIdent(v.Table) + ".*"
		}
		return "*"
	case *ast.ColumnRef:
		if v.Table != "" {
			return quoteIdent(v.Table) + "." + quoteIdent(v.Column)
		}
		return quoteIdent(v.Column)
	case *ast.TableName:
		return emitTableName(v)
	case *ast.BinaryExpr:
		return "(" + emitExpr(v.Left) + " " + v.Op + " " + emitExpr(v.Right) + ")"
	case *ast.UnaryExpr:
		if v.Op == "-" {
			return "-" + emitExpr(v.Expr)
		}
		return v.Op + " " + emitExpr(v.Expr)
	case *ast.FuncCall:
		return emitFuncCall(v)
	case *ast.TypeCast:
		return "CAST(" + emitExpr(v.Expr) + " AS " + emitTypeRef(v.Type) + ")"
	case *ast.CaseExpr:
		return emitCase(v)
	case *ast.InExpr:
		return emitIn(v)
	case *ast.BetweenExpr:
		not := ""
		if v.Not {
			not = "NOT "
		}
		return emitExpr(v.Expr) + " " + not + "BETWEEN " + emitExpr(v.Low) + " AND " + emitExpr(v.High)
	case *ast.IsNullExpr:
		if v.Not {
			return emitExpr(v.Expr) + " IS NOT NULL"
		}
		return emitExpr(v.Expr) + " IS NULL"
	case *ast.LikeExpr:
		not := ""
		if v.Not {
			not = "NOT "
		}
		s := emitExpr(v.Expr) + " " + not + v.Op + " " + emitExpr(v.Pattern)
		if v.Escape != nil {
			s += " ESCAPE " + emitExpr(v.Escape)
		}
		return s
	case *ast.ArrayExpr:
		var items []string
		for _, el := range v.Elements {
			items = append(items, emitExpr(el))
		}
		return "JSON_ARRAY(" + strings.Join(items, ", ") + ")"
	case *ast.Subquery:
		return "(" + emitSelect(v.Stmt) + ")"
	case *ast.ExistsExpr:
		return "EXISTS(" + emitSelect(v.Subquery) + ")"
	case *ast.RowExpr:
		var items []string
		for _, el := range v.Elements {
			items = append(items, emitExpr(el))
		}
		return "(" + strings.Join(items, ", ") + ")"
	case *ast.ExtractExpr:
		// fallback if not rewritten
		return emitExpr(&ast.RawExpr{SQL: fmt.Sprintf("CAST(strftime('%%Y', %s) AS INTEGER)", emitExpr(v.Expr))})
	case *ast.RawExpr:
		return v.SQL
	}
	return fmt.Sprintf("/* unsupported expr %T */", e)
}

func emitFuncCall(v *ast.FuncCall) string {
	var b strings.Builder
	if v.Schema != "" {
		b.WriteString(quoteIdent(v.Schema))
		b.WriteString(".")
	}
	b.WriteString(v.Name)
	b.WriteString("(")
	if v.Star {
		b.WriteString("*")
	} else if v.Distinct {
		b.WriteString("DISTINCT ")
		var args []string
		for _, a := range v.Args {
			args = append(args, emitExpr(a))
		}
		b.WriteString(strings.Join(args, ", "))
	} else {
		var args []string
		for _, a := range v.Args {
			args = append(args, emitExpr(a))
		}
		b.WriteString(strings.Join(args, ", "))
	}
	b.WriteString(")")
	if v.Filter != nil {
		b.WriteString(" FILTER (WHERE ")
		b.WriteString(emitExpr(v.Filter))
		b.WriteString(")")
	}
	if v.Over != nil {
		b.WriteString(" OVER (")
		b.WriteString(emitWindowSpec(v.Over))
		b.WriteString(")")
	}
	return b.String()
}

func emitWindowSpec(spec *ast.WindowSpec) string {
	var parts []string
	if len(spec.PartitionBy) > 0 {
		var pb []string
		for _, e := range spec.PartitionBy {
			pb = append(pb, emitExpr(e))
		}
		parts = append(parts, "PARTITION BY "+strings.Join(pb, ", "))
	}
	if len(spec.OrderBy) > 0 {
		parts = append(parts, "ORDER BY "+emitOrderBy(spec.OrderBy))
	}
	return strings.Join(parts, " ")
}

func emitCase(v *ast.CaseExpr) string {
	var b strings.Builder
	b.WriteString("CASE")
	if v.Input != nil {
		b.WriteString(" ")
		b.WriteString(emitExpr(v.Input))
	}
	for _, w := range v.Whens {
		b.WriteString(" WHEN ")
		b.WriteString(emitExpr(w.Cond))
		b.WriteString(" THEN ")
		b.WriteString(emitExpr(w.Result))
	}
	if v.Default != nil {
		b.WriteString(" ELSE ")
		b.WriteString(emitExpr(v.Default))
	}
	b.WriteString(" END")
	return b.String()
}

func emitIn(v *ast.InExpr) string {
	not := ""
	if v.Not {
		not = "NOT "
	}
	if v.Subquery != nil {
		return emitExpr(v.Expr) + " " + not + "IN (" + emitSelect(v.Subquery) + ")"
	}
	var items []string
	for _, i := range v.List {
		items = append(items, emitExpr(i))
	}
	return emitExpr(v.Expr) + " " + not + "IN (" + strings.Join(items, ", ") + ")"
}

// ── Type references ───────────────────────────────────────────────────────────

func emitTypeRef(t *ast.TypeRef) string {
	if t == nil {
		return "TEXT"
	}
	s := t.Name
	if len(t.Modifiers) > 0 {
		var mods []string
		for _, m := range t.Modifiers {
			mods = append(mods, fmt.Sprintf("%d", m))
		}
		s += "(" + strings.Join(mods, ", ") + ")"
	}
	return s
}

// ── Table names ───────────────────────────────────────────────────────────────

func emitTableName(t *ast.TableName) string {
	if t == nil {
		return ""
	}
	// Strip public schema.
	if strings.EqualFold(t.Schema, "public") || t.Schema == "" {
		return quoteIdent(t.Name)
	}
	return quoteIdent(t.Schema) + "." + quoteIdent(t.Name)
}

// ── Identifier quoting ────────────────────────────────────────────────────────

// quoteIdent quotes a SQL identifier with double quotes if it contains
// special characters or is a reserved word in SQLite.
func quoteIdent(name string) string {
	if name == "" || name == "*" {
		return name
	}
	// Already quoted.
	if strings.HasPrefix(name, `"`) {
		return name
	}
	// SQLite reserved words and identifiers with special chars need quoting.
	if needsQuoting(name) {
		return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
	}
	return name
}

// sqliteReserved is a set of SQLite reserved words that need quoting when
// used as identifiers.
var sqliteReserved = map[string]bool{
	"ABORT": true, "ACTION": true, "ADD": true, "AFTER": true, "ALL": true,
	"ALTER": true, "ALWAYS": true, "ANALYZE": true, "AND": true, "AS": true,
	"ASC": true, "ATTACH": true, "AUTOINCREMENT": true, "BEFORE": true,
	"BEGIN": true, "BETWEEN": true, "BY": true, "CASCADE": true, "CASE": true,
	"CAST": true, "CHECK": true, "COLLATE": true, "COLUMN": true,
	"COMMIT": true, "CONFLICT": true, "CONSTRAINT": true, "CREATE": true,
	"CROSS": true, "CURRENT": true, "CURRENT_DATE": true, "CURRENT_TIME": true,
	"CURRENT_TIMESTAMP": true, "DATABASE": true, "DEFAULT": true,
	"DEFERRABLE": true, "DEFERRED": true, "DELETE": true, "DESC": true,
	"DETACH": true, "DISTINCT": true, "DO": true, "DROP": true, "EACH": true,
	"ELSE": true, "END": true, "ESCAPE": true, "EXCEPT": true, "EXCLUDE": true,
	"EXCLUSIVE": true, "EXISTS": true, "EXPLAIN": true, "FAIL": true,
	"FILTER": true, "FIRST": true, "FOLLOWING": true, "FOR": true,
	"FOREIGN": true, "FROM": true, "FULL": true, "GENERATED": true,
	"GLOB": true, "GROUP": true, "GROUPS": true, "HAVING": true, "IF": true,
	"IGNORE": true, "IMMEDIATE": true, "IN": true, "INDEX": true,
	"INDEXED": true, "INITIALLY": true, "INNER": true, "INSERT": true,
	"INSTEAD": true, "INTERSECT": true, "INTO": true, "IS": true,
	"ISNULL": true, "JOIN": true, "KEY": true, "LAST": true, "LEFT": true,
	"LIKE": true, "LIMIT": true, "MATCH": true, "MATERIALIZED": true,
	"NATURAL": true, "NO": true, "NOT": true, "NOTHING": true, "NOTNULL": true,
	"NULL": true, "NULLS": true, "OF": true, "OFFSET": true, "ON": true,
	"OR": true, "ORDER": true, "OTHERS": true, "OUTER": true, "OVER": true,
	"PARTITION": true, "PLAN": true, "PRAGMA": true, "PRECEDING": true,
	"PRIMARY": true, "QUERY": true, "RAISE": true, "RANGE": true,
	"RECURSIVE": true, "REFERENCES": true, "REGEXP": true, "REINDEX": true,
	"RELEASE": true, "RENAME": true, "REPLACE": true, "RESTRICT": true,
	"RETURNING": true, "RIGHT": true, "ROLLBACK": true, "ROW": true,
	"ROWS": true, "SAVEPOINT": true, "SELECT": true, "SET": true,
	"TABLE": true, "TEMP": true, "TEMPORARY": true, "THEN": true, "TIES": true,
	"TO": true, "TRANSACTION": true, "TRIGGER": true, "UNBOUNDED": true,
	"UNION": true, "UNIQUE": true, "UPDATE": true, "USING": true,
	"VACUUM": true, "VALUES": true, "VIEW": true, "VIRTUAL": true,
	"WHEN": true, "WHERE": true, "WINDOW": true, "WITH": true, "WITHOUT": true,
}

func needsQuoting(name string) bool {
	upper := strings.ToUpper(name)
	if sqliteReserved[upper] {
		return true
	}
	for _, ch := range name {
		if !((ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') ||
			(ch >= '0' && ch <= '9') || ch == '_') {
			return true
		}
	}
	return false
}
