// Package ast defines the Abstract Syntax Tree (AST) nodes for the
// PostgreSQL SQL dialect.  Every node type satisfies the Node interface.
// The parser builds these nodes; the rewriter transforms them; the planner
// converts them to SQLite SQL strings.
package ast

import (
	"fmt"
	"strings"
)

// ─────────────────────────────────────────────────────────────────────────────
//  Node interface
// ─────────────────────────────────────────────────────────────────────────────

// Node is the root interface for all AST nodes.
type Node interface {
	nodeTag()       // marker method (prevents accidental implementation)
	String() string // debug representation
}

// Stmt is a top-level statement node.
type Stmt interface {
	Node
	stmtTag()
}

// Expr is an expression node.
type Expr interface {
	Node
	exprTag()
}

// ─────────────────────────────────────────────────────────────────────────────
//  Statements
// ─────────────────────────────────────────────────────────────────────────────

// SelectStmt represents SELECT … FROM … WHERE … GROUP BY … HAVING …
// ORDER BY … LIMIT … OFFSET … FOR …
type SelectStmt struct {
	Distinct     bool
	DistinctOn   []Expr
	Targets      []SelectTarget // SELECT list
	From         []FromClause
	Where        Expr
	GroupBy      []Expr
	Having       Expr
	OrderBy      []SortKey
	Limit        Expr
	Offset       Expr
	LockClause   *LockClause
	With         *WithClause
	SetOperation *SetOperation // UNION / INTERSECT / EXCEPT
	Window       []WindowDef
}

func (s *SelectStmt) nodeTag() {}
func (s *SelectStmt) stmtTag() {}
func (s *SelectStmt) String() string {
	var parts []string
	parts = append(parts, "SELECT")
	if s.Distinct {
		parts = append(parts, "DISTINCT")
	}
	var targets []string
	for _, t := range s.Targets {
		targets = append(targets, t.String())
	}
	parts = append(parts, strings.Join(targets, ", "))
	if len(s.From) > 0 {
		var froms []string
		for _, f := range s.From {
			froms = append(froms, f.String())
		}
		parts = append(parts, "FROM", strings.Join(froms, ", "))
	}
	if s.Where != nil {
		parts = append(parts, "WHERE", s.Where.String())
	}
	if len(s.GroupBy) > 0 {
		var gb []string
		for _, g := range s.GroupBy {
			gb = append(gb, g.String())
		}
		parts = append(parts, "GROUP BY", strings.Join(gb, ", "))
	}
	if s.Having != nil {
		parts = append(parts, "HAVING", s.Having.String())
	}
	if len(s.OrderBy) > 0 {
		var ob []string
		for _, o := range s.OrderBy {
			ob = append(ob, o.String())
		}
		parts = append(parts, "ORDER BY", strings.Join(ob, ", "))
	}
	if s.Limit != nil {
		parts = append(parts, "LIMIT", s.Limit.String())
	}
	if s.Offset != nil {
		parts = append(parts, "OFFSET", s.Offset.String())
	}
	return strings.Join(parts, " ")
}

// InsertStmt represents INSERT INTO … (cols) VALUES … / SELECT … RETURNING …
type InsertStmt struct {
	With       *WithClause
	Table      *TableName
	Columns    []*Ident
	Source     InsertSource
	OnConflict *OnConflict
	Returning  []SelectTarget
}

func (s *InsertStmt) nodeTag() {}
func (s *InsertStmt) stmtTag() {}
func (s *InsertStmt) String() string {
	var b strings.Builder
	b.WriteString("INSERT INTO ")
	b.WriteString(s.Table.String())
	if len(s.Columns) > 0 {
		var cols []string
		for _, c := range s.Columns {
			cols = append(cols, c.String())
		}
		b.WriteString(" (")
		b.WriteString(strings.Join(cols, ", "))
		b.WriteString(")")
	}
	b.WriteString(" ")
	b.WriteString(s.Source.String())
	if s.OnConflict != nil {
		b.WriteString(" ")
		b.WriteString(s.OnConflict.String())
	}
	if len(s.Returning) > 0 {
		b.WriteString(" RETURNING ")
		var ret []string
		for _, r := range s.Returning {
			ret = append(ret, r.String())
		}
		b.WriteString(strings.Join(ret, ", "))
	}
	return b.String()
}

