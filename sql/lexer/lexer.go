package lexer

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// ─────────────────────────────────────────────────────────────────────────────
//  Lexer
// ─────────────────────────────────────────────────────────────────────────────

// Lexer transforms a SQL string into a sequence of tokens.
type Lexer struct {
	src    string
	pos    int // current byte position
	line   int // current 1-based line number
	col    int // current 1-based column
	tokens []Token
	idx    int // index into tokens (for Peek/Next)
}

// New creates a new Lexer and tokenises the entire input.
func New(src string) *Lexer {
	l := &Lexer{src: src, line: 1, col: 1}
	l.tokenise()
	return l
}

// Next returns the next token (advancing the cursor).
func (l *Lexer) Next() Token {
	if l.idx >= len(l.tokens) {
		return Token{Kind: EOF}
	}
	t := l.tokens[l.idx]
	l.idx++
	return t
}

// Peek returns the next token without advancing.
func (l *Lexer) Peek() Token {
	if l.idx >= len(l.tokens) {
		return Token{Kind: EOF}
	}
	return l.tokens[l.idx]
}

// PeekN returns the token n steps ahead (0 = next).
func (l *Lexer) PeekN(n int) Token {
	i := l.idx + n
	if i >= len(l.tokens) {
		return Token{Kind: EOF}
	}
	return l.tokens[i]
}

// All returns all tokens (useful for testing and the planner).
func (l *Lexer) All() []Token {
	return l.tokens
}

// Reset rewinds to the beginning.
func (l *Lexer) Reset() { l.idx = 0 }

// ─────────────────────────────────────────────────────────────────────────────
//  Core tokeniser
// ─────────────────────────────────────────────────────────────────────────────

func (l *Lexer) tokenise() {
	for l.pos < len(l.src) {
		tok := l.readToken()
		// Silently skip whitespace and comments (the parser doesn't need them).
		if tok.Kind == WHITESPACE || tok.Kind == COMMENT {
			continue
		}
		l.tokens = append(l.tokens, tok)
	}
	l.tokens = append(l.tokens, Token{Kind: EOF, Line: l.line, Column: l.col})
}

// readToken reads and returns the next token from the source.
func (l *Lexer) readToken() Token {
	startLine, startCol := l.line, l.col

	ch := l.peek()

	// ── Whitespace ────────────────────────────────────────────────────────
	if unicode.IsSpace(rune(ch)) {
		return l.readWhitespace(startLine, startCol)
	}

	// ── Line comment  -- … ────────────────────────────────────────────────
	if ch == '-' && l.peekAt(1) == '-' {
		return l.readLineComment(startLine, startCol)
	}

	// ── Block comment  /* … */ ────────────────────────────────────────────
	if ch == '/' && l.peekAt(1) == '*' {
		return l.readBlockComment(startLine, startCol)
	}

	// ── Dollar-quoted string  $$…$$ ───────────────────────────────────────
	if ch == '$' {
		if tok, ok := l.tryDollarString(startLine, startCol); ok {
			return tok
		}
		// Could be a parameter $1 or just $.
	}

	// ── Parameter  $1 ─────────────────────────────────────────────────────
	if ch == '$' && isDigit(l.peekAt(1)) {
		return l.readParam(startLine, startCol)
	}

	// ── String literals ───────────────────────────────────────────────────
	if ch == '\'' {
		return l.readString(startLine, startCol)
	}
	// E'escape', e'escape'
	if (ch == 'E' || ch == 'e') && l.peekAt(1) == '\'' {
		l.advance()
		tok := l.readString(startLine, startCol)
		tok.Value = "E" + tok.Value
		return tok
	}
	// B'bitstring', b'bitstring'
	if (ch == 'B' || ch == 'b') && l.peekAt(1) == '\'' {
		l.advance()
		tok := l.readString(startLine, startCol)
		tok.Kind = BITSTRING
		tok.Value = "B" + tok.Value
		return tok
	}
	// X'hex', x'hex'
	if (ch == 'X' || ch == 'x') && l.peekAt(1) == '\'' {
		l.advance()
		tok := l.readString(startLine, startCol)
		tok.Kind = BYTES
		tok.Value = "X" + tok.Value
		return tok
	}

	// ── Quoted identifier  "…" ────────────────────────────────────────────
	if ch == '"' {
		return l.readQuotedIdent(startLine, startCol)
	}

	// ── Numbers ───────────────────────────────────────────────────────────
	if isDigit(ch) {
		return l.readNumber(startLine, startCol)
	}
	if ch == '.' && isDigit(l.peekAt(1)) {
		return l.readNumber(startLine, startCol)
	}

	// ── Identifiers and keywords ──────────────────────────────────────────
	if isIdentStart(ch) {
		return l.readIdent(startLine, startCol)
	}

	// ── Operators and punctuation ─────────────────────────────────────────
	return l.readOperatorOrPunct(startLine, startCol)
}

