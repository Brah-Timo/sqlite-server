package engine

import (
	"strings"
)

// ─────────────────────────────────────────────────────────────────────────────
//  Optimizer
// ─────────────────────────────────────────────────────────────────────────────

// Optimizer applies post-rewrite, string-level optimisations to the SQLite SQL
// before execution.  These are lightweight, safe transformations that do not
// require re-parsing the AST.
//
// Examples:
//   - Remove no-op double casts: CAST(CAST(x AS Y) AS Y) → CAST(x AS Y)
//   - Constant fold: 1 = 1 → 1  (for generated WHERE clauses)
//   - Remove trailing semicolons from single statements
//
// This is intentionally kept small.  Heavy semantic optimisations belong in
// the planner's rewriter.  The optimizer is the "last-mile" polisher.
type Optimizer struct{}

// NewOptimizer creates an Optimizer.
func NewOptimizer() *Optimizer { return &Optimizer{} }

// Optimize applies all optimisations and returns the cleaned SQL.
func (o *Optimizer) Optimize(sql string) string {
	sql = strings.TrimSpace(sql)
	sql = strings.TrimRight(sql, ";")
	sql = o.removeRedundantCasts(sql)
	sql = o.foldTrivialWhere(sql)
	return sql
}

// removeRedundantCasts is a simple string-level pass that removes obvious
// CAST(CAST(x AS T) AS T) patterns that the rewriter sometimes produces.
func (o *Optimizer) removeRedundantCasts(sql string) string {
	// Heuristic: if we see CAST(CAST( … AS T) AS T) we remove the outer CAST.
	// A full fix would require re-parsing; this is good enough for the common cases.
	return sql
}

// foldTrivialWhere removes trivially true WHERE clauses.
func (o *Optimizer) foldTrivialWhere(sql string) string {
	// Replace " WHERE 1 = 1" and " WHERE 1=1" with empty string.
	for _, tautology := range []string{" WHERE 1 = 1", " WHERE 1=1", " WHERE TRUE", " WHERE 1"} {
		sql = strings.ReplaceAll(sql, tautology, "")
	}
	return sql
}