// UpdateStmt represents UPDATE … SET … WHERE … RETURNING …
type UpdateStmt struct {
	With      *WithClause
	Table     *TableName
	Alias     *Ident
	Sets      []Assignment
	From      []FromClause
	Where     Expr
	Returning []SelectTarget
}

func (s *UpdateStmt) nodeTag() {}
func (s *UpdateStmt) stmtTag() {}
func (s *UpdateStmt) String() string {
	var b strings.Builder
	b.WriteString("UPDATE ")
	b.WriteString(s.Table.String())
	b.WriteString(" SET ")
	var sets []string
	for _, a := range s.Sets {
		sets = append(sets, a.String())
	}
	b.WriteString(strings.Join(sets, ", "))
	if s.Where != nil {
		b.WriteString(" WHERE ")
		b.WriteString(s.Where.String())
	}
	if len(s.Returning) > 0 {
		var ret []string
		for _, r := range s.Returning {
			ret = append(ret, r.String())
		}
		b.WriteString(" RETURNING ")
		b.WriteString(strings.Join(ret, ", "))
	}
	return b.String()
}

// DeleteStmt represents DELETE FROM … WHERE … RETURNING …
type DeleteStmt struct {
	With      *WithClause
	Table     *TableName
	Alias     *Ident
	Using     []FromClause
	Where     Expr
	Returning []SelectTarget
}

func (s *DeleteStmt) nodeTag() {}
func (s *DeleteStmt) stmtTag() {}
func (s *DeleteStmt) String() string {
	var b strings.Builder
	b.WriteString("DELETE FROM ")
	b.WriteString(s.Table.String())
	if s.Where != nil {
		b.WriteString(" WHERE ")
		b.WriteString(s.Where.String())
	}
	if len(s.Returning) > 0 {
		var ret []string
		for _, r := range s.Returning {
			ret = append(ret, r.String())
		}
		b.WriteString(" RETURNING ")
		b.WriteString(strings.Join(ret, ", "))
	}
	return b.String()
}

// CreateTableStmt represents CREATE [TEMP] TABLE … (…)
type CreateTableStmt struct {
	IfNotExists bool
	Temp        bool
	Name        *TableName
	Columns     []*ColumnDef
	Constraints []*TableConstraint
	Like        *TableName
}

func (s *CreateTableStmt) nodeTag() {}
func (s *CreateTableStmt) stmtTag() {}
func (s *CreateTableStmt) String() string {
	var b strings.Builder
	b.WriteString("CREATE ")
	if s.Temp {
		b.WriteString("TEMP ")
	}
	b.WriteString("TABLE ")
	if s.IfNotExists {
		b.WriteString("IF NOT EXISTS ")
	}
	b.WriteString(s.Name.String())
	b.WriteString(" (")
	var defs []string
	for _, c := range s.Columns {
		defs = append(defs, c.String())
	}
	for _, c := range s.Constraints {
		defs = append(defs, c.String())
	}
	b.WriteString(strings.Join(defs, ", "))
	b.WriteString(")")
	return b.String()
}

// DropTableStmt represents DROP TABLE …
type DropTableStmt struct {
	IfExists bool
	Tables   []*TableName
	Cascade  bool
}

func (s *DropTableStmt) nodeTag() {}
func (s *DropTableStmt) stmtTag() {}
func (s *DropTableStmt) String() string {
	var b strings.Builder
	b.WriteString("DROP TABLE ")
	if s.IfExists {
		b.WriteString("IF EXISTS ")
	}
	var tables []string
	for _, t := range s.Tables {
		tables = append(tables, t.String())
	}
	b.WriteString(strings.Join(tables, ", "))
	if s.Cascade {
		b.WriteString(" CASCADE")
	}
	return b.String()
}

// AlterTableStmt represents ALTER TABLE …
type AlterTableStmt struct {
	Name   *TableName
	Action AlterTableAction
}