// ─────────────────────────────────────────────────────────────────────────────
//  Whitespace / comments
// ─────────────────────────────────────────────────────────────────────────────

func (l *Lexer) readWhitespace(line, col int) Token {
	var b strings.Builder
	for l.pos < len(l.src) && unicode.IsSpace(rune(l.src[l.pos])) {
		b.WriteByte(l.advance())
	}
	return Token{Kind: WHITESPACE, Value: b.String(), Line: line, Column: col}
}

func (l *Lexer) readLineComment(line, col int) Token {
	var b strings.Builder
	for l.pos < len(l.src) && l.src[l.pos] != '\n' {
		b.WriteByte(l.advance())
	}
	return Token{Kind: COMMENT, Value: b.String(), Line: line, Column: col}
}

func (l *Lexer) readBlockComment(line, col int) Token {
	var b strings.Builder
	b.WriteByte(l.advance()) // /
	b.WriteByte(l.advance()) // *
	depth := 1
	for l.pos < len(l.src) && depth > 0 {
		ch := l.peek()
		if ch == '/' && l.peekAt(1) == '*' {
			b.WriteByte(l.advance())
			b.WriteByte(l.advance())
			depth++
			continue
		}
		if ch == '*' && l.peekAt(1) == '/' {
			b.WriteByte(l.advance())
			b.WriteByte(l.advance())
			depth--
			continue
		}
		b.WriteByte(l.advance())
	}
	return Token{Kind: COMMENT, Value: b.String(), Line: line, Column: col}
}

// ─────────────────────────────────────────────────────────────────────────────
//  String literals
// ─────────────────────────────────────────────────────────────────────────────

func (l *Lexer) readString(line, col int) Token {
	var b strings.Builder
	b.WriteByte(l.advance()) // opening '
	for l.pos < len(l.src) {
		ch := l.advance()
		b.WriteByte(ch)
		if ch == '\'' {
			// Check for escaped quote ''
			if l.pos < len(l.src) && l.src[l.pos] == '\'' {
				b.WriteByte(l.advance())
				continue
			}
			break
		}
	}
	return Token{Kind: STRING, Value: b.String(), Line: line, Column: col}
}

func (l *Lexer) tryDollarString(line, col int) (Token, bool) {
	// Scan for $tag$ pattern.
	i := l.pos + 1
	for i < len(l.src) && (isIdentChar(l.src[i]) || l.src[i] == '_') {
		i++
	}
	if i >= len(l.src) || l.src[i] != '$' {
		return Token{}, false
	}
	tag := l.src[l.pos : i+1] // includes both $
	// Search for closing tag.
	closeIdx := strings.Index(l.src[i+1:], tag)
	if closeIdx < 0 {
		return Token{}, false
	}
	endPos := i + 1 + closeIdx + len(tag)
	value := l.src[l.pos:endPos]
	// Advance lexer position.
	for l.pos < endPos {
		l.advance()
	}
	return Token{Kind: DOLLAR_STR, Value: value, Line: line, Column: col}, true
}

func (l *Lexer) readQuotedIdent(line, col int) Token {
	var b strings.Builder
	b.WriteByte(l.advance()) // opening "
	for l.pos < len(l.src) {
		ch := l.advance()
		b.WriteByte(ch)
		if ch == '"' {
			if l.pos < len(l.src) && l.src[l.pos] == '"' {
				b.WriteByte(l.advance())
				continue
			}
			break
		}
	}
	return Token{Kind: IDENTIFIER, Value: b.String(), Line: line, Column: col, Quoted: true}
}

// ─────────────────────────────────────────────────────────────────────────────
//  Numbers
// ─────────────────────────────────────────────────────────────────────────────

