// Package lexer implements a full PostgreSQL SQL lexer.
// The lexer transforms a raw SQL string into a flat list of tokens.
// The parser (sql/parser) then builds an AST from those tokens.
package lexer

import "fmt"

// ─────────────────────────────────────────────────────────────────────────────
//  Token kinds
// ─────────────────────────────────────────────────────────────────────────────

// TokenKind classifies a token.
type TokenKind int

const (
	// ── Literals ──────────────────────────────────────────────────────────
	INTEGER    TokenKind = iota // 42
	FLOAT                       // 3.14
	STRING                      // 'hello'
	DOLLAR_STR                  // $$…$$ or $tag$…$tag$
	BYTES                       // E'\x41'
	BITSTRING                   // B'101'
	IDENTIFIER                  // table_name, "quoted ident"
	PARAM                       // $1, $2
	TYPECAST                    // ::

	// ── Keywords (subset — full list in keywords.go) ──────────────────────
	// DDL
	KW_CREATE
	KW_DROP
	KW_ALTER
	KW_TRUNCATE
	KW_RENAME
	KW_ADD
	KW_COLUMN
	KW_TABLE
	KW_VIEW
	KW_INDEX
	KW_UNIQUE
	KW_PRIMARY
	KW_FOREIGN
	KW_KEY
	KW_REFERENCES
	KW_ON
	KW_DELETE
	KW_UPDATE
	KW_CASCADE
	KW_RESTRICT
	KW_NO
	KW_ACTION
	KW_DEFERRABLE
	KW_INITIALLY
	KW_DEFERRED
	KW_IMMEDIATE
	KW_CONSTRAINT
	KW_CHECK
	KW_DEFAULT
	KW_NOT
	KW_NULL
	KW_IF
	KW_EXISTS
	KW_TEMP
	KW_TEMPORARY
	KW_SCHEMA
	KW_SEQUENCE
	KW_TRIGGER
	KW_FUNCTION
	KW_PROCEDURE
	KW_EXTENSION
	KW_DATABASE

	// DML
	KW_SELECT
	KW_INSERT
	KW_INTO
	KW_VALUES
	KW_FROM
	KW_WHERE
	KW_AND
	KW_OR
	KW_NOT2 // alias for KW_NOT in expression context
	KW_IN
	KW_LIKE
	KW_ILIKE
	KW_SIMILAR
	KW_TO
	KW_BETWEEN
	KW_IS
	KW_TRUE
	KW_FALSE
	KW_GROUP
	KW_BY
	KW_HAVING
	KW_ORDER
	KW_ASC
	KW_DESC
	KW_NULLS
	KW_FIRST
	KW_LAST
	KW_LIMIT
	KW_OFFSET
	KW_FETCH
	KW_NEXT
	KW_ROWS
	KW_ROW
	KW_ONLY
	KW_FOR
	KW_UPDATE2 // in FOR UPDATE
	KW_SHARE
	KW_SKIP
	KW_LOCKED
	KW_NOWAIT
	KW_UNION
	KW_INTERSECT
	KW_EXCEPT
	KW_ALL
	KW_DISTINCT
	KW_AS
	KW_JOIN
	KW_INNER
	KW_OUTER
	KW_LEFT
	KW_RIGHT
	KW_FULL
	KW_CROSS
	KW_NATURAL
	KW_USING
	KW_WITH
	KW_RECURSIVE
	KW_RETURNING
	KW_SET
	KW_DO
	KW_NOTHING
	KW_CONFLICT
	KW_OVERRIDING

	// Type names (parsed as keywords but can act as identifiers)
	KW_INT
	KW_INTEGER
	KW_BIGINT
	KW_SMALLINT
	KW_SERIAL
	KW_BIGSERIAL
	KW_SMALLSERIAL
	KW_REAL
	KW_FLOAT
	KW_DOUBLE
	KW_PRECISION
	KW_NUMERIC
	KW_DECIMAL
	KW_BOOLEAN
	KW_BOOL
	KW_TEXT
	KW_VARCHAR
	KW_CHAR
	KW_CHARACTER
	KW_VARYING
	KW_BYTEA
	KW_DATE
	KW_TIME
	KW_TIMESTAMP
	KW_INTERVAL
	KW_UUID
	KW_JSON
	KW_JSONB
	KW_XML
	KW_ARRAY
	KW_VOID
	KW_IDENTITY
	KW_GENERATED
	KW_ALWAYS
	KW_STORED
	KW_AUTOINCREMENT

	// Functions
	KW_COALESCE
	KW_NULLIF
	KW_GREATEST
	KW_LEAST
	KW_CAST
	KW_EXTRACT
	KW_OVERLAY
	KW_POSITION
	KW_SUBSTRING
	KW_TRIM
	KW_LEADING
	KW_TRAILING
	KW_BOTH
	KW_CASE
	KW_WHEN
	KW_THEN
	KW_ELSE
	KW_END
	KW_OVER
	KW_PARTITION
	KW_WINDOW
	KW_FILTER
	KW_WITHIN
	KW_CURRENT
	KW_UNBOUNDED
	KW_PRECEDING
	KW_FOLLOWING
	KW_RANGE
	KW_GROUPS
	KW_EXCLUDE
	KW_TIES
	KW_CURRENT_ROW

	// TCL
	KW_BEGIN
	KW_COMMIT
	KW_ROLLBACK
	KW_SAVEPOINT
	KW_RELEASE
	KW_START
	KW_TRANSACTION
	KW_ISOLATION
	KW_LEVEL
	KW_READ
	KW_WRITE
	KW_UNCOMMITTED
	KW_COMMITTED
	KW_REPEATABLE
	KW_SERIALIZABLE
	KW_CHAIN

	// Misc
	KW_EXPLAIN
	KW_ANALYZE
	KW_VERBOSE
	KW_COSTS
	KW_BUFFERS
	KW_FORMAT
	KW_COPY
	KW_STDIN
	KW_STDOUT
	KW_CSV
	KW_HEADER
	KW_DELIMITER
	KW_QUOTE
	KW_ESCAPE
	KW_FORCE
	KW_ENCODING
	KW_FREEZE
	KW_PREPARE
	KW_EXECUTE
	KW_DEALLOCATE
	KW_SHOW
	KW_LOCK
	KW_LISTEN
	KW_NOTIFY
	KW_UNLISTEN
	KW_CLUSTER
	KW_VACUUM
	KW_REINDEX
	KW_REFRESH
	KW_MATERIALIZED
	KW_CONCURRENTLY
	KW_LATERAL
	KW_WITHOUT
	KW_ZONE
	KW_WORK
	KW_LOCAL
	KW_SESSION
	KW_OF

	// ── Operators ─────────────────────────────────────────────────────────
	OP_EQ       // =
	OP_NEQ      // <> or !=
	OP_LT       // <
	OP_LTE      // <=
	OP_GT       // >
	OP_GTE      // >=
	OP_PLUS     // +
	OP_MINUS    // -
	OP_STAR     // *
	OP_SLASH    // /
	OP_PERCENT  // %
	OP_CARET    // ^ (power)
	OP_CONCAT   // ||
	OP_ARROW    // ->
	OP_DARROW   // ->>
	OP_POUNDAT  // #>
	OP_POUNDDBL // #>>
	OP_TILDE    // ~
	OP_DTILDE   // !~
	OP_ITILDE   // ~*
	OP_DITILDE  // !~*
	OP_CAST2    // ::  (duplicate for clarity)
	OP_RANGE    // ..  (used in array slices)

	// ── Punctuation ───────────────────────────────────────────────────────
	LPAREN    // (
	RPAREN    // )
	LBRACKET  // [
	RBRACKET  // ]
	COMMA     // ,
	SEMICOLON // ;
	DOT       // .
	COLON     // :
	AT        // @
	HASH      // #
	DOLLAR    // $

	// ── Special ───────────────────────────────────────────────────────────
	EOF
	ILLEGAL // unrecognized character
	COMMENT // -- … or /* … */
	WHITESPACE
)