func (s *AlterTableStmt) nodeTag() {}
func (s *AlterTableStmt) stmtTag() {}
func (s *AlterTableStmt) String() string {
	return fmt.Sprintf("ALTER TABLE %s %s", s.Name, s.Action)
}

// CreateIndexStmt represents CREATE [UNIQUE] INDEX …
type CreateIndexStmt struct {
	Unique       bool
	Concurrently bool
	IfNotExists  bool
	Name         *Ident
	Table        *TableName
	Columns      []IndexElement
	Where        Expr
}

func (s *CreateIndexStmt) nodeTag() {}
func (s *CreateIndexStmt) stmtTag() {}
func (s *CreateIndexStmt) String() string {
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
		b.WriteString(s.Name.String())
		b.WriteString(" ")
	}
	b.WriteString("ON ")
	b.WriteString(s.Table.String())
	var cols []string
	for _, c := range s.Columns {
		cols = append(cols, c.Expr.String())
	}
	b.WriteString(" (")
	b.WriteString(strings.Join(cols, ", "))
	b.WriteString(")")
	if s.Where != nil {
		b.WriteString(" WHERE ")
		b.WriteString(s.Where.String())
	}
	return b.String()
}

// BeginStmt represents BEGIN [TRANSACTION]
type BeginStmt struct{ IsolationLevel string }

func (s *BeginStmt) nodeTag()       {}
func (s *BeginStmt) stmtTag()       {}
func (s *BeginStmt) String() string { return "BEGIN" }

// CommitStmt represents COMMIT [TRANSACTION]
type CommitStmt struct{}

func (s *CommitStmt) nodeTag()       {}
func (s *CommitStmt) stmtTag()       {}
func (s *CommitStmt) String() string { return "COMMIT" }

// RollbackStmt represents ROLLBACK [TRANSACTION]
type RollbackStmt struct{ Savepoint string }

func (s *RollbackStmt) nodeTag()       {}
func (s *RollbackStmt) stmtTag()       {}
func (s *RollbackStmt) String() string { return "ROLLBACK" }

// SavepointStmt represents SAVEPOINT name
type SavepointStmt struct{ Name string }

func (s *SavepointStmt) nodeTag()       {}
func (s *SavepointStmt) stmtTag()       {}
func (s *SavepointStmt) String() string { return "SAVEPOINT " + s.Name }

// ReleaseSavepointStmt represents RELEASE SAVEPOINT name
type ReleaseSavepointStmt struct{ Name string }

func (s *ReleaseSavepointStmt) nodeTag()       {}
func (s *ReleaseSavepointStmt) stmtTag()       {}
func (s *ReleaseSavepointStmt) String() string { return "RELEASE SAVEPOINT " + s.Name }

// SetStmt represents SET parameter = value
type SetStmt struct {
	Name  string
	Value string
}

func (s *SetStmt) nodeTag()       {}
func (s *SetStmt) stmtTag()       {}
func (s *SetStmt) String() string { return fmt.Sprintf("SET %s = %s", s.Name, s.Value) }

// ShowStmt represents SHOW parameter
type ShowStmt struct{ Name string }

func (s *ShowStmt) nodeTag()       {}
func (s *ShowStmt) stmtTag()       {}
func (s *ShowStmt) String() string { return "SHOW " + s.Name }

// ExplainStmt represents EXPLAIN [ANALYZE] stmt
type ExplainStmt struct {
	Analyze bool
	Verbose bool
	Inner   Stmt
}

func (s *ExplainStmt) nodeTag()       {}
func (s *ExplainStmt) stmtTag()       {}
func (s *ExplainStmt) String() string { return "EXPLAIN " + s.Inner.String() }

// RawStmt wraps a statement we could not fully parse.
// The rewriter will pass it through as-is to SQLite.
type RawStmt struct{ SQL string }

func (s *RawStmt) nodeTag()       {}
func (s *RawStmt) stmtTag()       {}
func (s *RawStmt) String() string { return s.SQL }

// ─────────────────────────────────────────────────────────────────────────────
//  Expressions
// ─────────────────────────────────────────────────────────────────────────────

// Ident is a simple or quoted identifier.
type Ident struct {
	Name   string
	Quoted bool
}

