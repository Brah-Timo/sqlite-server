// Package parser implements a recursive-descent parser for the PostgreSQL SQL
// dialect.  It consumes tokens produced by the lexer and returns a typed AST.
//
// The parser follows a standard top-down approach:
//
//	parseStmt        — entry point, dispatches on first keyword
//	parseSelect      — SELECT / WITH
//	parseInsert      — INSERT INTO
//	parseUpdate      — UPDATE
//	parseDelete      — DELETE FROM
//	parseDDL         — CREATE / DROP / ALTER
//	parseTCL         — BEGIN / COMMIT / ROLLBACK / SAVEPOINT
//	parseExpr        — expression parser (Pratt-style, operator precedence)
package parser

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/sqlite-server/sqlite-server/sql/ast"
	"github.com/sqlite-server/sqlite-server/sql/lexer"
)

// ─────────────────────────────────────────────────────────────────────────────
//  Parser
// ─────────────────────────────────────────────────────────────────────────────

// Parser holds state during a parse.
type Parser struct {
	lex    *lexer.Lexer
	errors []ParseError
}

// ParseError records a position and message for a parse failure.
type ParseError struct {
	Line    int
	Column  int
	Message string
}

func (e ParseError) Error() string {
	return fmt.Sprintf("parse error at line %d col %d: %s", e.Line, e.Column, e.Message)
}

// Parse parses a SQL string and returns one or more statements.
func Parse(sql string) ([]ast.Stmt, error) {
	p := &Parser{lex: lexer.New(sql)}
	var stmts []ast.Stmt

	for p.peek().Kind != lexer.EOF {
		// Skip leading semicolons.
		for p.peek().Kind == lexer.SEMICOLON {
			p.advance()
		}
		if p.peek().Kind == lexer.EOF {
			break
		}
		stmt := p.parseStmt()
		if stmt != nil {
			stmts = append(stmts, stmt)
		}
		// Consume optional trailing semicolon.
		if p.peek().Kind == lexer.SEMICOLON {
			p.advance()
		}
	}

	if len(p.errors) > 0 {
		return stmts, p.errors[0]
	}
	return stmts, nil
}

// ParseOne parses a single statement.
func ParseOne(sql string) (ast.Stmt, error) {
	stmts, err := Parse(sql)
	if err != nil {
		return nil, err
	}
	if len(stmts) == 0 {
		return nil, nil
	}
	return stmts[0], nil
}

// ─────────────────────────────────────────────────────────────────────────────
//  Top-level statement dispatcher
// ─────────────────────────────────────────────────────────────────────────────

func (p *Parser) parseStmt() ast.Stmt {
	tok := p.peek()

	switch tok.Kind {
	case lexer.KW_SELECT, lexer.KW_WITH, lexer.KW_VALUES:
		return p.parseSelectOrWith()
	case lexer.KW_INSERT:
		return p.parseInsert()
	case lexer.KW_UPDATE:
		return p.parseUpdate()
	case lexer.KW_DELETE:
		return p.parseDelete()
	case lexer.KW_CREATE:
		return p.parseCreate()
	case lexer.KW_DROP:
		return p.parseDrop()
	case lexer.KW_ALTER:
		return p.parseAlterTable()
	case lexer.KW_BEGIN, lexer.KW_START:
		return p.parseBegin()
	case lexer.KW_COMMIT, lexer.KW_END:
		return p.parseCommit()
	case lexer.KW_ROLLBACK:
		return p.parseRollback()
	case lexer.KW_SAVEPOINT:
		return p.parseSavepoint()
	case lexer.KW_RELEASE:
		return p.parseReleaseSavepoint()
	case lexer.KW_SET:
		return p.parseSet()
	case lexer.KW_SHOW:
		return p.parseShow()
	case lexer.KW_EXPLAIN:
		return p.parseExplain()
	default:
		// Unknown — wrap in RawStmt and try to recover.
		return p.parseRaw()
	}
}

// ─────────────────────────────────────────────────────────────────────────────
//  SELECT
// ─────────────────────────────────────────────────────────────────────────────

func (p *Parser) parseSelectOrWith() ast.Stmt {
	// WITH clause
	var with *ast.WithClause
	if p.peek().Kind == lexer.KW_WITH {
		with = p.parseWith()
	}

	var stmt ast.Stmt
	if p.peek().Kind == lexer.KW_VALUES {
		stmt = p.parseValues()
	} else {
		sel := p.parseSelect()
		sel.With = with
		stmt = sel
	}

	// UNION / INTERSECT / EXCEPT
	for p.peek().Kind == lexer.KW_UNION ||
		p.peek().Kind == lexer.KW_INTERSECT ||
		p.peek().Kind == lexer.KW_EXCEPT {
		op := strings.ToUpper(p.advance().Value)
		all := false
		if p.peek().Kind == lexer.KW_ALL {
			p.advance()
			all = true
		} else if p.peek().Kind == lexer.KW_DISTINCT {
			p.advance()
		}
		right := p.parseSelect()
		stmt = &ast.SetOperation{Op: op, All: all, Left: stmt, Right: right}
	}
	return stmt
}

func (p *Parser) parseSelect() *ast.SelectStmt {
	p.expect(lexer.KW_SELECT)
	sel := &ast.SelectStmt{}

	// DISTINCT / ALL
	if p.peek().Kind == lexer.KW_ALL {
		p.advance()
	} else if p.peek().Kind == lexer.KW_DISTINCT {
		p.advance()
		sel.Distinct = true
		if p.peek().Kind == lexer.KW_ON {
			p.advance() // ON
			p.expect(lexer.LPAREN)
			for {
				sel.DistinctOn = append(sel.DistinctOn, p.parseExpr(0))
				if p.peek().Kind != lexer.COMMA {
					break
				}
				p.advance()
			}
			p.expect(lexer.RPAREN)
		}
	}

	// SELECT list
	sel.Targets = p.parseSelectList()

	// FROM
	if p.peek().Kind == lexer.KW_FROM {
		p.advance()
		sel.From = p.parseFromList()
	}

	// WHERE
	if p.peek().Kind == lexer.KW_WHERE {
		p.advance()
		sel.Where = p.parseExpr(0)
	}

	// GROUP BY
	if p.peek().Kind == lexer.KW_GROUP {
		p.advance()
		p.expect(lexer.KW_BY)
		for {
			sel.GroupBy = append(sel.GroupBy, p.parseExpr(0))
			if p.peek().Kind != lexer.COMMA {
				break
			}
			p.advance()
		}
	}

	// HAVING
	if p.peek().Kind == lexer.KW_HAVING {
		p.advance()
		sel.Having = p.parseExpr(0)
	}

	// WINDOW
	if p.peek().Kind == lexer.KW_WINDOW {
		p.advance()
		for {
			name := p.expectIdent()
			p.expectKw(lexer.KW_AS)
			p.expect(lexer.LPAREN)
			spec := p.parseWindowSpec()
			p.expect(lexer.RPAREN)
			sel.Window = append(sel.Window, ast.WindowDef{Name: name, Spec: spec})
			if p.peek().Kind != lexer.COMMA {
				break
			}
			p.advance()
		}
	}

	// ORDER BY
	if p.peek().Kind == lexer.KW_ORDER {
		p.advance()
		p.expect(lexer.KW_BY)
		sel.OrderBy = p.parseOrderBy()
	}

	// LIMIT / OFFSET / FETCH
	sel.Limit, sel.Offset = p.parseLimitOffset()

	// FOR UPDATE / SHARE
	if p.peek().Kind == lexer.KW_FOR {
		sel.LockClause = p.parseLockClause()
	}

	return sel
}