// ─────────────────────────────────────────────────────────────────────────────
//  Token
// ─────────────────────────────────────────────────────────────────────────────

// Token is a single lexical unit.
type Token struct {
	Kind   TokenKind
	Value  string // exact text from the source
	Line   int    // 1-based source line
	Column int    // 1-based column within the line
	Quoted bool   // true for "quoted identifiers" or E'strings'
}

// String returns a human-readable description for debugging.
func (t Token) String() string {
	return fmt.Sprintf("Token(%s, %q, line=%d col=%d)", t.Kind, t.Value, t.Line, t.Column)
}

// IsKeyword returns true when the token represents a SQL keyword.
func (t Token) IsKeyword() bool {
	return t.Kind >= KW_CREATE && t.Kind <= KW_OF
}

// IsOperator returns true for operator tokens.
func (t Token) IsOperator() bool {
	return t.Kind >= OP_EQ && t.Kind <= OP_RANGE
}

// IsPunctuation returns true for punctuation tokens.
func (t Token) IsPunctuation() bool {
	return t.Kind >= LPAREN && t.Kind <= DOLLAR
}

// ─────────────────────────────────────────────────────────────────────────────
//  TokenKind.String — for debugging
// ─────────────────────────────────────────────────────────────────────────────

func (k TokenKind) String() string {
	names := map[TokenKind]string{
		INTEGER: "INTEGER", FLOAT: "FLOAT", STRING: "STRING",
		IDENTIFIER: "IDENTIFIER", PARAM: "PARAM", TYPECAST: "TYPECAST",
		EOF: "EOF", ILLEGAL: "ILLEGAL",
		OP_EQ: "=", OP_NEQ: "<>", OP_LT: "<", OP_LTE: "<=",
		OP_GT: ">", OP_GTE: ">=", OP_PLUS: "+", OP_MINUS: "-",
		OP_STAR: "*", OP_SLASH: "/",
		LPAREN: "(", RPAREN: ")", COMMA: ",", SEMICOLON: ";",
		DOT: ".", COLON: ":",
		KW_SELECT: "SELECT", KW_INSERT: "INSERT", KW_UPDATE: "UPDATE",
		KW_DELETE: "DELETE", KW_FROM: "FROM", KW_WHERE: "WHERE",
		KW_AND: "AND", KW_OR: "OR", KW_NOT: "NOT",
		KW_CREATE: "CREATE", KW_DROP: "DROP", KW_ALTER: "ALTER",
		KW_BEGIN: "BEGIN", KW_COMMIT: "COMMIT", KW_ROLLBACK: "ROLLBACK",
	}
	if n, ok := names[k]; ok {
		return n
	}
	return fmt.Sprintf("Token(%d)", int(k))
}