func (l *Lexer) readNumber(line, col int) Token {
	var b strings.Builder
	isFloat := false

	// Integer part.
	for l.pos < len(l.src) && isDigit(l.src[l.pos]) {
		b.WriteByte(l.advance())
	}

	// Decimal part.
	if l.pos < len(l.src) && l.src[l.pos] == '.' && isDigit(l.peekAt(1)) {
		isFloat = true
		b.WriteByte(l.advance()) // .
		for l.pos < len(l.src) && isDigit(l.src[l.pos]) {
			b.WriteByte(l.advance())
		}
	}

	// Exponent.
	if l.pos < len(l.src) && (l.src[l.pos] == 'e' || l.src[l.pos] == 'E') {
		isFloat = true
		b.WriteByte(l.advance()) // e/E
		if l.pos < len(l.src) && (l.src[l.pos] == '+' || l.src[l.pos] == '-') {
			b.WriteByte(l.advance())
		}
		for l.pos < len(l.src) && isDigit(l.src[l.pos]) {
			b.WriteByte(l.advance())
		}
	}

	kind := INTEGER
	if isFloat {
		kind = FLOAT
	}
	return Token{Kind: kind, Value: b.String(), Line: line, Column: col}
}

// ─────────────────────────────────────────────────────────────────────────────
//  Parameters  $1
// ─────────────────────────────────────────────────────────────────────────────

func (l *Lexer) readParam(line, col int) Token {
	var b strings.Builder
	b.WriteByte(l.advance()) // $
	for l.pos < len(l.src) && isDigit(l.src[l.pos]) {
		b.WriteByte(l.advance())
	}
	return Token{Kind: PARAM, Value: b.String(), Line: line, Column: col}
}

// ─────────────────────────────────────────────────────────────────────────────
//  Identifiers and keywords
// ─────────────────────────────────────────────────────────────────────────────

func (l *Lexer) readIdent(line, col int) Token {
	var b strings.Builder
	for l.pos < len(l.src) && isIdentChar(l.src[l.pos]) {
		b.WriteByte(l.advance())
	}
	word := b.String()
	upper := strings.ToUpper(word)
	if kw, ok := keywords[upper]; ok {
		return Token{Kind: kw, Value: upper, Line: line, Column: col}
	}
	return Token{Kind: IDENTIFIER, Value: word, Line: line, Column: col}
}

// ─────────────────────────────────────────────────────────────────────────────
//  Operators and punctuation
// ─────────────────────────────────────────────────────────────────────────────