func (p *Parser) parseSelectList() []ast.SelectTarget {
	var targets []ast.SelectTarget
	for {
		target := p.parseSelectTarget()
		targets = append(targets, target)
		if p.peek().Kind != lexer.COMMA {
			break
		}
		p.advance()
	}
	return targets
}

func (p *Parser) parseSelectTarget() ast.SelectTarget {
	// * is a special case.
	if p.peek().Kind == lexer.OP_STAR {
		p.advance()
		return ast.SelectTarget{Expr: &ast.Star{}}
	}
	expr := p.parseExpr(0)
	var alias *ast.Ident
	// AS alias or bare alias (some tools omit AS).
	if p.peek().Kind == lexer.KW_AS {
		p.advance()
		name := p.expectIdent()
		alias = &ast.Ident{Name: name}
	} else if p.peek().Kind == lexer.IDENTIFIER {
		// Bare alias (not followed by a keyword that could be a column name).
		tok := p.peek()
		if !isReservedKeyword(tok.Kind) {
			name := p.advance().Value
			alias = &ast.Ident{Name: name}
		}
	}
	return ast.SelectTarget{Expr: expr, Alias: alias}
}

// ─────────────────────────────────────────────────────────────────────────────
//  FROM clause
// ─────────────────────────────────────────────────────────────────────────────

func (p *Parser) parseFromList() []ast.FromClause {
	var items []ast.FromClause
	items = append(items, p.parseFromItem())
	for p.peek().Kind == lexer.COMMA {
		p.advance()
		items = append(items, p.parseFromItem())
	}
	return items
}

func (p *Parser) parseFromItem() ast.FromClause {
	var item ast.FromClause

	// Subquery
	if p.peek().Kind == lexer.LPAREN {
		p.advance()
		sub := p.parseSelect()
		p.expect(lexer.RPAREN)
		alias := p.parseOptionalAlias()
		item = &ast.SubqueryFromClause{Stmt: sub, Alias: alias}
	} else if p.peek().Kind == lexer.KW_LATERAL {
		p.advance()
		p.expect(lexer.LPAREN)
		sub := p.parseSelect()
		p.expect(lexer.RPAREN)
		alias := p.parseOptionalAlias()
		item = &ast.SubqueryFromClause{Stmt: sub, Alias: alias}
	} else {
		// Plain table reference
		name := p.parseTableName()
		alias := p.parseOptionalAlias()
		item = &ast.TableRef{Name: name, Alias: alias}
	}

	// JOINs
	for {
		joinType := ""
		switch p.peek().Kind {
		case lexer.KW_INNER:
			p.advance()
			p.expectKw(lexer.KW_JOIN)
			joinType = "INNER"
		case lexer.KW_LEFT:
			p.advance()
			if p.peek().Kind == lexer.KW_OUTER {
				p.advance()
			}
			p.expectKw(lexer.KW_JOIN)
			joinType = "LEFT"
		case lexer.KW_RIGHT:
			p.advance()
			if p.peek().Kind == lexer.KW_OUTER {
				p.advance()
			}
			p.expectKw(lexer.KW_JOIN)
			joinType = "RIGHT"
		case lexer.KW_FULL:
			p.advance()
			if p.peek().Kind == lexer.KW_OUTER {
				p.advance()
			}
			p.expectKw(lexer.KW_JOIN)
			joinType = "FULL"
		case lexer.KW_CROSS:
			p.advance()
			p.expectKw(lexer.KW_JOIN)
			joinType = "CROSS"
		case lexer.KW_NATURAL:
			p.advance()
			p.expectKw(lexer.KW_JOIN)
			joinType = "NATURAL"
		case lexer.KW_JOIN:
			p.advance()
			joinType = "INNER"
		}
		if joinType == "" {
			break
		}

		right := p.parseFromItem()
		je := &ast.JoinExpr{JoinType: joinType, Left: item, Right: right}

		if joinType != "CROSS" && joinType != "NATURAL" {
			if p.peek().Kind == lexer.KW_ON {
				p.advance()
				je.On = p.parseExpr(0)
			} else if p.peek().Kind == lexer.KW_USING {
				p.advance()
				p.expect(lexer.LPAREN)
				for {
					je.Using = append(je.Using, p.expectIdent())
					if p.peek().Kind != lexer.COMMA {
						break
					}
					p.advance()
				}
				p.expect(lexer.RPAREN)
			}
		}
		item = je
	}

	return item
}

// ─────────────────────────────────────────────────────────────────────────────
//  INSERT
// ─────────────────────────────────────────────────────────────────────────────

func (p *Parser) parseInsert() ast.Stmt {
	p.advance() // INSERT
	var with *ast.WithClause

	p.expect(lexer.KW_INTO)
	table := p.parseTableName()

	// Optional column list
	var cols []*ast.Ident
	if p.peek().Kind == lexer.LPAREN {
		// Make sure it's a column list, not a subquery.
		p.advance()
		if p.peek().Kind != lexer.KW_SELECT && p.peek().Kind != lexer.KW_WITH {
			for {
				name := p.expectIdent()
				cols = append(cols, &ast.Ident{Name: name})
				if p.peek().Kind != lexer.COMMA {
					break
				}
				p.advance()
			}
			p.expect(lexer.RPAREN)
		}
	}

	// OVERRIDING …
	if p.peek().Kind == lexer.KW_OVERRIDING {
		p.advance() // OVERRIDING
		p.advance() // SYSTEM | USER
		p.advance() // VALUE
	}

	// Source: VALUES | DEFAULT VALUES | SELECT
	var source ast.InsertSource
	switch p.peek().Kind {
	case lexer.KW_DEFAULT:
		p.advance()
		p.expectKw(lexer.KW_VALUES)
		source = &ast.DefaultValues{}
	case lexer.KW_VALUES:
		source = p.parseValuesSource()
	case lexer.KW_SELECT, lexer.KW_WITH:
		stmt := p.parseSelectOrWith()
		source = &ast.SelectSource{Stmt: stmt.(*ast.SelectStmt)}
	default:
		p.errorf("expected VALUES, DEFAULT VALUES, or SELECT in INSERT")
		source = &ast.DefaultValues{}
	}

	stmt := &ast.InsertStmt{
		With:    with,
		Table:   table,
		Columns: cols,
		Source:  source,
	}

	// ON CONFLICT
	if p.peek().Kind == lexer.KW_ON {
		p.advance()
		p.expectIdent() // CONFLICT
		stmt.OnConflict = p.parseOnConflict()
	}

	// RETURNING
	if p.peek().Kind == lexer.KW_RETURNING {
		p.advance()
		stmt.Returning = p.parseSelectList()
	}

	return stmt
}

func (p *Parser) parseValuesSource() *ast.ValuesSource {
	p.advance() // VALUES
	vs := &ast.ValuesSource{}
	for {
		p.expect(lexer.LPAREN)
		var row []ast.Expr
		for {
			if p.peek().Kind == lexer.KW_DEFAULT {
				p.advance()
				row = append(row, &ast.Literal{Kind: ast.LitNull, Value: "DEFAULT"})
			} else {
				row = append(row, p.parseExpr(0))
			}
			if p.peek().Kind != lexer.COMMA {
				break
			}
			p.advance()
		}
		p.expect(lexer.RPAREN)
		vs.Rows = append(vs.Rows, row)
		if p.peek().Kind != lexer.COMMA {
			break
		}
		p.advance()
	}
	return vs
}

func (p *Parser) parseValues() ast.Stmt {
	vs := p.parseValuesSource()
	// Wrap as a SelectStmt for uniformity.
	sel := &ast.SelectStmt{
		Targets: []ast.SelectTarget{{Expr: &ast.Star{}}},
	}
	_ = vs
	return sel
}