func (e *Ident) nodeTag() {}
func (e *Ident) exprTag() {}
func (e *Ident) String() string {
	if e.Quoted {
		return `"` + strings.ReplaceAll(e.Name, `"`, `""`) + `"`
	}
	return e.Name
}

// TableName is a [schema.]table name.
type TableName struct {
	Schema string
	Name   string
}

func (t *TableName) nodeTag() {}
func (t *TableName) exprTag() {}
func (t *TableName) String() string {
	if t.Schema != "" {
		return t.Schema + "." + t.Name
	}
	return t.Name
}

// ColumnRef is a [table.]column reference.
type ColumnRef struct {
	Table  string
	Column string
}

func (e *ColumnRef) nodeTag() {}
func (e *ColumnRef) exprTag() {}
func (e *ColumnRef) String() string {
	if e.Table != "" {
		return e.Table + "." + e.Column
	}
	return e.Column
}

// Star is * in SELECT or table.*.
type Star struct{ Table string }

func (e *Star) nodeTag() {}
func (e *Star) exprTag() {}
func (e *Star) String() string {
	if e.Table != "" {
		return e.Table + ".*"
	}
	return "*"
}

// Literal is a scalar literal value.
type Literal struct {
	Kind  LiteralKind
	Value string
}

type LiteralKind int

const (
	LitInteger LiteralKind = iota
	LitFloat
	LitString
	LitTrue
	LitFalse
	LitNull
)

func (e *Literal) nodeTag()       {}
func (e *Literal) exprTag()       {}
func (e *Literal) String() string { return e.Value }

// Param is a query parameter $n.
type Param struct{ Index int }

func (e *Param) nodeTag()       {}
func (e *Param) exprTag()       {}
func (e *Param) String() string { return fmt.Sprintf("$%d", e.Index) }

// BinaryExpr is left op right.
type BinaryExpr struct {
	Op    string
	Left  Expr
	Right Expr
}

func (e *BinaryExpr) nodeTag() {}
func (e *BinaryExpr) exprTag() {}
func (e *BinaryExpr) String() string {
	return fmt.Sprintf("(%s %s %s)", e.Left, e.Op, e.Right)
}

// UnaryExpr is op expr.
type UnaryExpr struct {
	Op   string
	Expr Expr
}

func (e *UnaryExpr) nodeTag()       {}
func (e *UnaryExpr) exprTag()       {}
func (e *UnaryExpr) String() string { return fmt.Sprintf("(%s %s)", e.Op, e.Expr) }

// FuncCall is function(args) [FILTER (…)] [OVER (…)]
type FuncCall struct {
	Schema   string
	Name     string
	Args     []Expr
	Distinct bool
	Star     bool // COUNT(*)
	Filter   Expr
	Over     *WindowSpec
	OrderBy  []SortKey // for ordered-set aggregates
}

func (e *FuncCall) nodeTag() {}
func (e *FuncCall) exprTag() {}
func (e *FuncCall) String() string {
	var b strings.Builder
	if e.Schema != "" {
		b.WriteString(e.Schema)
		b.WriteString(".")
	}
	b.WriteString(e.Name)
	b.WriteString("(")
	if e.Star {
		b.WriteString("*")
	} else if e.Distinct {
		b.WriteString("DISTINCT ")
		var args []string
		for _, a := range e.Args {
			args = append(args, a.String())
		}
		b.WriteString(strings.Join(args, ", "))
	} else {
		var args []string
		for _, a := range e.Args {
			args = append(args, a.String())
		}
		b.WriteString(strings.Join(args, ", "))
	}
	b.WriteString(")")
	return b.String()
}

// TypeCast is expr::type or CAST(expr AS type).
type TypeCast struct {
	Expr Expr
	Type *TypeRef
}

func (e *TypeCast) nodeTag() {}
func (e *TypeCast) exprTag() {}
func (e *TypeCast) String() string {
	return fmt.Sprintf("CAST(%s AS %s)", e.Expr, e.Type)
}

// TypeRef is a data type reference.
type TypeRef struct {
	Schema    string
	Name      string
	Modifiers []int // e.g. VARCHAR(255)  →  [255]
	IsArray   bool
}

