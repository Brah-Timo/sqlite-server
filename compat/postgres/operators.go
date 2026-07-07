package postgres

import "strings"

// ─────────────────────────────────────────────────────────────────────────────
//  PostgreSQL operator compatibility table
// ─────────────────────────────────────────────────────────────────────────────

// OpSupport describes how a PostgreSQL operator is handled.
type OpSupport int

const (
	OpNative      OpSupport = iota // identical behaviour in SQLite
	OpRewritten                    // rewritten to SQLite equivalent
	OpEmulated                     // approximate rewrite
	OpUnsupported                  // no equivalent; error or NULL
)

// OperatorInfo describes one PostgreSQL operator.
type OperatorInfo struct {
	PGOp       string // PostgreSQL operator symbol
	LeftType   string // left operand type (empty = prefix/postfix)
	RightType  string // right operand type (empty = prefix/postfix)
	Support    OpSupport
	SQLiteForm string // SQLite equivalent
	Notes      string
}

// Operators is the full operator compatibility table.
var Operators = []OperatorInfo{

	// ── Comparison ────────────────────────────────────────────────────────
	{PGOp: "=", Support: OpNative},
	{PGOp: "<>", Support: OpNative},
	{PGOp: "!=", Support: OpNative},
	{PGOp: "<", Support: OpNative},
	{PGOp: "<=", Support: OpNative},
	{PGOp: ">", Support: OpNative},
	{PGOp: ">=", Support: OpNative},

	// ── Arithmetic ────────────────────────────────────────────────────────
	{PGOp: "+", Support: OpNative},
	{PGOp: "-", Support: OpNative},
	{PGOp: "*", Support: OpNative},
	{PGOp: "/", Support: OpNative},
	{PGOp: "%", Support: OpNative},
	{PGOp: "^", Support: OpNative, SQLiteForm: "pow(x,y)", Notes: "SQLite uses ** or pow()"},
	{PGOp: "|/", Support: OpRewritten, SQLiteForm: "sqrt(x)"},
	{PGOp: "||/", Support: OpRewritten, SQLiteForm: "pow(x, 1.0/3)"},
	{PGOp: "@", Support: OpRewritten, SQLiteForm: "abs(x)"},
	{PGOp: "~", Support: OpNative}, // bitwise NOT (integer context)

	// ── Bitwise ───────────────────────────────────────────────────────────
	{PGOp: "&", Support: OpNative},  // bitwise AND
	{PGOp: "|", Support: OpNative},  // bitwise OR
	{PGOp: "#", Support: OpNative},  // bitwise XOR (SQLite uses #)
	{PGOp: "<<", Support: OpNative}, // left shift
	{PGOp: ">>", Support: OpNative}, // right shift

	// ── String ────────────────────────────────────────────────────────────
	{PGOp: "||", Support: OpNative}, // concatenation
	{PGOp: "~~", Support: OpRewritten, SQLiteForm: "LIKE", Notes: "LIKE operator (internal form)"},
	{PGOp: "~~*", Support: OpRewritten, SQLiteForm: "LOWER(x) LIKE LOWER(y)", Notes: "ILIKE operator (internal form)"},
	{PGOp: "!~~", Support: OpRewritten, SQLiteForm: "NOT LIKE"},
	{PGOp: "!~~*", Support: OpRewritten, SQLiteForm: "NOT LIKE (case-insensitive)"},
	{PGOp: "~", Support: OpNative, Notes: "regex match (SQLite uses REGEXP)"},
	{PGOp: "!~", Support: OpNative, Notes: "regex not-match"},
	{PGOp: "~*", Support: OpEmulated, Notes: "case-insensitive regex — approximate"},
	{PGOp: "!~*", Support: OpEmulated, Notes: "case-insensitive regex not-match — approximate"},

	// ── JSON / JSONB ──────────────────────────────────────────────────────
	{PGOp: "->", Support: OpRewritten, SQLiteForm: "json_extract(x, '$.' || y)"},
	{PGOp: "->>", Support: OpRewritten, SQLiteForm: "json_extract(x, '$.' || y)"},
	{PGOp: "#>", Support: OpRewritten, SQLiteForm: "json_extract(x, path)"},
	{PGOp: "#>>", Support: OpRewritten, SQLiteForm: "json_extract(x, path)"},
	{PGOp: "@>", Support: OpEmulated, Notes: "JSON containment — approximate"},
	{PGOp: "<@", Support: OpEmulated, Notes: "JSON contained-by — approximate"},
	{PGOp: "?", Support: OpUnsupported, Notes: "JSON key exists — returns NULL"},
	{PGOp: "?|", Support: OpUnsupported, Notes: "JSON any-key exists — returns NULL"},
	{PGOp: "?&", Support: OpUnsupported, Notes: "JSON all-keys exist — returns NULL"},
	{PGOp: "||", LeftType: "json", Support: OpRewritten, SQLiteForm: "json_patch(x, y)", Notes: "JSON merge (JSONB context)"},

	// ── Array ─────────────────────────────────────────────────────────────
	{PGOp: "=", LeftType: "array", Support: OpEmulated, Notes: "array equality — approximate via JSON comparison"},
	{PGOp: "@>", LeftType: "array", Support: OpUnsupported, Notes: "array containment — not supported"},
	{PGOp: "<@", LeftType: "array", Support: OpUnsupported},
	{PGOp: "&&", LeftType: "array", Support: OpUnsupported, Notes: "array overlap — not supported"},
	{PGOp: "||", LeftType: "array", Support: OpRewritten, SQLiteForm: "json_patch"},

	// ── Range ─────────────────────────────────────────────────────────────
	{PGOp: "@>", LeftType: "range", Support: OpUnsupported, Notes: "range containment — not supported"},
	{PGOp: "<@", LeftType: "range", Support: OpUnsupported},
	{PGOp: "&&", LeftType: "range", Support: OpUnsupported},
	{PGOp: "-|-", LeftType: "range", Support: OpUnsupported},

	// ── Network ───────────────────────────────────────────────────────────
	{PGOp: "<<", LeftType: "inet", Support: OpUnsupported, Notes: "subnet containment — not supported"},
	{PGOp: ">>", LeftType: "inet", Support: OpUnsupported},
	{PGOp: "<<=", LeftType: "inet", Support: OpUnsupported},
	{PGOp: ">>=", LeftType: "inet", Support: OpUnsupported},

	// ── IS [NOT] DISTINCT FROM ────────────────────────────────────────────
	{PGOp: "IS NOT DISTINCT FROM", Support: OpRewritten, SQLiteForm: "IS", Notes: "SQLite IS is NULL-safe equality"},
	{PGOp: "IS DISTINCT FROM", Support: OpRewritten, SQLiteForm: "IS NOT", Notes: "SQLite IS NOT is NULL-safe inequality"},

	// ── Casting ───────────────────────────────────────────────────────────
	{PGOp: "::", Support: OpRewritten, SQLiteForm: "CAST(expr AS type)"},
}

// ─────────────────────────────────────────────────────────────────────────────
//  Lookup
// ─────────────────────────────────────────────────────────────────────────────

// OperatorLookup maps operator symbols to their info.
var OperatorLookup map[string]*OperatorInfo

func init() {
	OperatorLookup = make(map[string]*OperatorInfo, len(Operators))
	for i := range Operators {
		key := strings.ToUpper(Operators[i].PGOp)
		if _, exists := OperatorLookup[key]; !exists {
			OperatorLookup[key] = &Operators[i]
		}
	}
}

// LookupOperator returns the OperatorInfo for a PostgreSQL operator symbol.
func LookupOperator(op string) *OperatorInfo {
	return OperatorLookup[strings.ToUpper(strings.TrimSpace(op))]
}

// IsNativeOperator returns true if the operator behaves identically in SQLite.
func IsNativeOperator(op string) bool {
	if info := LookupOperator(op); info != nil {
		return info.Support == OpNative
	}
	return false
}