func (p *Parser) parseOnConflict() *ast.OnConflict {
	oc := &ast.OnConflict{}
	// Optional target
	if p.peek().Kind == lexer.LPAREN {
		p.advance()
		for {
			oc.Target = append(oc.Target, p.parseExpr(0))
			if p.peek().Kind != lexer.COMMA {
				break
			}
			p.advance()
		}
		p.expect(lexer.RPAREN)
		// Optional WHERE
		if p.peek().Kind == lexer.KW_WHERE {
			p.advance()
			_ = p.parseExpr(0)
		}
	}
	p.expectKw(lexer.KW_DO)
	switch p.peek().Kind {
	case lexer.KW_NOTHING:
		p.advance()
		oc.Action = "NOTHING"
	case lexer.KW_UPDATE:
		p.advance()
		p.expectKw(lexer.KW_SET)
		oc.Action = "UPDATE"
		for {
			col := p.parseExpr(0)
			p.expect(lexer.OP_EQ)
			val := p.parseExpr(0)
			oc.Updates = append(oc.Updates, ast.Assignment{Column: col, Value: val})
			if p.peek().Kind != lexer.COMMA {
				break
			}
			p.advance()
		}
		if p.peek().Kind == lexer.KW_WHERE {
			p.advance()
			oc.Where = p.parseExpr(0)
		}
	}
	return oc
}

// ─────────────────────────────────────────────────────────────────────────────
//  UPDATE
// ─────────────────────────────────────────────────────────────────────────────

func (p *Parser) parseUpdate() ast.Stmt {
	p.advance() // UPDATE
	// Optional ONLY
	if p.peek().Kind == lexer.KW_ONLY {
		p.advance()
	}
	table := p.parseTableName()
	var alias *ast.Ident
	if p.peek().Kind == lexer.KW_AS {
		p.advance()
		name := p.expectIdent()
		alias = &ast.Ident{Name: name}
	}

	p.expectKw(lexer.KW_SET)
	var sets []ast.Assignment
	for {
		col := p.parseExpr(0)
		p.expect(lexer.OP_EQ)
		val := p.parseExpr(0)
		sets = append(sets, ast.Assignment{Column: col, Value: val})
		if p.peek().Kind != lexer.COMMA {
			break
		}
		p.advance()
	}

	var from []ast.FromClause
	if p.peek().Kind == lexer.KW_FROM {
		p.advance()
		from = p.parseFromList()
	}

	var where ast.Expr
	if p.peek().Kind == lexer.KW_WHERE {
		p.advance()
		where = p.parseExpr(0)
	}

	stmt := &ast.UpdateStmt{
		Table: table, Alias: alias, Sets: sets, From: from, Where: where,
	}

	if p.peek().Kind == lexer.KW_RETURNING {
		p.advance()
		stmt.Returning = p.parseSelectList()
	}

	return stmt
}

// ─────────────────────────────────────────────────────────────────────────────
//  DELETE
// ─────────────────────────────────────────────────────────────────────────────

func (p *Parser) parseDelete() ast.Stmt {
	p.advance() // DELETE
	p.expect(lexer.KW_FROM)
	// Optional ONLY
	if p.peek().Kind == lexer.KW_ONLY {
		p.advance()
	}
	table := p.parseTableName()
	var alias *ast.Ident
	if p.peek().Kind == lexer.KW_AS {
		p.advance()
		name := p.expectIdent()
		alias = &ast.Ident{Name: name}
	}

	var using []ast.FromClause
	if p.peek().Kind == lexer.KW_USING {
		p.advance()
		using = p.parseFromList()
	}

	var where ast.Expr
	if p.peek().Kind == lexer.KW_WHERE {
		p.advance()
		where = p.parseExpr(0)
	}

	stmt := &ast.DeleteStmt{
		Table: table, Alias: alias, Using: using, Where: where,
	}

	if p.peek().Kind == lexer.KW_RETURNING {
		p.advance()
		stmt.Returning = p.parseSelectList()
	}

	return stmt
}

// ─────────────────────────────────────────────────────────────────────────────
//  DDL
// ─────────────────────────────────────────────────────────────────────────────

func (p *Parser) parseCreate() ast.Stmt {
	p.advance() // CREATE
	switch p.peek().Kind {
	case lexer.KW_TABLE, lexer.KW_TEMP, lexer.KW_TEMPORARY:
		return p.parseCreateTable()
	case lexer.KW_INDEX, lexer.KW_UNIQUE:
		return p.parseCreateIndex()
	default:
		// Fall through for CREATE VIEW, FUNCTION, SEQUENCE, etc.
		return p.parseRaw()
	}
}

func (p *Parser) parseCreateTable() ast.Stmt {
	temp := false
	if p.peek().Kind == lexer.KW_TEMP || p.peek().Kind == lexer.KW_TEMPORARY {
		p.advance()
		temp = true
	}
	p.expectKw(lexer.KW_TABLE)

	ifNotExists := false
	if p.peek().Kind == lexer.KW_IF {
		p.advance()
		p.expectKw(lexer.KW_NOT)
		p.expectKw(lexer.KW_NULL) // EXISTS — we repurpose NOT NULL check
		ifNotExists = true
	}

	name := p.parseTableName()
	p.expect(lexer.LPAREN)

	var cols []*ast.ColumnDef
	var tblConstraints []*ast.TableConstraint

	for p.peek().Kind != lexer.RPAREN && p.peek().Kind != lexer.EOF {
		// Table-level constraints
		if p.peek().Kind == lexer.KW_PRIMARY || p.peek().Kind == lexer.KW_UNIQUE ||
			p.peek().Kind == lexer.KW_FOREIGN || p.peek().Kind == lexer.KW_CHECK ||
			p.peek().Kind == lexer.KW_CONSTRAINT {
			tc := p.parseTableConstraint()
			tblConstraints = append(tblConstraints, tc)
		} else {
			col := p.parseColumnDef()
			cols = append(cols, col)
		}
		if p.peek().Kind == lexer.COMMA {
			p.advance()
		} else {
			break
		}
	}
	p.expect(lexer.RPAREN)

	return &ast.CreateTableStmt{
		Temp:        temp,
		IfNotExists: ifNotExists,
		Name:        name,
		Columns:     cols,
		Constraints: tblConstraints,
	}
}

func (p *Parser) parseColumnDef() *ast.ColumnDef {
	name := p.expectIdent()
	typeRef := p.parseTypeRef()
	var constraints []*ast.ColumnConstraint

	for {
		con := p.tryParseColumnConstraint()
		if con == nil {
			break
		}
		constraints = append(constraints, con)
	}

	return &ast.ColumnDef{Name: name, Type: typeRef, Constraints: constraints}
}