func (t *TypeRef) nodeTag() {}
func (t *TypeRef) exprTag() {}
func (t *TypeRef) String() string {
	var b strings.Builder
	if t.Schema != "" {
		b.WriteString(t.Schema)
		b.WriteString(".")
	}
	b.WriteString(t.Name)
	if len(t.Modifiers) > 0 {
		var mods []string
		for _, m := range t.Modifiers {
			mods = append(mods, fmt.Sprintf("%d", m))
		}
		b.WriteString("(")
		b.WriteString(strings.Join(mods, ","))
		b.WriteString(")")
	}
	if t.IsArray {
		b.WriteString("[]")
	}
	return b.String()
}

// CaseExpr is CASE [expr] WHEN … THEN … [ELSE …] END.
type CaseExpr struct {
	Input   Expr // nil for searched CASE
	Whens   []WhenClause
	Default Expr
}

func (e *CaseExpr) nodeTag() {}
func (e *CaseExpr) exprTag() {}
func (e *CaseExpr) String() string {
	var b strings.Builder
	b.WriteString("CASE")
	if e.Input != nil {
		b.WriteString(" ")
		b.WriteString(e.Input.String())
	}
	for _, w := range e.Whens {
		b.WriteString(fmt.Sprintf(" WHEN %s THEN %s", w.Cond, w.Result))
	}
	if e.Default != nil {
		b.WriteString(" ELSE ")
		b.WriteString(e.Default.String())
	}
	b.WriteString(" END")
	return b.String()
}

// WhenClause is a WHEN … THEN … pair.
type WhenClause struct {
	Cond   Expr
	Result Expr
}

// InExpr is expr [NOT] IN (list / subquery).
type InExpr struct {
	Expr     Expr
	Not      bool
	List     []Expr
	Subquery *SelectStmt
}

func (e *InExpr) nodeTag() {}
func (e *InExpr) exprTag() {}
func (e *InExpr) String() string {
	not := ""
	if e.Not {
		not = "NOT "
	}
	if e.Subquery != nil {
		return fmt.Sprintf("(%s %sIN (%s))", e.Expr, not, e.Subquery)
	}
	var items []string
	for _, i := range e.List {
		items = append(items, i.String())
	}
	return fmt.Sprintf("(%s %sIN (%s))", e.Expr, not, strings.Join(items, ", "))
}

// BetweenExpr is expr [NOT] BETWEEN low AND high.
type BetweenExpr struct {
	Expr Expr
	Not  bool
	Low  Expr
	High Expr
}

func (e *BetweenExpr) nodeTag() {}
func (e *BetweenExpr) exprTag() {}
func (e *BetweenExpr) String() string {
	not := ""
	if e.Not {
		not = "NOT "
	}
	return fmt.Sprintf("(%s %sBETWEEN %s AND %s)", e.Expr, not, e.Low, e.High)
}

// IsNullExpr is expr IS [NOT] NULL.
type IsNullExpr struct {
	Expr Expr
	Not  bool
}

func (e *IsNullExpr) nodeTag() {}
func (e *IsNullExpr) exprTag() {}
func (e *IsNullExpr) String() string {
	if e.Not {
		return fmt.Sprintf("(%s IS NOT NULL)", e.Expr)
	}
	return fmt.Sprintf("(%s IS NULL)", e.Expr)
}

// LikeExpr is expr [NOT] LIKE/ILIKE pattern [ESCAPE char].
type LikeExpr struct {
	Expr    Expr
	Not     bool
	Op      string // LIKE, ILIKE, SIMILAR TO
	Pattern Expr
	Escape  Expr
}

func (e *LikeExpr) nodeTag() {}
func (e *LikeExpr) exprTag() {}
func (e *LikeExpr) String() string {
	not := ""
	if e.Not {
		not = "NOT "
	}
	s := fmt.Sprintf("(%s %s%s %s)", e.Expr, not, e.Op, e.Pattern)
	if e.Escape != nil {
		s += " ESCAPE " + e.Escape.String()
	}
	return s
}

// ArrayExpr is ARRAY[…].
type ArrayExpr struct{ Elements []Expr }

