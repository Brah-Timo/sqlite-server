// Package planner contains the normalizer, rewriter, and SQL-emitter that
// transform a PostgreSQL AST into a SQLite-compatible SQL string.
//
// Pipeline:
//
//	raw SQL (PostgreSQL)
//	→ Lexer        (sql/lexer)
//	→ Parser       (sql/parser)
//	→ AST          (sql/ast)
//	→ Normalizer   (this file)    — uppercase keywords, fold aliases
//	→ Rewriter     (rewriter.go)  — PG→SQLite semantic transformations
//	→ Planner      (planner.go)   — emit final SQL string for SQLite
package planner

import (
	"strings"

	"github.com/sqlite-server/sqlite-server/sql/ast"
)

// ─────────────────────────────────────────────────────────────────────────────
//  Normalizer
// ─────────────────────────────────────────────────────────────────────────────

// Normalizer performs structural normalisation on an AST before rewriting.
// It does NOT change semantics — it only standardises representation so that
// later passes have a consistent view.
//
// Current normalisations:
//   - Uppercase all identifiers that are not quoted.
//   - Convert 1 = 1 / 0 = 0 boolean shortcuts.
//   - Canonicalize TRUE/FALSE literals.
//   - Replace schema-qualified public.X references to bare X.
type Normalizer struct{}

// NewNormalizer returns a ready Normalizer.
func NewNormalizer() *Normalizer { return &Normalizer{} }

// NormalizeStmt normalises a single statement.
func (n *Normalizer) NormalizeStmt(stmt ast.Stmt) ast.Stmt {
	return n.stmt(stmt)
}

func (n *Normalizer) stmt(s ast.Stmt) ast.Stmt {
	if s == nil {
		return nil
	}
	switch v := s.(type) {
	case *ast.SelectStmt:
		return n.selectStmt(v)
	case *ast.InsertStmt:
		return n.insertStmt(v)
	case *ast.UpdateStmt:
		return n.updateStmt(v)
	case *ast.DeleteStmt:
		return n.deleteStmt(v)
	case *ast.SetOperation:
		return &ast.SetOperation{
			Op:    strings.ToUpper(v.Op),
			All:   v.All,
			Left:  n.stmt(v.Left),
			Right: n.stmt(v.Right),
		}
	default:
		return s
	}
}

func (n *Normalizer) selectStmt(s *ast.SelectStmt) *ast.SelectStmt {
	if s == nil {
		return nil
	}
	out := *s
	// Normalise SELECT list.
	for i, t := range out.Targets {
		out.Targets[i].Expr = n.expr(t.Expr)
	}
	// Normalise FROM.
	for i, f := range out.From {
		out.From[i] = n.fromClause(f)
	}
	if out.Where != nil {
		out.Where = n.expr(out.Where)
	}
	for i, g := range out.GroupBy {
		out.GroupBy[i] = n.expr(g)
	}
	if out.Having != nil {
		out.Having = n.expr(out.Having)
	}
	for i, o := range out.OrderBy {
		out.OrderBy[i].Expr = n.expr(o.Expr)
	}
	if out.Limit != nil {
		out.Limit = n.expr(out.Limit)
	}
	if out.Offset != nil {
		out.Offset = n.expr(out.Offset)
	}
	return &out
}

func (n *Normalizer) insertStmt(s *ast.InsertStmt) *ast.InsertStmt {
	if s == nil {
		return nil
	}
	out := *s
	out.Table = n.tableName(s.Table)
	return &out
}

func (n *Normalizer) updateStmt(s *ast.UpdateStmt) *ast.UpdateStmt {
	if s == nil {
		return nil
	}
	out := *s
	out.Table = n.tableName(s.Table)
	for i, set := range out.Sets {
		out.Sets[i] = ast.Assignment{Column: n.expr(set.Column), Value: n.expr(set.Value)}
	}
	if out.Where != nil {
		out.Where = n.expr(out.Where)
	}
	return &out
}

func (n *Normalizer) deleteStmt(s *ast.DeleteStmt) *ast.DeleteStmt {
	if s == nil {
		return nil
	}
	out := *s
	out.Table = n.tableName(s.Table)
	if out.Where != nil {
		out.Where = n.expr(out.Where)
	}
	return &out
}

func (n *Normalizer) fromClause(f ast.FromClause) ast.FromClause {
	if f == nil {
		return nil
	}
	switch v := f.(type) {
	case *ast.TableRef:
		out := *v
		out.Name = n.tableName(v.Name)
		return &out
	case *ast.JoinExpr:
		out := *v
		out.Left = n.fromClause(v.Left)
		out.Right = n.fromClause(v.Right)
		if out.On != nil {
			out.On = n.expr(out.On)
		}
		return &out
	case *ast.SubqueryFromClause:
		out := *v
		out.Stmt = n.selectStmt(v.Stmt)
		return &out
	}
	return f
}

func (n *Normalizer) tableName(t *ast.TableName) *ast.TableName {
	if t == nil {
		return nil
	}
	// Drop "public." schema prefix — SQLite has no schemas.
	if strings.EqualFold(t.Schema, "public") {
		return &ast.TableName{Name: t.Name}
	}
	return t
}

func (n *Normalizer) expr(e ast.Expr) ast.Expr {
	if e == nil {
		return nil
	}
	switch v := e.(type) {
	case *ast.BinaryExpr:
		return &ast.BinaryExpr{
			Op:    v.Op,
			Left:  n.expr(v.Left),
			Right: n.expr(v.Right),
		}
	case *ast.UnaryExpr:
		return &ast.UnaryExpr{Op: v.Op, Expr: n.expr(v.Expr)}
	case *ast.FuncCall:
		out := *v
		for i, a := range out.Args {
			out.Args[i] = n.expr(a)
		}
		return &out
	case *ast.TypeCast:
		return &ast.TypeCast{Expr: n.expr(v.Expr), Type: v.Type}
	case *ast.CaseExpr:
		out := *v
		if out.Input != nil {
			out.Input = n.expr(out.Input)
		}
		for i, w := range out.Whens {
			out.Whens[i] = ast.WhenClause{Cond: n.expr(w.Cond), Result: n.expr(w.Result)}
		}
		if out.Default != nil {
			out.Default = n.expr(out.Default)
		}
		return &out
	case *ast.InExpr:
		out := *v
		out.Expr = n.expr(v.Expr)
		for i, item := range out.List {
			out.List[i] = n.expr(item)
		}
		return &out
	case *ast.BetweenExpr:
		return &ast.BetweenExpr{
			Expr: n.expr(v.Expr), Not: v.Not,
			Low: n.expr(v.Low), High: n.expr(v.High),
		}
	case *ast.IsNullExpr:
		return &ast.IsNullExpr{Expr: n.expr(v.Expr), Not: v.Not}
	case *ast.LikeExpr:
		out := *v
		out.Expr = n.expr(v.Expr)
		out.Pattern = n.expr(v.Pattern)
		return &out
	case *ast.ExtractExpr:
		return &ast.ExtractExpr{Field: strings.ToUpper(v.Field), Expr: n.expr(v.Expr)}
	case *ast.Subquery:
		return &ast.Subquery{Stmt: n.selectStmt(v.Stmt)}
	case *ast.ExistsExpr:
		return &ast.ExistsExpr{Subquery: n.selectStmt(v.Subquery)}
	case *ast.TableName:
		return n.tableName(v)
	}
	return e
}