func (p *Parser) parseTypeRef() *ast.TypeRef {
	ref := &ast.TypeRef{}
	// Schema-qualified type
	if p.peek().Kind == lexer.IDENTIFIER && p.lex.PeekN(1).Kind == lexer.DOT {
		ref.Schema = p.advance().Value
		p.advance() // .
	}
	ref.Name = p.expectIdentOrKeyword()

	// Handle DOUBLE PRECISION, CHARACTER VARYING, etc.
	switch strings.ToUpper(ref.Name) {
	case "DOUBLE":
		if p.peek().Kind == lexer.KW_PRECISION {
			p.advance()
			ref.Name = "DOUBLE PRECISION"
		}
	case "CHARACTER", "CHAR", "NATIONAL":
		if p.peek().Kind == lexer.KW_VARYING {
			p.advance()
			ref.Name = "CHARACTER VARYING"
		}
	case "BIT":
		if p.peek().Kind == lexer.KW_VARYING {
			p.advance()
			ref.Name = "BIT VARYING"
		}
	case "TIMESTAMP", "TIME":
		if p.peek().Kind == lexer.KW_WITH || p.peek().Kind == lexer.KW_WITHOUT {
			withOrWithout := p.advance().Value
			p.expectKw(lexer.KW_TIME)
			p.expectKw(lexer.KW_ZONE)
			if strings.ToUpper(withOrWithout) == "WITH" {
				ref.Name += "TZ"
			}
		}
	}

	// Optional modifiers (n) or (p, s).
	if p.peek().Kind == lexer.LPAREN {
		p.advance()
		for {
			if n, err := strconv.Atoi(p.peek().Value); err == nil {
				p.advance()
				ref.Modifiers = append(ref.Modifiers, n)
			} else {
				p.advance() // skip non-numeric like MAX
			}
			if p.peek().Kind != lexer.COMMA {
				break
			}
			p.advance()
		}
		p.expect(lexer.RPAREN)
	}

	// Array suffix.
	if p.peek().Kind == lexer.LBRACKET {
		p.advance()
		p.expect(lexer.RBRACKET)
		ref.IsArray = true
	}

	return ref
}

func (p *Parser) tryParseColumnConstraint() *ast.ColumnConstraint {
	tok := p.peek()
	switch tok.Kind {
	case lexer.KW_NOT:
		p.advance()
		p.expectKw(lexer.KW_NULL)
		return &ast.ColumnConstraint{Kind: ast.ColConstrNotNull}
	case lexer.KW_NULL:
		p.advance()
		return &ast.ColumnConstraint{Kind: ast.ColConstrNull}
	case lexer.KW_PRIMARY:
		p.advance()
		p.expectKw(lexer.KW_KEY)
		return &ast.ColumnConstraint{Kind: ast.ColConstrPrimaryKey}
	case lexer.KW_UNIQUE:
		p.advance()
		return &ast.ColumnConstraint{Kind: ast.ColConstrUnique}
	case lexer.KW_DEFAULT:
		p.advance()
		val := p.parseExpr(0)
		return &ast.ColumnConstraint{Kind: ast.ColConstrDefault, Default: val}
	case lexer.KW_CHECK:
		p.advance()
		p.expect(lexer.LPAREN)
		expr := p.parseExpr(0)
		p.expect(lexer.RPAREN)
		return &ast.ColumnConstraint{Kind: ast.ColConstrCheck, Check: expr}
	case lexer.KW_REFERENCES:
		p.advance()
		// Just skip for now; full FK parsing can be added later.
		p.parseTableName()
		if p.peek().Kind == lexer.LPAREN {
			p.advance()
			p.expectIdent()
			p.expect(lexer.RPAREN)
		}
		return &ast.ColumnConstraint{Kind: ast.ColConstrForeignKey}
	case lexer.KW_GENERATED:
		// GENERATED ALWAYS AS IDENTITY / AS (expr) STORED
		for tok.Kind != lexer.COMMA && tok.Kind != lexer.RPAREN && tok.Kind != lexer.EOF {
			tok = p.advance()
		}
		return &ast.ColumnConstraint{Kind: ast.ColConstrGenerated}
	case lexer.KW_CONSTRAINT:
		p.advance()
		_ = p.expectIdent() // constraint name
		return p.tryParseColumnConstraint()
	}
	return nil
}

func (p *Parser) parseTableConstraint() *ast.TableConstraint {
	tc := &ast.TableConstraint{}
	if p.peek().Kind == lexer.KW_CONSTRAINT {
		p.advance()
		tc.Name = p.expectIdent()
	}
	switch p.peek().Kind {
	case lexer.KW_PRIMARY:
		p.advance()
		p.expectKw(lexer.KW_KEY)
		tc.Kind = ast.TblConstrPrimaryKey
		tc.Columns = p.parseColumnNameList()
	case lexer.KW_UNIQUE:
		p.advance()
		tc.Kind = ast.TblConstrUnique
		tc.Columns = p.parseColumnNameList()
	case lexer.KW_CHECK:
		p.advance()
		p.expect(lexer.LPAREN)
		tc.Check = p.parseExpr(0)
		p.expect(lexer.RPAREN)
		tc.Kind = ast.TblConstrCheck
	case lexer.KW_FOREIGN:
		p.advance()
		p.expectKw(lexer.KW_KEY)
		tc.Columns = p.parseColumnNameList()
		tc.Kind = ast.TblConstrForeignKey
		if p.peek().Kind == lexer.KW_REFERENCES {
			p.advance()
			refTable := p.parseTableName()
			var refCols []string
			if p.peek().Kind == lexer.LPAREN {
				refCols = p.parseColumnNameList()
			}
			tc.Ref = &ast.ForeignKeyRef{Table: refTable, Columns: refCols}
			// ON DELETE / ON UPDATE
			for p.peek().Kind == lexer.KW_ON {
				p.advance()
				action := strings.ToUpper(p.advance().Value)    // DELETE or UPDATE
				refAction := strings.ToUpper(p.advance().Value) // CASCADE, etc.
				if action == "DELETE" {
					tc.Ref.OnDelete = refAction
				} else {
					tc.Ref.OnUpdate = refAction
				}
			}
		}
	}
	return tc
}

func (p *Parser) parseColumnNameList() []string {
	p.expect(lexer.LPAREN)
	var cols []string
	for {
		cols = append(cols, p.expectIdent())
		if p.peek().Kind != lexer.COMMA {
			break
		}
		p.advance()
	}
	p.expect(lexer.RPAREN)
	return cols
}

func (p *Parser) parseCreateIndex() ast.Stmt {
	unique := false
	if p.peek().Kind == lexer.KW_UNIQUE {
		p.advance()
		unique = true
	}
	p.expectKw(lexer.KW_INDEX)
	stmt := &ast.CreateIndexStmt{Unique: unique}

	if p.peek().Kind == lexer.KW_IF {
		p.advance()
		p.expectKw(lexer.KW_NOT)
		p.expectKw(lexer.KW_NULL) // EXISTS
		stmt.IfNotExists = true
	}

	if p.peek().Kind == lexer.IDENTIFIER {
		name := p.advance().Value
		stmt.Name = &ast.Ident{Name: name}
	}

	p.expectKw(lexer.KW_ON)
	stmt.Table = p.parseTableName()

	p.expect(lexer.LPAREN)
	for {
		col := p.parseExpr(0)
		elem := ast.IndexElement{Expr: col, Ascending: true}
		if p.peek().Kind == lexer.KW_ASC {
			p.advance()
		} else if p.peek().Kind == lexer.KW_DESC {
			p.advance()
			elem.Ascending = false
		}
		stmt.Columns = append(stmt.Columns, elem)
		if p.peek().Kind != lexer.COMMA {
			break
		}
		p.advance()
	}
	p.expect(lexer.RPAREN)

	if p.peek().Kind == lexer.KW_WHERE {
		p.advance()
		stmt.Where = p.parseExpr(0)
	}
	return stmt
}

func (p *Parser) parseDrop() ast.Stmt {
	p.advance() // DROP
	switch p.peek().Kind {
	case lexer.KW_TABLE:
		p.advance()
		stmt := &ast.DropTableStmt{}
		if p.peek().Kind == lexer.KW_IF {
			p.advance()
			p.expectKw(lexer.KW_EXISTS)
			stmt.IfExists = true
		}
		for {
			stmt.Tables = append(stmt.Tables, p.parseTableName())
			if p.peek().Kind != lexer.COMMA {
				break
			}
			p.advance()
		}
		if p.peek().Kind == lexer.KW_CASCADE {
			p.advance()
			stmt.Cascade = true
		} else if p.peek().Kind == lexer.KW_RESTRICT {
			p.advance()
		}
		return stmt
	default:
		return p.parseRaw()
	}
}