func (e *ArrayExpr) nodeTag() {}
func (e *ArrayExpr) exprTag() {}
func (e *ArrayExpr) String() string {
	var items []string
	for _, el := range e.Elements {
		items = append(items, el.String())
	}
	return "ARRAY[" + strings.Join(items, ", ") + "]"
}

// Subquery is (SELECT …).
type Subquery struct {
	Stmt *SelectStmt
}

func (e *Subquery) nodeTag()       {}
func (e *Subquery) exprTag()       {}
func (e *Subquery) String() string { return "(" + e.Stmt.String() + ")" }

// ExistsExpr is EXISTS(subquery).
type ExistsExpr struct{ Subquery *SelectStmt }

func (e *ExistsExpr) nodeTag()       {}
func (e *ExistsExpr) exprTag()       {}
func (e *ExistsExpr) String() string { return "EXISTS(" + e.Subquery.String() + ")" }

// RowExpr is ROW(…) or (val, val, …).
type RowExpr struct{ Elements []Expr }

func (e *RowExpr) nodeTag() {}
func (e *RowExpr) exprTag() {}
func (e *RowExpr) String() string {
	var items []string
	for _, el := range e.Elements {
		items = append(items, el.String())
	}
	return "ROW(" + strings.Join(items, ", ") + ")"
}

// ExtractExpr is EXTRACT(field FROM expr).
type ExtractExpr struct {
	Field string
	Expr  Expr
}

func (e *ExtractExpr) nodeTag() {}
func (e *ExtractExpr) exprTag() {}
func (e *ExtractExpr) String() string {
	return fmt.Sprintf("EXTRACT(%s FROM %s)", e.Field, e.Expr)
}

// ─────────────────────────────────────────────────────────────────────────────
//  Clause sub-types
// ─────────────────────────────────────────────────────────────────────────────

// SelectTarget is one item in a SELECT list.
type SelectTarget struct {
	Expr  Expr
	Alias *Ident
}

func (t SelectTarget) String() string {
	if t.Alias != nil {
		return t.Expr.String() + " AS " + t.Alias.String()
	}
	return t.Expr.String()
}

// FromClause is one item in a FROM list.
type FromClause interface {
	Node
	fromTag()
}

// TableRef is a FROM clause table reference.
type TableRef struct {
	Name  *TableName
	Alias *Ident
}

func (t *TableRef) nodeTag() {}
func (t *TableRef) fromTag() {}
func (t *TableRef) String() string {
	if t.Alias != nil {
		return t.Name.String() + " AS " + t.Alias.String()
	}
	return t.Name.String()
}

// JoinExpr is a JOIN.
type JoinExpr struct {
	JoinType string // INNER, LEFT, RIGHT, FULL, CROSS
	Left     FromClause
	Right    FromClause
	On       Expr
	Using    []string
}

func (j *JoinExpr) nodeTag() {}
func (j *JoinExpr) fromTag() {}
func (j *JoinExpr) String() string {
	var b strings.Builder
	b.WriteString(j.Left.String())
	b.WriteString(" ")
	b.WriteString(j.JoinType)
	b.WriteString(" JOIN ")
	b.WriteString(j.Right.String())
	if j.On != nil {
		b.WriteString(" ON ")
		b.WriteString(j.On.String())
	}
	if len(j.Using) > 0 {
		b.WriteString(" USING (")
		b.WriteString(strings.Join(j.Using, ", "))
		b.WriteString(")")
	}
	return b.String()
}

// SubqueryFromClause is (SELECT …) AS alias in FROM.
type SubqueryFromClause struct {
	Stmt  *SelectStmt
	Alias *Ident
}

func (s *SubqueryFromClause) nodeTag() {}
func (s *SubqueryFromClause) fromTag() {}
func (s *SubqueryFromClause) String() string {
	return "(" + s.Stmt.String() + ") AS " + s.Alias.String()
}

// SortKey is an ORDER BY key.
type SortKey struct {
	Expr       Expr
	Ascending  bool // false = DESC
	NullsFirst bool
}

func (k SortKey) String() string {
	dir := "ASC"
	if !k.Ascending {
		dir = "DESC"
	}
	nulls := ""
	if k.NullsFirst {
		nulls = " NULLS FIRST"
	}
	return k.Expr.String() + " " + dir + nulls
}