func (l *Lexer) readOperatorOrPunct(line, col int) Token {
	ch := l.advance()

	switch ch {
	case '(':
		return Token{Kind: LPAREN, Value: "(", Line: line, Column: col}
	case ')':
		return Token{Kind: RPAREN, Value: ")", Line: line, Column: col}
	case '[':
		return Token{Kind: LBRACKET, Value: "[", Line: line, Column: col}
	case ']':
		return Token{Kind: RBRACKET, Value: "]", Line: line, Column: col}
	case ',':
		return Token{Kind: COMMA, Value: ",", Line: line, Column: col}
	case ';':
		return Token{Kind: SEMICOLON, Value: ";", Line: line, Column: col}
	case '.':
		return Token{Kind: DOT, Value: ".", Line: line, Column: col}
	case '@':
		return Token{Kind: AT, Value: "@", Line: line, Column: col}
	case '#':
		if l.pos < len(l.src) {
			if l.src[l.pos] == '>' {
				l.advance()
				if l.pos < len(l.src) && l.src[l.pos] == '>' {
					l.advance()
					return Token{Kind: OP_POUNDDBL, Value: "#>>", Line: line, Column: col}
				}
				return Token{Kind: OP_POUNDAT, Value: "#>", Line: line, Column: col}
			}
		}
		return Token{Kind: HASH, Value: "#", Line: line, Column: col}
	case '+':
		return Token{Kind: OP_PLUS, Value: "+", Line: line, Column: col}
	case '-':
		if l.pos < len(l.src) && l.src[l.pos] == '>' {
			l.advance()
			if l.pos < len(l.src) && l.src[l.pos] == '>' {
				l.advance()
				return Token{Kind: OP_DARROW, Value: "->>", Line: line, Column: col}
			}
			return Token{Kind: OP_ARROW, Value: "->", Line: line, Column: col}
		}
		return Token{Kind: OP_MINUS, Value: "-", Line: line, Column: col}
	case '*':
		return Token{Kind: OP_STAR, Value: "*", Line: line, Column: col}
	case '/':
		return Token{Kind: OP_SLASH, Value: "/", Line: line, Column: col}
	case '%':
		return Token{Kind: OP_PERCENT, Value: "%", Line: line, Column: col}
	case '^':
		return Token{Kind: OP_CARET, Value: "^", Line: line, Column: col}
	case '=':
		return Token{Kind: OP_EQ, Value: "=", Line: line, Column: col}
	case '!':
		if l.pos < len(l.src) && l.src[l.pos] == '=' {
			l.advance()
			return Token{Kind: OP_NEQ, Value: "!=", Line: line, Column: col}
		}
		if l.pos < len(l.src) && l.src[l.pos] == '~' {
			l.advance()
			if l.pos < len(l.src) && l.src[l.pos] == '*' {
				l.advance()
				return Token{Kind: OP_DITILDE, Value: "!~*", Line: line, Column: col}
			}
			return Token{Kind: OP_DTILDE, Value: "!~", Line: line, Column: col}
		}
		return Token{Kind: ILLEGAL, Value: "!", Line: line, Column: col}
	case '<':
		if l.pos < len(l.src) {
			if l.src[l.pos] == '=' {
				l.advance()
				return Token{Kind: OP_LTE, Value: "<=", Line: line, Column: col}
			}
			if l.src[l.pos] == '>' {
				l.advance()
				return Token{Kind: OP_NEQ, Value: "<>", Line: line, Column: col}
			}
		}
		return Token{Kind: OP_LT, Value: "<", Line: line, Column: col}
	case '>':
		if l.pos < len(l.src) && l.src[l.pos] == '=' {
			l.advance()
			return Token{Kind: OP_GTE, Value: ">=", Line: line, Column: col}
		}
		return Token{Kind: OP_GT, Value: ">", Line: line, Column: col}
	case '~':
		if l.pos < len(l.src) && l.src[l.pos] == '*' {
			l.advance()
			return Token{Kind: OP_ITILDE, Value: "~*", Line: line, Column: col}
		}
		return Token{Kind: OP_TILDE, Value: "~", Line: line, Column: col}
	case '|':
		if l.pos < len(l.src) && l.src[l.pos] == '|' {
			l.advance()
			return Token{Kind: OP_CONCAT, Value: "||", Line: line, Column: col}
		}
		return Token{Kind: ILLEGAL, Value: "|", Line: line, Column: col}
	case ':':
		if l.pos < len(l.src) && l.src[l.pos] == ':' {
			l.advance()
			return Token{Kind: TYPECAST, Value: "::", Line: line, Column: col}
		}
		return Token{Kind: COLON, Value: ":", Line: line, Column: col}
	case '$':
		return Token{Kind: DOLLAR, Value: "$", Line: line, Column: col}
	default:
		r, _ := utf8.DecodeRuneInString(string(ch))
		return Token{Kind: ILLEGAL, Value: string(r), Line: line, Column: col}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
//  Helpers
// ─────────────────────────────────────────────────────────────────────────────

// advance consumes the next byte and returns it.
func (l *Lexer) advance() byte {
	ch := l.src[l.pos]
	l.pos++
	if ch == '\n' {
		l.line++
		l.col = 1
	} else {
		l.col++
	}
	return ch
}

// peek returns the current byte without consuming it.
func (l *Lexer) peek() byte {
	if l.pos >= len(l.src) {
		return 0
	}
	return l.src[l.pos]
}

// peekAt returns the byte at pos+n.
func (l *Lexer) peekAt(n int) byte {
	i := l.pos + n
	if i >= len(l.src) {
		return 0
	}
	return l.src[i]
}

func isDigit(ch byte) bool      { return ch >= '0' && ch <= '9' }
func isIdentStart(ch byte) bool { return ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch == '_' }
func isIdentChar(ch byte) bool  { return isIdentStart(ch) || isDigit(ch) }

// Token.Col field alias (avoid name clash with column).
func init() {
	// ensure Token has a Col field (it's defined as Column in token.go,
	// the lexer uses Col for brevity internally)
}

// Token.Col accessor helper — the struct uses "Column" as the field name
// in token.go; we alias here.
type tokenCol = int // compile-time alias check (noop)

// ─────────────────────────────────────────────────────────────────────────────
//  Keyword table
// ─────────────────────────────────────────────────────────────────────────────

// keywords maps uppercase SQL keywords to their TokenKind.
var keywords = map[string]TokenKind{
	"CREATE": KW_CREATE, "DROP": KW_DROP, "ALTER": KW_ALTER,
	"TRUNCATE": KW_TRUNCATE, "RENAME": KW_RENAME, "ADD": KW_ADD,
	"COLUMN": KW_COLUMN, "TABLE": KW_TABLE, "VIEW": KW_VIEW,
	"INDEX": KW_INDEX, "UNIQUE": KW_UNIQUE, "PRIMARY": KW_PRIMARY,
	"FOREIGN": KW_FOREIGN, "KEY": KW_KEY, "REFERENCES": KW_REFERENCES,
	"ON": KW_ON, "DELETE": KW_DELETE, "UPDATE": KW_UPDATE,
	"CASCADE": KW_CASCADE, "RESTRICT": KW_RESTRICT, "NO": KW_NO,
	"ACTION": KW_ACTION, "DEFERRABLE": KW_DEFERRABLE, "INITIALLY": KW_INITIALLY,
	"DEFERRED": KW_DEFERRED, "IMMEDIATE": KW_IMMEDIATE,
	"CONSTRAINT": KW_CONSTRAINT, "CHECK": KW_CHECK,
	"DEFAULT": KW_DEFAULT, "NOT": KW_NOT, "NULL": KW_NULL,
	"IF": KW_IF, "EXISTS": KW_EXISTS, "TEMP": KW_TEMP, "TEMPORARY": KW_TEMPORARY,
	"SCHEMA": KW_SCHEMA, "SEQUENCE": KW_SEQUENCE, "TRIGGER": KW_TRIGGER,
	"FUNCTION": KW_FUNCTION, "PROCEDURE": KW_PROCEDURE,
	"EXTENSION": KW_EXTENSION, "DATABASE": KW_DATABASE,
	"SELECT": KW_SELECT, "INSERT": KW_INSERT, "INTO": KW_INTO,
	"VALUES": KW_VALUES, "FROM": KW_FROM, "WHERE": KW_WHERE,
	"AND": KW_AND, "OR": KW_OR, "IN": KW_IN,
	"LIKE": KW_LIKE, "ILIKE": KW_ILIKE, "SIMILAR": KW_SIMILAR,
	"TO": KW_TO, "BETWEEN": KW_BETWEEN, "IS": KW_IS,
	"TRUE": KW_TRUE, "FALSE": KW_FALSE,
	"GROUP": KW_GROUP, "BY": KW_BY, "HAVING": KW_HAVING,
	"ORDER": KW_ORDER, "ASC": KW_ASC, "DESC": KW_DESC,
	"NULLS": KW_NULLS, "FIRST": KW_FIRST, "LAST": KW_LAST,
	"LIMIT": KW_LIMIT, "OFFSET": KW_OFFSET, "FETCH": KW_FETCH,
	"NEXT": KW_NEXT, "ROWS": KW_ROWS, "ROW": KW_ROW, "ONLY": KW_ONLY,
	"FOR": KW_FOR, "SHARE": KW_SHARE, "SKIP": KW_SKIP,
	"LOCKED": KW_LOCKED, "NOWAIT": KW_NOWAIT,
	"UNION": KW_UNION, "INTERSECT": KW_INTERSECT, "EXCEPT": KW_EXCEPT,
	"ALL": KW_ALL, "DISTINCT": KW_DISTINCT, "AS": KW_AS,
	"JOIN": KW_JOIN, "INNER": KW_INNER, "OUTER": KW_OUTER,
	"LEFT": KW_LEFT, "RIGHT": KW_RIGHT, "FULL": KW_FULL,
	"CROSS": KW_CROSS, "NATURAL": KW_NATURAL, "USING": KW_USING,
	"WITH": KW_WITH, "RECURSIVE": KW_RECURSIVE,
	"RETURNING": KW_RETURNING, "SET": KW_SET,
	"DO": KW_DO, "NOTHING": KW_NOTHING,
	"CONFLICT": KW_CONFLICT, "OVERRIDING": KW_OVERRIDING,
	"INT": KW_INT, "INTEGER": KW_INTEGER, "BIGINT": KW_BIGINT,
	"SMALLINT": KW_SMALLINT, "SERIAL": KW_SERIAL, "BIGSERIAL": KW_BIGSERIAL,
	"SMALLSERIAL": KW_SMALLSERIAL, "REAL": KW_REAL, "FLOAT": KW_FLOAT,
	"DOUBLE": KW_DOUBLE, "PRECISION": KW_PRECISION,
	"NUMERIC": KW_NUMERIC, "DECIMAL": KW_DECIMAL,
	"BOOLEAN": KW_BOOLEAN, "BOOL": KW_BOOL,
	"TEXT": KW_TEXT, "VARCHAR": KW_VARCHAR, "CHAR": KW_CHAR,
	"CHARACTER": KW_CHARACTER, "VARYING": KW_VARYING,
	"BYTEA": KW_BYTEA, "DATE": KW_DATE, "TIME": KW_TIME,
	"TIMESTAMP": KW_TIMESTAMP, "INTERVAL": KW_INTERVAL,
	"UUID": KW_UUID, "JSON": KW_JSON, "JSONB": KW_JSONB,
	"XML": KW_XML, "ARRAY": KW_ARRAY, "VOID": KW_VOID,
	"IDENTITY": KW_IDENTITY, "GENERATED": KW_GENERATED,
	"ALWAYS": KW_ALWAYS, "STORED": KW_STORED,
	"AUTOINCREMENT": KW_AUTOINCREMENT,
	"COALESCE":      KW_COALESCE, "NULLIF": KW_NULLIF,
	"GREATEST": KW_GREATEST, "LEAST": KW_LEAST,
	"CAST": KW_CAST, "EXTRACT": KW_EXTRACT,
	"OVERLAY": KW_OVERLAY, "POSITION": KW_POSITION,
	"SUBSTRING": KW_SUBSTRING, "TRIM": KW_TRIM,
	"LEADING": KW_LEADING, "TRAILING": KW_TRAILING, "BOTH": KW_BOTH,
	"CASE": KW_CASE, "WHEN": KW_WHEN, "THEN": KW_THEN,
	"ELSE": KW_ELSE, "END": KW_END,
	"OVER": KW_OVER, "PARTITION": KW_PARTITION,
	"WINDOW": KW_WINDOW, "FILTER": KW_FILTER,
	"WITHIN": KW_WITHIN, "CURRENT": KW_CURRENT,
	"UNBOUNDED": KW_UNBOUNDED, "PRECEDING": KW_PRECEDING,
	"FOLLOWING": KW_FOLLOWING, "RANGE": KW_RANGE,
	"GROUPS": KW_GROUPS, "EXCLUDE": KW_EXCLUDE,
	"TIES": KW_TIES, "CURRENT_ROW": KW_CURRENT_ROW,
	"BEGIN": KW_BEGIN, "COMMIT": KW_COMMIT, "ROLLBACK": KW_ROLLBACK,
	"SAVEPOINT": KW_SAVEPOINT, "RELEASE": KW_RELEASE,
	"START": KW_START, "TRANSACTION": KW_TRANSACTION,
	"ISOLATION": KW_ISOLATION, "LEVEL": KW_LEVEL,
	"READ": KW_READ, "WRITE": KW_WRITE,
	"UNCOMMITTED": KW_UNCOMMITTED, "COMMITTED": KW_COMMITTED,
	"REPEATABLE": KW_REPEATABLE, "SERIALIZABLE": KW_SERIALIZABLE,
	"CHAIN":   KW_CHAIN,
	"EXPLAIN": KW_EXPLAIN, "ANALYZE": KW_ANALYZE, "VERBOSE": KW_VERBOSE,
	"COSTS": KW_COSTS, "BUFFERS": KW_BUFFERS, "FORMAT": KW_FORMAT,
	"COPY": KW_COPY, "STDIN": KW_STDIN, "STDOUT": KW_STDOUT,
	"CSV": KW_CSV, "HEADER": KW_HEADER, "DELIMITER": KW_DELIMITER,
	"QUOTE": KW_QUOTE, "ESCAPE": KW_ESCAPE,
	"PREPARE": KW_PREPARE, "EXECUTE": KW_EXECUTE, "DEALLOCATE": KW_DEALLOCATE,
	"SHOW": KW_SHOW, "LOCK": KW_LOCK, "LISTEN": KW_LISTEN,
	"NOTIFY": KW_NOTIFY, "UNLISTEN": KW_UNLISTEN,
	"CLUSTER": KW_CLUSTER, "VACUUM": KW_VACUUM, "REINDEX": KW_REINDEX,
	"REFRESH": KW_REFRESH, "MATERIALIZED": KW_MATERIALIZED,
	"CONCURRENTLY": KW_CONCURRENTLY, "LATERAL": KW_LATERAL,
	"WITHOUT": KW_WITHOUT, "ZONE": KW_ZONE, "WORK": KW_WORK,
	"LOCAL": KW_LOCAL, "SESSION": KW_SESSION, "OF": KW_OF,
}