func (p *Parser) parseAlterTable() ast.Stmt {
	p.advance() // ALTER
	p.expectKw(lexer.KW_TABLE)
	// Optional ONLY / IF EXISTS
	for p.peek().Kind == lexer.KW_IF || p.peek().Kind == lexer.KW_ONLY {
		if p.peek().Kind == lexer.KW_IF {
			p.advance()
			p.expectKw(lexer.KW_EXISTS)
		} else {
			p.advance()
		}
	}
	name := p.parseTableName()
	var action ast.AlterTableAction

	switch p.peek().Kind {
	case lexer.KW_ADD:
		p.advance()
		if p.peek().Kind == lexer.KW_COLUMN {
			p.advance()
		}
		col := p.parseColumnDef()
		action = &ast.AddColumnAction{Column: col}
	case lexer.KW_DROP:
		p.advance()
		if p.peek().Kind == lexer.KW_COLUMN {
			p.advance()
		}
		da := &ast.DropColumnAction{}
		if p.peek().Kind == lexer.KW_IF {
			p.advance()
			p.expectKw(lexer.KW_EXISTS)
			da.IfExists = true
		}
		da.Name = p.expectIdent()
		action = da
	case lexer.KW_RENAME:
		p.advance()
		p.expectKw(lexer.KW_TO)
		action = &ast.RenameTableAction{Name: p.expectIdent()}
	default:
		return p.parseRaw()
	}

	return &ast.AlterTableStmt{Name: name, Action: action}
}

// ─────────────────────────────────────────────────────────────────────────────
//  TCL
// ─────────────────────────────────────────────────────────────────────────────

func (p *Parser) parseBegin() ast.Stmt {
	p.advance()
	if p.peek().Kind == lexer.KW_TRANSACTION || p.peek().Kind == lexer.KW_WORK {
		p.advance()
	}
	stmt := &ast.BeginStmt{}
	// Parse optional isolation level.
	for p.peek().Kind == lexer.KW_ISOLATION ||
		p.peek().Kind == lexer.KW_READ ||
		p.peek().Kind == lexer.KW_WRITE ||
		p.peek().Kind == lexer.KW_NOT {
		p.advance()
	}
	return stmt
}

func (p *Parser) parseCommit() ast.Stmt {
	p.advance()
	for p.peek().Kind == lexer.KW_TRANSACTION || p.peek().Kind == lexer.KW_WORK ||
		p.peek().Kind == lexer.KW_CHAIN {
		p.advance()
	}
	return &ast.CommitStmt{}
}

func (p *Parser) parseRollback() ast.Stmt {
	p.advance()
	for p.peek().Kind == lexer.KW_TRANSACTION || p.peek().Kind == lexer.KW_WORK ||
		p.peek().Kind == lexer.KW_CHAIN {
		p.advance()
	}
	stmt := &ast.RollbackStmt{}
	if p.peek().Kind == lexer.KW_TO {
		p.advance()
		if p.peek().Kind == lexer.KW_SAVEPOINT {
			p.advance()
		}
		stmt.Savepoint = p.expectIdent()
	}
	return stmt
}

func (p *Parser) parseSavepoint() ast.Stmt {
	p.advance()
	name := p.expectIdent()
	return &ast.SavepointStmt{Name: name}
}

func (p *Parser) parseReleaseSavepoint() ast.Stmt {
	p.advance()
	if p.peek().Kind == lexer.KW_SAVEPOINT {
		p.advance()
	}
	name := p.expectIdent()
	return &ast.ReleaseSavepointStmt{Name: name}
}

func (p *Parser) parseSet() ast.Stmt {
	p.advance()
	if p.peek().Kind == lexer.KW_LOCAL || p.peek().Kind == lexer.KW_SESSION {
		p.advance()
	}
	name := p.expectIdentOrKeyword()
	stmt := &ast.SetStmt{Name: name}
	if p.peek().Kind == lexer.OP_EQ || p.peek().Kind == lexer.KW_TO {
		p.advance()
		var vals []string
		for {
			vals = append(vals, p.advance().Value)
			if p.peek().Kind != lexer.COMMA {
				break
			}
			p.advance()
		}
		stmt.Value = strings.Join(vals, ", ")
	}
	return stmt
}

func (p *Parser) parseShow() ast.Stmt {
	p.advance()
	name := p.expectIdentOrKeyword()
	return &ast.ShowStmt{Name: name}
}

func (p *Parser) parseExplain() ast.Stmt {
	p.advance()
	stmt := &ast.ExplainStmt{}
	if p.peek().Kind == lexer.KW_ANALYZE {
		p.advance()
		stmt.Analyze = true
	}
	if p.peek().Kind == lexer.KW_VERBOSE {
		p.advance()
		stmt.Verbose = true
	}
	// Options in parentheses
	if p.peek().Kind == lexer.LPAREN {
		p.advance()
		for p.peek().Kind != lexer.RPAREN && p.peek().Kind != lexer.EOF {
			p.advance()
		}
		p.expect(lexer.RPAREN)
	}
	stmt.Inner = p.parseStmt()
	return stmt
}

// ─────────────────────────────────────────────────────────────────────────────
//  WITH clause
// ─────────────────────────────────────────────────────────────────────────────

func (p *Parser) parseWith() *ast.WithClause {
	p.advance() // WITH
	wc := &ast.WithClause{}
	if p.peek().Kind == lexer.KW_RECURSIVE {
		p.advance()
		wc.Recursive = true
	}
	for {
		name := p.expectIdent()
		// Optional column list
		if p.peek().Kind == lexer.LPAREN {
			p.advance()
			for p.peek().Kind != lexer.RPAREN {
				p.expectIdent()
				if p.peek().Kind == lexer.COMMA {
					p.advance()
				}
			}
			p.advance()
		}
		p.expectKw(lexer.KW_AS)
		// Optional MATERIALIZED / NOT MATERIALIZED
		var mat *bool
		if p.peek().Kind == lexer.KW_NOT {
			p.advance()
			b := false
			mat = &b
		} else if p.peek().Kind == lexer.KW_MATERIALIZED {
			p.advance()
			b := true
			mat = &b
		}
		_ = mat
		p.expect(lexer.LPAREN)
		inner := p.parseStmt()
		p.expect(lexer.RPAREN)
		wc.CTEs = append(wc.CTEs, ast.CTE{Name: name, Stmt: inner})
		if p.peek().Kind != lexer.COMMA {
			break
		}
		p.advance()
	}
	return wc
}

// ─────────────────────────────────────────────────────────────────────────────
//  ORDER BY / LIMIT / OFFSET
// ─────────────────────────────────────────────────────────────────────────────

func (p *Parser) parseOrderBy() []ast.SortKey {
	var keys []ast.SortKey
	for {
		expr := p.parseExpr(0)
		key := ast.SortKey{Expr: expr, Ascending: true}
		if p.peek().Kind == lexer.KW_ASC {
			p.advance()
		} else if p.peek().Kind == lexer.KW_DESC {
			p.advance()
			key.Ascending = false
		}
		if p.peek().Kind == lexer.KW_NULLS {
			p.advance()
			if p.peek().Kind == lexer.KW_FIRST {
				p.advance()
				key.NullsFirst = true
			} else if p.peek().Kind == lexer.KW_LAST {
				p.advance()
			}
		}
		keys = append(keys, key)
		if p.peek().Kind != lexer.COMMA {
			break
		}
		p.advance()
	}
	return keys
}