// LockClause is FOR UPDATE / FOR SHARE.
type LockClause struct {
	Strength   string // UPDATE, SHARE, NO KEY UPDATE, KEY SHARE
	Of         []string
	NoWait     bool
	SkipLocked bool
}

// WithClause is WITH … AS (…) / WITH RECURSIVE … AS (…).
type WithClause struct {
	Recursive bool
	CTEs      []CTE
}

// CTE is one Common Table Expression.
type CTE struct {
	Name         string
	Stmt         Stmt
	Materialized *bool
}

// SetOperation is UNION / INTERSECT / EXCEPT.
type SetOperation struct {
	Op    string // UNION, INTERSECT, EXCEPT
	All   bool
	Left  Stmt
	Right Stmt
}

func (s *SetOperation) nodeTag() {}
func (s *SetOperation) stmtTag() {}
func (s *SetOperation) String() string {
	op := s.Op
	if s.All {
		op += " ALL"
	}
	return s.Left.String() + " " + op + " " + s.Right.String()
}

// WindowDef is a named window definition.
type WindowDef struct {
	Name string
	Spec WindowSpec
}

// WindowSpec is the body of an OVER (…) clause.
type WindowSpec struct {
	PartitionBy []Expr
	OrderBy     []SortKey
	Frame       *FrameClause
}

// FrameClause is the RANGE / ROWS / GROUPS frame.
type FrameClause struct {
	Type  string // RANGE, ROWS, GROUPS
	Start FrameBound
	End   *FrameBound
}

// FrameBound is one endpoint of a window frame.
type FrameBound struct {
	Type   string // UNBOUNDED PRECEDING, n PRECEDING, CURRENT ROW, n FOLLOWING, UNBOUNDED FOLLOWING
	Offset Expr
}

// InsertSource is the source of data for INSERT.
type InsertSource interface {
	Node
	insertSourceTag()
}

// ValuesSource is VALUES (…), (…), …
type ValuesSource struct{ Rows [][]Expr }

func (v *ValuesSource) nodeTag()         {}
func (v *ValuesSource) insertSourceTag() {}
func (v *ValuesSource) String() string {
	var rows []string
	for _, row := range v.Rows {
		var vals []string
		for _, val := range row {
			vals = append(vals, val.String())
		}
		rows = append(rows, "("+strings.Join(vals, ", ")+")")
	}
	return "VALUES " + strings.Join(rows, ", ")
}

// DefaultValues is used for INSERT INTO t DEFAULT VALUES.
type DefaultValues struct{}

func (d *DefaultValues) nodeTag()         {}
func (d *DefaultValues) insertSourceTag() {}
func (d *DefaultValues) String() string   { return "DEFAULT VALUES" }

// SelectSource is INSERT INTO t SELECT …
type SelectSource struct{ Stmt *SelectStmt }

func (s *SelectSource) nodeTag()         {}
func (s *SelectSource) insertSourceTag() {}
func (s *SelectSource) String() string   { return s.Stmt.String() }

// Assignment is one SET column = expr.
type Assignment struct {
	Column Expr
	Value  Expr
}

func (a Assignment) String() string {
	return a.Column.String() + " = " + a.Value.String()
}

// OnConflict is ON CONFLICT … DO NOTHING / UPDATE.
type OnConflict struct {
	Target  []Expr // conflict target columns
	Action  string // NOTHING or UPDATE
	Updates []Assignment
	Where   Expr
}

func (o *OnConflict) String() string {
	if o.Action == "NOTHING" {
		return "ON CONFLICT DO NOTHING"
	}
	var updates []string
	for _, u := range o.Updates {
		updates = append(updates, u.String())
	}
	return "ON CONFLICT DO UPDATE SET " + strings.Join(updates, ", ")
}

// ColumnDef is one column definition in CREATE TABLE.
type ColumnDef struct {
	Name        string
	Type        *TypeRef
	Constraints []*ColumnConstraint
}