func (p *Parser) parseLimitOffset() (limit, offset ast.Expr) {
	for {
		switch p.peek().Kind {
		case lexer.KW_LIMIT:
			p.advance()
			if p.peek().Kind == lexer.KW_ALL {
				p.advance() // LIMIT ALL = no limit
			} else {
				limit = p.parseExpr(0)
			}
		case lexer.KW_OFFSET:
			p.advance()
			offset = p.parseExpr(0)
			if p.peek().Kind == lexer.KW_ROW || p.peek().Kind == lexer.KW_ROWS {
				p.advance()
			}
		case lexer.KW_FETCH:
			p.advance()
			if p.peek().Kind == lexer.KW_FIRST || p.peek().Kind == lexer.KW_NEXT {
				p.advance()
			}
			limit = p.parseExpr(0)
			if p.peek().Kind == lexer.KW_ROW || p.peek().Kind == lexer.KW_ROWS {
				p.advance()
			}
			if p.peek().Kind == lexer.KW_ONLY {
				p.advance()
			}
		default:
			return
		}
	}
}

func (p *Parser) parseLockClause() *ast.LockClause {
	p.advance() // FOR
	lc := &ast.LockClause{Strength: strings.ToUpper(p.advance().Value)}
	if p.peek().Kind == lexer.KW_OF {
		p.advance()
		for {
			lc.Of = append(lc.Of, p.expectIdent())
			if p.peek().Kind != lexer.COMMA {
				break
			}
			p.advance()
		}
	}
	if p.peek().Kind == lexer.KW_NOWAIT {
		p.advance()
		lc.NoWait = true
	} else if p.peek().Kind == lexer.KW_SKIP {
		p.advance()
		p.expectKw(lexer.KW_LOCKED)
		lc.SkipLocked = true
	}
	return lc
}

func (p *Parser) parseWindowSpec() ast.WindowSpec {
	spec := ast.WindowSpec{}
	if p.peek().Kind == lexer.KW_PARTITION {
		p.advance()
		p.expectKw(lexer.KW_BY)
		for {
			spec.PartitionBy = append(spec.PartitionBy, p.parseExpr(0))
			if p.peek().Kind != lexer.COMMA {
				break
			}
			p.advance()
		}
	}
	if p.peek().Kind == lexer.KW_ORDER {
		p.advance()
		p.expectKw(lexer.KW_BY)
		spec.OrderBy = p.parseOrderBy()
	}
	return spec
}

// ─────────────────────────────────────────────────────────────────────────────
//  Expression parser (Pratt / top-down operator precedence)
// ─────────────────────────────────────────────────────────────────────────────

// Operator precedences (higher = tighter binding).
const (
	precOr      = 1
	precAnd     = 2
	precNot     = 3
	precCmp     = 4 // = <> < <= > >=
	precLike    = 5 // LIKE ILIKE SIMILAR
	precIs      = 5
	precIn      = 5
	precBetween = 5
	precConcat  = 6 // ||
	precAdd     = 7 // + -
	precMul     = 8 // * / %
	precUnary   = 9
	precCast    = 10 // ::
	precMember  = 11 // -> ->>
)

func (p *Parser) parseExpr(minPrec int) ast.Expr {
	left := p.parsePrimary()

	for {
		op, prec := p.peekInfix()
		if prec < minPrec {
			break
		}

		// Handle special forms first.
		switch p.peek().Kind {
		case lexer.TYPECAST, lexer.OP_CAST2:
			p.advance()
			typeRef := p.parseTypeRef()
			left = &ast.TypeCast{Expr: left, Type: typeRef}
			continue

		case lexer.KW_NOT:
			// NOT LIKE, NOT IN, NOT BETWEEN, NOT SIMILAR
			p.advance()
			switch p.peek().Kind {
			case lexer.KW_LIKE:
				p.advance()
				right := p.parsePrimary()
				left = &ast.LikeExpr{Expr: left, Not: true, Op: "LIKE", Pattern: right}
			case lexer.KW_ILIKE:
				p.advance()
				right := p.parsePrimary()
				left = &ast.LikeExpr{Expr: left, Not: true, Op: "ILIKE", Pattern: right}
			case lexer.KW_IN:
				p.advance()
				left = p.parseInTail(left, true)
			case lexer.KW_BETWEEN:
				p.advance()
				left = p.parseBetweenTail(left, true)
			case lexer.KW_SIMILAR:
				p.advance()
				p.expectKw(lexer.KW_TO)
				right := p.parsePrimary()
				left = &ast.LikeExpr{Expr: left, Not: true, Op: "SIMILAR TO", Pattern: right}
			default:
				// Give back and break.
				break
			}
			continue

		case lexer.KW_IS:
			p.advance()
			not := false
			if p.peek().Kind == lexer.KW_NOT {
				p.advance()
				not = true
			}
			// IS NULL / IS TRUE / IS FALSE / IS DISTINCT FROM
			switch p.peek().Kind {
			case lexer.KW_NULL:
				p.advance()
				left = &ast.IsNullExpr{Expr: left, Not: not}
			case lexer.KW_TRUE, lexer.KW_FALSE:
				val := strings.ToUpper(p.advance().Value)
				op := "="
				if not {
					op = "<>"
				}
				left = &ast.BinaryExpr{Op: op, Left: left, Right: &ast.Literal{Kind: ast.LitString, Value: val}}
			default:
				// skip unknown IS form
				p.advance()
			}
			continue

		case lexer.KW_LIKE:
			p.advance()
			right := p.parsePrimary()
			like := &ast.LikeExpr{Expr: left, Op: "LIKE", Pattern: right}
			if p.peek().Kind == lexer.KW_ESCAPE {
				p.advance()
				like.Escape = p.parsePrimary()
			}
			left = like
			continue

		case lexer.KW_ILIKE:
			p.advance()
			right := p.parsePrimary()
			left = &ast.LikeExpr{Expr: left, Op: "ILIKE", Pattern: right}
			continue

		case lexer.KW_IN:
			p.advance()
			left = p.parseInTail(left, false)
			continue

		case lexer.KW_BETWEEN:
			p.advance()
			left = p.parseBetweenTail(left, false)
			continue

		case lexer.KW_AND:
			if prec < minPrec {
				break
			}
			p.advance()
			right := p.parseExpr(prec + 1)
			left = &ast.BinaryExpr{Op: "AND", Left: left, Right: right}
			continue

		case lexer.KW_OR:
			if prec < minPrec {
				break
			}
			p.advance()
			right := p.parseExpr(prec + 1)
			left = &ast.BinaryExpr{Op: "OR", Left: left, Right: right}
			continue
		}

		p.advance()
		right := p.parseExpr(prec + 1)
		left = &ast.BinaryExpr{Op: op, Left: left, Right: right}
	}

	return left
}