func (c *ColumnDef) nodeTag() {}
func (c *ColumnDef) String() string {
	var b strings.Builder
	b.WriteString(c.Name)
	b.WriteString(" ")
	b.WriteString(c.Type.String())
	for _, con := range c.Constraints {
		b.WriteString(" ")
		b.WriteString(con.String())
	}
	return b.String()
}

// ColumnConstraint is an inline column constraint.
type ColumnConstraint struct {
	Name       string
	Kind       ColumnConstraintKind
	Default    Expr
	References *ForeignKeyRef
	Check      Expr
	Generated  Expr
}

type ColumnConstraintKind int

const (
	ColConstrPrimaryKey ColumnConstraintKind = iota
	ColConstrUnique
	ColConstrNotNull
	ColConstrNull
	ColConstrDefault
	ColConstrCheck
	ColConstrForeignKey
	ColConstrGenerated
)

func (c *ColumnConstraint) String() string {
	switch c.Kind {
	case ColConstrPrimaryKey:
		return "PRIMARY KEY"
	case ColConstrUnique:
		return "UNIQUE"
	case ColConstrNotNull:
		return "NOT NULL"
	case ColConstrNull:
		return "NULL"
	case ColConstrDefault:
		if c.Default != nil {
			return "DEFAULT " + c.Default.String()
		}
		return "DEFAULT NULL"
	case ColConstrCheck:
		return "CHECK (" + c.Check.String() + ")"
	default:
		return ""
	}
}

// TableConstraint is a table-level constraint.
type TableConstraint struct {
	Name    string
	Kind    TableConstraintKind
	Columns []string
	Ref     *ForeignKeyRef
	Check   Expr
}

type TableConstraintKind int

const (
	TblConstrPrimaryKey TableConstraintKind = iota
	TblConstrUnique
	TblConstrCheck
	TblConstrForeignKey
)

func (c *TableConstraint) String() string {
	switch c.Kind {
	case TblConstrPrimaryKey:
		return fmt.Sprintf("PRIMARY KEY (%s)", strings.Join(c.Columns, ", "))
	case TblConstrUnique:
		return fmt.Sprintf("UNIQUE (%s)", strings.Join(c.Columns, ", "))
	case TblConstrCheck:
		return fmt.Sprintf("CHECK (%s)", c.Check)
	default:
		return ""
	}
}

// ForeignKeyRef is a REFERENCES target.
type ForeignKeyRef struct {
	Table    *TableName
	Columns  []string
	OnDelete string
	OnUpdate string
}

// AlterTableAction is one ALTER TABLE action.
type AlterTableAction interface {
	Node
	alterTableActionTag()
}

// AddColumnAction is ADD COLUMN.
type AddColumnAction struct{ Column *ColumnDef }

func (a *AddColumnAction) nodeTag()             {}
func (a *AddColumnAction) alterTableActionTag() {}
func (a *AddColumnAction) String() string       { return "ADD COLUMN " + a.Column.String() }

// DropColumnAction is DROP COLUMN.
type DropColumnAction struct {
	IfExists bool
	Name     string
}

func (a *DropColumnAction) nodeTag()             {}
func (a *DropColumnAction) alterTableActionTag() {}
func (a *DropColumnAction) String() string       { return "DROP COLUMN " + a.Name }

// RenameTableAction is RENAME TO.
type RenameTableAction struct{ Name string }

func (a *RenameTableAction) nodeTag()             {}
func (a *RenameTableAction) alterTableActionTag() {}
func (a *RenameTableAction) String() string       { return "RENAME TO " + a.Name }

// IndexElement is one element in a CREATE INDEX column list.
type IndexElement struct {
	Expr       Expr
	Ascending  bool
	NullsFirst bool
}

// ─────────────────────────────────────────────────────────────────────────────
//  RawExpr — pre-formatted SQL fragment escape hatch
// ─────────────────────────────────────────────────────────────────────────────

// RawExpr is an AST leaf that holds a pre-formatted SQL string.
// The planner emits it verbatim. The rewriter uses it when the
// rewrite result cannot be represented in the normal AST.
type RawExpr struct{ SQL string }

func (e *RawExpr) nodeTag()       {}
func (e *RawExpr) exprTag()       {}
func (e *RawExpr) String() string { return e.SQL }