// parsePrimary parses a primary expression (literal, ident, function, etc.).
func (p *Parser) parsePrimary() ast.Expr {
	tok := p.peek()

	switch tok.Kind {

	// ── Literals ──────────────────────────────────────────────────────────
	case lexer.INTEGER:
		p.advance()
		return &ast.Literal{Kind: ast.LitInteger, Value: tok.Value}
	case lexer.FLOAT:
		p.advance()
		return &ast.Literal{Kind: ast.LitFloat, Value: tok.Value}
	case lexer.STRING, lexer.DOLLAR_STR, lexer.BYTES, lexer.BITSTRING:
		p.advance()
		return &ast.Literal{Kind: ast.LitString, Value: tok.Value}
	case lexer.KW_TRUE:
		p.advance()
		return &ast.Literal{Kind: ast.LitTrue, Value: "TRUE"}
	case lexer.KW_FALSE:
		p.advance()
		return &ast.Literal{Kind: ast.LitFalse, Value: "FALSE"}
	case lexer.KW_NULL:
		p.advance()
		return &ast.Literal{Kind: ast.LitNull, Value: "NULL"}

	// ── Parameter  $1 ─────────────────────────────────────────────────────
	case lexer.PARAM:
		p.advance()
		idx, _ := strconv.Atoi(tok.Value[1:])
		return &ast.Param{Index: idx}

	// ── Star  * ───────────────────────────────────────────────────────────
	case lexer.OP_STAR:
		p.advance()
		return &ast.Star{}

	// ── Unary operators ───────────────────────────────────────────────────
	case lexer.KW_NOT:
		p.advance()
		expr := p.parseExpr(precNot)
		return &ast.UnaryExpr{Op: "NOT", Expr: expr}
	case lexer.OP_MINUS:
		p.advance()
		expr := p.parseExpr(precUnary)
		return &ast.UnaryExpr{Op: "-", Expr: expr}
	case lexer.OP_PLUS:
		p.advance()
		return p.parseExpr(precUnary)

	// ── Parenthesised expression or subquery ──────────────────────────────
	case lexer.LPAREN:
		p.advance()
		if p.peek().Kind == lexer.KW_SELECT || p.peek().Kind == lexer.KW_WITH {
			sub := p.parseSelectOrWith().(*ast.SelectStmt)
			p.expect(lexer.RPAREN)
			return &ast.Subquery{Stmt: sub}
		}
		expr := p.parseExpr(0)
		// Could be a row expression: (a, b, c)
		if p.peek().Kind == lexer.COMMA {
			row := &ast.RowExpr{Elements: []ast.Expr{expr}}
			for p.peek().Kind == lexer.COMMA {
				p.advance()
				row.Elements = append(row.Elements, p.parseExpr(0))
			}
			p.expect(lexer.RPAREN)
			return row
		}
		p.expect(lexer.RPAREN)
		return expr

	// ── CASE ──────────────────────────────────────────────────────────────
	case lexer.KW_CASE:
		return p.parseCaseExpr()

	// ── EXISTS ────────────────────────────────────────────────────────────
	case lexer.KW_EXISTS:
		p.advance()
		p.expect(lexer.LPAREN)
		sub := p.parseSelectOrWith().(*ast.SelectStmt)
		p.expect(lexer.RPAREN)
		return &ast.ExistsExpr{Subquery: sub}

	// ── EXTRACT ───────────────────────────────────────────────────────────
	case lexer.KW_EXTRACT:
		p.advance()
		p.expect(lexer.LPAREN)
		field := p.expectIdentOrKeyword()
		p.expectKw(lexer.KW_FROM)
		expr := p.parseExpr(0)
		p.expect(lexer.RPAREN)
		return &ast.ExtractExpr{Field: field, Expr: expr}

	// ── CAST ──────────────────────────────────────────────────────────────
	case lexer.KW_CAST:
		p.advance()
		p.expect(lexer.LPAREN)
		expr := p.parseExpr(0)
		p.expectKw(lexer.KW_AS)
		typeRef := p.parseTypeRef()
		p.expect(lexer.RPAREN)
		return &ast.TypeCast{Expr: expr, Type: typeRef}

	// ── ARRAY ─────────────────────────────────────────────────────────────
	case lexer.KW_ARRAY:
		p.advance()
		p.expect(lexer.LBRACKET)
		arr := &ast.ArrayExpr{}
		if p.peek().Kind != lexer.RBRACKET {
			for {
				arr.Elements = append(arr.Elements, p.parseExpr(0))
				if p.peek().Kind != lexer.COMMA {
					break
				}
				p.advance()
			}
		}
		p.expect(lexer.RBRACKET)
		return arr

	// ── Identifier or function call ────────────────────────────────────────
	case lexer.IDENTIFIER:
		return p.parseIdentOrFunc()

	// ── Keyword-as-identifier or built-in function ────────────────────────
	default:
		if tok.IsKeyword() {
			return p.parseIdentOrFunc()
		}
		p.advance()
		p.errorf("unexpected token %s in expression", tok.String())
		return &ast.Literal{Kind: ast.LitNull, Value: "NULL"}
	}
}

func (p *Parser) parseIdentOrFunc() ast.Expr {
	name := p.advance().Value
	schema := ""

	// Schema-qualified: schema.name(
	if p.peek().Kind == lexer.DOT {
		p.advance()
		schema = name
		name = p.advance().Value
	}

	// Function call
	if p.peek().Kind == lexer.LPAREN {
		return p.parseFuncCall(schema, name)
	}

	// Another dot → qualified column reference
	if p.peek().Kind == lexer.DOT {
		p.advance()
		if p.peek().Kind == lexer.OP_STAR {
			p.advance()
			return &ast.Star{Table: name}
		}
		colName := p.advance().Value
		return &ast.ColumnRef{Table: name, Column: colName}
	}

	return &ast.Ident{Name: name}
}

func (p *Parser) parseFuncCall(schema, name string) *ast.FuncCall {
	p.expect(lexer.LPAREN)
	fc := &ast.FuncCall{Schema: schema, Name: strings.ToUpper(name)}

	if p.peek().Kind == lexer.OP_STAR {
		p.advance()
		fc.Star = true
	} else if p.peek().Kind == lexer.KW_ALL {
		p.advance() // ALL is default, skip
		fc.Args = p.parseFuncArgs()
	} else if p.peek().Kind == lexer.KW_DISTINCT {
		p.advance()
		fc.Distinct = true
		fc.Args = p.parseFuncArgs()
	} else if p.peek().Kind != lexer.RPAREN {
		fc.Args = p.parseFuncArgs()
	}
	p.expect(lexer.RPAREN)

	// FILTER (WHERE …)
	if p.peek().Kind == lexer.KW_FILTER {
		p.advance()
		p.expect(lexer.LPAREN)
		p.expectKw(lexer.KW_WHERE)
		fc.Filter = p.parseExpr(0)
		p.expect(lexer.RPAREN)
	}

	// OVER (…)
	if p.peek().Kind == lexer.KW_OVER {
		p.advance()
		if p.peek().Kind == lexer.IDENTIFIER {
			// Named window reference
			_ = p.advance().Value
		} else {
			p.expect(lexer.LPAREN)
			spec := p.parseWindowSpec()
			fc.Over = &spec
			p.expect(lexer.RPAREN)
		}
	}

	return fc
}

func (p *Parser) parseFuncArgs() []ast.Expr {
	var args []ast.Expr
	for p.peek().Kind != lexer.RPAREN && p.peek().Kind != lexer.EOF {
		args = append(args, p.parseExpr(0))
		if p.peek().Kind != lexer.COMMA {
			break
		}
		p.advance()
	}
	return args
}

func (p *Parser) parseCaseExpr() *ast.CaseExpr {
	p.advance() // CASE
	ce := &ast.CaseExpr{}

	// Optional input expression (simple CASE).
	if p.peek().Kind != lexer.KW_WHEN {
		ce.Input = p.parseExpr(0)
	}

	for p.peek().Kind == lexer.KW_WHEN {
		p.advance()
		cond := p.parseExpr(0)
		p.expectKw(lexer.KW_THEN)
		result := p.parseExpr(0)
		ce.Whens = append(ce.Whens, ast.WhenClause{Cond: cond, Result: result})
	}

	if p.peek().Kind == lexer.KW_ELSE {
		p.advance()
		ce.Default = p.parseExpr(0)
	}

	p.expectKw(lexer.KW_END)
	return ce
}

func (p *Parser) parseInTail(left ast.Expr, not bool) ast.Expr {
	p.expect(lexer.LPAREN)
	ie := &ast.InExpr{Expr: left, Not: not}
	if p.peek().Kind == lexer.KW_SELECT || p.peek().Kind == lexer.KW_WITH {
		ie.Subquery = p.parseSelectOrWith().(*ast.SelectStmt)
	} else {
		for {
			ie.List = append(ie.List, p.parseExpr(0))
			if p.peek().Kind != lexer.COMMA {
				break
			}
			p.advance()
		}
	}
	p.expect(lexer.RPAREN)
	return ie
}

func (p *Parser) parseBetweenTail(left ast.Expr, not bool) ast.Expr {
	low := p.parseExpr(precBetween + 1)
	p.expectKw(lexer.KW_AND)
	high := p.parseExpr(precBetween + 1)
	return &ast.BetweenExpr{Expr: left, Not: not, Low: low, High: high}
}

// peekInfix returns the infix operator string and precedence for the current token.
func (p *Parser) peekInfix() (op string, prec int) {
	tok := p.peek()
	switch tok.Kind {
	case lexer.KW_OR:
		return "OR", precOr
	case lexer.KW_AND:
		return "AND", precAnd
	case lexer.KW_NOT:
		return "NOT", precNot
	case lexer.KW_IS, lexer.KW_ILIKE, lexer.KW_LIKE, lexer.KW_IN, lexer.KW_BETWEEN, lexer.KW_SIMILAR:
		return tok.Value, precCmp
	case lexer.OP_EQ:
		return "=", precCmp
	case lexer.OP_NEQ:
		return "<>", precCmp
	case lexer.OP_LT:
		return "<", precCmp
	case lexer.OP_LTE:
		return "<=", precCmp
	case lexer.OP_GT:
		return ">", precCmp
	case lexer.OP_GTE:
		return ">=", precCmp
	case lexer.OP_CONCAT:
		return "||", precConcat
	case lexer.OP_PLUS:
		return "+", precAdd
	case lexer.OP_MINUS:
		return "-", precAdd
	case lexer.OP_STAR:
		return "*", precMul
	case lexer.OP_SLASH:
		return "/", precMul
	case lexer.OP_PERCENT:
		return "%", precMul
	case lexer.OP_CARET:
		return "^", precMul
	case lexer.TYPECAST, lexer.OP_CAST2:
		return "::", precCast
	case lexer.OP_ARROW:
		return "->", precMember
	case lexer.OP_DARROW:
		return "->>", precMember
	case lexer.OP_TILDE:
		return "~", precCmp
	case lexer.OP_DTILDE:
		return "!~", precCmp
	case lexer.OP_ITILDE:
		return "~*", precCmp
	case lexer.OP_DITILDE:
		return "!~*", precCmp
	}
	return "", -1
}

// ─────────────────────────────────────────────────────────────────────────────
//  Helper parsers
// ─────────────────────────────────────────────────────────────────────────────

func (p *Parser) parseTableName() *ast.TableName {
	name := p.expectIdentOrKeyword()
	if p.peek().Kind == lexer.DOT {
		p.advance()
		schema := name
		name = p.expectIdentOrKeyword()
		return &ast.TableName{Schema: schema, Name: name}
	}
	return &ast.TableName{Name: name}
}

func (p *Parser) parseOptionalAlias() *ast.Ident {
	if p.peek().Kind == lexer.KW_AS {
		p.advance()
		name := p.expectIdent()
		return &ast.Ident{Name: name}
	}
	// Bare alias — only if the next token is an identifier (not a keyword
	// that could start a new clause).
	if p.peek().Kind == lexer.IDENTIFIER {
		name := p.advance().Value
		return &ast.Ident{Name: name}
	}
	return nil
}

// parseRaw consumes tokens until a statement boundary and wraps them in RawStmt.
func (p *Parser) parseRaw() ast.Stmt {
	var b strings.Builder
	for p.peek().Kind != lexer.SEMICOLON && p.peek().Kind != lexer.EOF {
		b.WriteString(p.advance().Value)
		b.WriteString(" ")
	}
	return &ast.RawStmt{SQL: strings.TrimSpace(b.String())}
}

// ─────────────────────────────────────────────────────────────────────────────
//  Token-level utilities
// ─────────────────────────────────────────────────────────────────────────────

func (p *Parser) peek() lexer.Token    { return p.lex.Peek() }
func (p *Parser) advance() lexer.Token { return p.lex.Next() }

func (p *Parser) expect(kind lexer.TokenKind) lexer.Token {
	tok := p.lex.Next()
	if tok.Kind != kind {
		p.errorf("expected %s, got %s (%q)", kind, tok.Kind, tok.Value)
	}
	return tok
}

func (p *Parser) expectKw(kind lexer.TokenKind) lexer.Token {
	return p.expect(kind)
}

func (p *Parser) expectIdent() string {
	tok := p.lex.Next()
	if tok.Kind != lexer.IDENTIFIER && !tok.IsKeyword() {
		p.errorf("expected identifier, got %s (%q)", tok.Kind, tok.Value)
		return "_error_"
	}
	return tok.Value
}

func (p *Parser) expectIdentOrKeyword() string {
	tok := p.lex.Next()
	if tok.Kind == lexer.IDENTIFIER || tok.IsKeyword() {
		return tok.Value
	}
	p.errorf("expected identifier or keyword, got %s (%q)", tok.Kind, tok.Value)
	return "_error_"
}

func (p *Parser) errorf(format string, args ...interface{}) {
	tok := p.lex.Peek()
	p.errors = append(p.errors, ParseError{
		Line:    tok.Line,
		Column:  tok.Column,
		Message: fmt.Sprintf(format, args...),
	})
}

// isReservedKeyword returns true for keywords that cannot be bare identifiers.
func isReservedKeyword(k lexer.TokenKind) bool {
	switch k {
	case lexer.KW_FROM, lexer.KW_WHERE, lexer.KW_GROUP, lexer.KW_ORDER,
		lexer.KW_HAVING, lexer.KW_LIMIT, lexer.KW_OFFSET, lexer.KW_UNION,
		lexer.KW_INTERSECT, lexer.KW_EXCEPT, lexer.KW_JOIN, lexer.KW_INNER,
		lexer.KW_LEFT, lexer.KW_RIGHT, lexer.KW_FULL, lexer.KW_CROSS,
		lexer.KW_ON, lexer.KW_AND, lexer.KW_OR, lexer.KW_NOT,
		lexer.KW_IS, lexer.KW_IN, lexer.KW_BETWEEN, lexer.KW_LIKE,
		lexer.KW_ILIKE, lexer.KW_SELECT, lexer.KW_INSERT, lexer.KW_UPDATE,
		lexer.KW_DELETE, lexer.KW_SET, lexer.KW_RETURNING, lexer.SEMICOLON,
		lexer.KW_BEGIN, lexer.KW_COMMIT, lexer.KW_ROLLBACK, lexer.KW_END,
		lexer.KW_WHEN, lexer.KW_THEN, lexer.KW_ELSE, lexer.KW_CASE,
		lexer.KW_WINDOW, lexer.KW_OVER, lexer.KW_PARTITION, lexer.KW_FOR:
		return true
	}
	return false
}

// Token.Col is mapped via a minor alias trick in the lexer package.
// We access it as .Column from here.
var _ = lexer.Token{}.Column
