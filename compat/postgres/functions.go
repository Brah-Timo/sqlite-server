// Package postgres contains the PostgreSQL compatibility layer.
// It documents every PG built-in function, type, and operator that is handled
// by the rewriter and provides lookup tables used during rewriting and catalog
// virtualisation.
package postgres

import "strings"

// ─────────────────────────────────────────────────────────────────────────────
//  Function compatibility table
// ─────────────────────────────────────────────────────────────────────────────

// FuncSupport describes how a PostgreSQL function is handled.
type FuncSupport int

const (
	// FuncNative — function exists in SQLite with the same name and semantics.
	FuncNative FuncSupport = iota
	// FuncRewritten — function is rewritten to equivalent SQLite SQL by the rewriter.
	FuncRewritten
	// FuncEmulated — approximated (may not be 100% equivalent).
	FuncEmulated
	// FuncUnsupported — not supported; NULL is returned.
	FuncUnsupported
)

// FuncInfo describes a PostgreSQL function's compatibility status.
type FuncInfo struct {
	Name       string
	Support    FuncSupport
	SQLiteForm string // SQLite equivalent (for reference / documentation)
	Notes      string
}

// Functions is the full compatibility table for PostgreSQL built-in functions.
// This table is used for documentation and by tools that inspect compatibility.
var Functions = []FuncInfo{

	// ── Date / time ───────────────────────────────────────────────────────
	{Name: "now", Support: FuncRewritten, SQLiteForm: "datetime('now')", Notes: "returns current UTC timestamp"},
	{Name: "current_timestamp", Support: FuncRewritten, SQLiteForm: "datetime('now')"},
	{Name: "current_date", Support: FuncRewritten, SQLiteForm: "date('now')"},
	{Name: "current_time", Support: FuncRewritten, SQLiteForm: "time('now')"},
	{Name: "clock_timestamp", Support: FuncRewritten, SQLiteForm: "datetime('now')", Notes: "same as now() — no wall clock"},
	{Name: "date_trunc", Support: FuncRewritten, SQLiteForm: "strftime(…)", Notes: "year/month/day/hour/minute/second"},
	{Name: "date_part", Support: FuncRewritten, SQLiteForm: "strftime(…)"},
	{Name: "extract", Support: FuncRewritten, SQLiteForm: "CAST(strftime(…) AS INTEGER)"},
	{Name: "age", Support: FuncEmulated, SQLiteForm: "julianday diff", Notes: "returns days as REAL, not interval"},
	{Name: "to_char", Support: FuncRewritten, SQLiteForm: "strftime(…)", Notes: "subset of PG formats"},
	{Name: "to_date", Support: FuncEmulated, Notes: "format string ignored"},
	{Name: "to_timestamp", Support: FuncRewritten, SQLiteForm: "datetime(x, 'unixepoch')"},
	{Name: "make_date", Support: FuncUnsupported, Notes: "returns NULL"},
	{Name: "make_interval", Support: FuncUnsupported, Notes: "returns NULL"},
	{Name: "timezone", Support: FuncUnsupported, Notes: "SQLite has no timezone support"},
	{Name: "timeofday", Support: FuncRewritten, SQLiteForm: "datetime('now')"},

	// ── String ────────────────────────────────────────────────────────────
	{Name: "length", Support: FuncNative},
	{Name: "char_length", Support: FuncNative, SQLiteForm: "length"},
	{Name: "character_length", Support: FuncNative, SQLiteForm: "length"},
	{Name: "octet_length", Support: FuncNative, SQLiteForm: "length"},
	{Name: "bit_length", Support: FuncEmulated, SQLiteForm: "length(x)*8"},
	{Name: "upper", Support: FuncNative},
	{Name: "lower", Support: FuncNative},
	{Name: "initcap", Support: FuncUnsupported, Notes: "returns input as-is"},
	{Name: "ltrim", Support: FuncNative},
	{Name: "rtrim", Support: FuncNative},
	{Name: "btrim", Support: FuncNative, SQLiteForm: "trim"},
	{Name: "trim", Support: FuncNative},
	{Name: "lpad", Support: FuncRewritten, Notes: "approximate using substr/||"},
	{Name: "rpad", Support: FuncRewritten, Notes: "approximate using substr/||"},
	{Name: "left", Support: FuncNative},
	{Name: "right", Support: FuncNative},
	{Name: "substr", Support: FuncNative},
	{Name: "substring", Support: FuncNative, SQLiteForm: "substr"},
	{Name: "overlay", Support: FuncEmulated, Notes: "approximate"},
	{Name: "position", Support: FuncNative, SQLiteForm: "instr"},
	{Name: "strpos", Support: FuncNative, SQLiteForm: "instr"},
	{Name: "instr", Support: FuncNative},
	{Name: "replace", Support: FuncNative},
	{Name: "translate", Support: FuncUnsupported, Notes: "returns NULL"},
	{Name: "repeat", Support: FuncNative},
	{Name: "concat", Support: FuncNative, SQLiteForm: "||"},
	{Name: "concat_ws", Support: FuncRewritten, Notes: "uses || with separator"},
	{Name: "split_part", Support: FuncUnsupported, Notes: "returns NULL"},
	{Name: "reverse", Support: FuncNative},
	{Name: "md5", Support: FuncEmulated, Notes: "random hex — not real MD5"},
	{Name: "encode", Support: FuncUnsupported, Notes: "returns NULL"},
	{Name: "decode", Support: FuncUnsupported, Notes: "returns NULL"},
	{Name: "format", Support: FuncEmulated, SQLiteForm: "printf"},
	{Name: "printf", Support: FuncNative},
	{Name: "regexp_replace", Support: FuncUnsupported, Notes: "returns input unchanged"},
	{Name: "regexp_match", Support: FuncUnsupported, Notes: "returns NULL"},
	{Name: "regexp_matches", Support: FuncUnsupported, Notes: "returns NULL"},
	{Name: "regexp_split_to_array", Support: FuncUnsupported, Notes: "returns NULL"},
	{Name: "regexp_split_to_table", Support: FuncUnsupported, Notes: "returns NULL"},
	{Name: "quote_ident", Support: FuncUnsupported, Notes: "returns input unchanged"},
	{Name: "quote_literal", Support: FuncUnsupported, Notes: "single-quotes the value"},
	{Name: "quote_nullable", Support: FuncUnsupported, Notes: "returns NULL or single-quoted value"},
	{Name: "to_hex", Support: FuncNative, SQLiteForm: "hex"},
	{Name: "hex", Support: FuncNative},
	{Name: "ascii", Support: FuncNative},
	{Name: "chr", Support: FuncNative},
	{Name: "pg_client_encoding", Support: FuncRewritten, SQLiteForm: "'UTF8'"},

	// ── Math ──────────────────────────────────────────────────────────────
	{Name: "abs", Support: FuncNative},
	{Name: "cbrt", Support: FuncEmulated, SQLiteForm: "pow(x, 1.0/3)"},
	{Name: "ceil", Support: FuncNative},
	{Name: "ceiling", Support: FuncNative, SQLiteForm: "ceil"},
	{Name: "floor", Support: FuncNative},
	{Name: "round", Support: FuncNative},
	{Name: "trunc", Support: FuncNative},
	{Name: "sign", Support: FuncNative},
	{Name: "mod", Support: FuncNative},
	{Name: "div", Support: FuncRewritten, SQLiteForm: "CAST(x AS INTEGER) / CAST(y AS INTEGER)"},
	{Name: "exp", Support: FuncNative},
	{Name: "ln", Support: FuncNative},
	{Name: "log", Support: FuncNative},
	{Name: "log10", Support: FuncNative},
	{Name: "log2", Support: FuncEmulated, SQLiteForm: "log(x)/log(2)"},
	{Name: "power", Support: FuncNative},
	{Name: "pow", Support: FuncNative},
	{Name: "sqrt", Support: FuncNative},
	{Name: "pi", Support: FuncNative},
	{Name: "degrees", Support: FuncNative},
	{Name: "radians", Support: FuncNative},
	{Name: "sin", Support: FuncNative},
	{Name: "cos", Support: FuncNative},
	{Name: "tan", Support: FuncNative},
	{Name: "asin", Support: FuncNative},
	{Name: "acos", Support: FuncNative},
	{Name: "atan", Support: FuncNative},
	{Name: "atan2", Support: FuncNative},
	{Name: "sinh", Support: FuncNative},
	{Name: "cosh", Support: FuncNative},
	{Name: "tanh", Support: FuncNative},
	{Name: "random", Support: FuncRewritten, SQLiteForm: "(RANDOM()/9223372036854775808.0/2.0+0.5)"},
	{Name: "setseed", Support: FuncUnsupported, Notes: "SQLite PRNG is not seedable"},
	{Name: "greatest", Support: FuncNative},
	{Name: "least", Support: FuncNative},
	{Name: "coalesce", Support: FuncNative},
	{Name: "nullif", Support: FuncNative},

	// ── Aggregate ─────────────────────────────────────────────────────────
	{Name: "count", Support: FuncNative},
	{Name: "sum", Support: FuncNative},
	{Name: "avg", Support: FuncNative},
	{Name: "min", Support: FuncNative},
	{Name: "max", Support: FuncNative},
	{Name: "stddev", Support: FuncNative},
	{Name: "stddev_pop", Support: FuncNative},
	{Name: "stddev_samp", Support: FuncNative},
	{Name: "variance", Support: FuncNative},
	{Name: "var_pop", Support: FuncNative},
	{Name: "var_samp", Support: FuncNative},
	{Name: "string_agg", Support: FuncRewritten, SQLiteForm: "GROUP_CONCAT(x, sep)"},
	{Name: "array_agg", Support: FuncRewritten, SQLiteForm: "JSON_GROUP_ARRAY(x)"},
	{Name: "bool_and", Support: FuncRewritten, SQLiteForm: "MIN(x)"},
	{Name: "bool_or", Support: FuncRewritten, SQLiteForm: "MAX(x)"},
	{Name: "every", Support: FuncRewritten, SQLiteForm: "MIN(x)"},
	{Name: "json_agg", Support: FuncRewritten, SQLiteForm: "JSON_GROUP_ARRAY(x)"},
	{Name: "jsonb_agg", Support: FuncRewritten, SQLiteForm: "JSON_GROUP_ARRAY(x)"},
	{Name: "json_object_agg", Support: FuncUnsupported, Notes: "returns NULL"},
	{Name: "xmlagg", Support: FuncUnsupported, Notes: "returns NULL"},

	// ── Window ────────────────────────────────────────────────────────────
	{Name: "row_number", Support: FuncNative},
	{Name: "rank", Support: FuncNative},
	{Name: "dense_rank", Support: FuncNative},
	{Name: "percent_rank", Support: FuncNative},
	{Name: "cume_dist", Support: FuncNative},
	{Name: "ntile", Support: FuncNative},
	{Name: "lag", Support: FuncNative},
	{Name: "lead", Support: FuncNative},
	{Name: "first_value", Support: FuncNative},
	{Name: "last_value", Support: FuncNative},
	{Name: "nth_value", Support: FuncNative},

	// ── JSON ──────────────────────────────────────────────────────────────
	{Name: "json_build_object", Support: FuncRewritten, SQLiteForm: "JSON_OBJECT(k, v, …)"},
	{Name: "jsonb_build_object", Support: FuncRewritten, SQLiteForm: "JSON_OBJECT(k, v, …)"},
	{Name: "json_build_array", Support: FuncRewritten, SQLiteForm: "JSON_ARRAY(…)"},
	{Name: "json_object", Support: FuncNative, SQLiteForm: "JSON_OBJECT"},
	{Name: "json_array", Support: FuncNative, SQLiteForm: "JSON_ARRAY"},
	{Name: "json_extract", Support: FuncNative},
	{Name: "json_extract_path", Support: FuncRewritten, SQLiteForm: "json_extract"},
	{Name: "jsonb_extract_path", Support: FuncRewritten, SQLiteForm: "json_extract"},
	{Name: "json_typeof", Support: FuncNative, SQLiteForm: "JSON_TYPE"},
	{Name: "json_array_length", Support: FuncNative},
	{Name: "json_each", Support: FuncUnsupported, Notes: "no table-valued function equivalent"},
	{Name: "jsonb_each", Support: FuncUnsupported},
	{Name: "json_keys", Support: FuncNative, SQLiteForm: "JSON_EACH.key"},

	// ── Type conversion ───────────────────────────────────────────────────
	{Name: "to_number", Support: FuncRewritten, SQLiteForm: "CAST(x AS REAL)"},
	{Name: "to_char", Support: FuncRewritten, SQLiteForm: "strftime/printf"},
	{Name: "to_date", Support: FuncEmulated},
	{Name: "to_timestamp", Support: FuncRewritten, SQLiteForm: "datetime(x,'unixepoch')"},
	{Name: "text", Support: FuncRewritten, SQLiteForm: "CAST(x AS TEXT)"},
	{Name: "integer", Support: FuncRewritten, SQLiteForm: "CAST(x AS INTEGER)"},
	{Name: "float", Support: FuncRewritten, SQLiteForm: "CAST(x AS REAL)"},
	{Name: "numeric", Support: FuncRewritten, SQLiteForm: "CAST(x AS NUMERIC)"},
	{Name: "boolean", Support: FuncRewritten, SQLiteForm: "CAST(x AS INTEGER)"},

	// ── System / meta ─────────────────────────────────────────────────────
	{Name: "version", Support: FuncRewritten, SQLiteForm: "'PostgreSQL 14.5 (sqlite-server)'"},
	{Name: "pg_client_encoding", Support: FuncRewritten, SQLiteForm: "'UTF8'"},
	{Name: "pg_server_version", Support: FuncRewritten},
	{Name: "pg_get_userbyid", Support: FuncRewritten, SQLiteForm: "'postgres'"},
	{Name: "current_user", Support: FuncRewritten, SQLiteForm: "'postgres'"},
	{Name: "current_database", Support: FuncRewritten, SQLiteForm: "'main'"},
	{Name: "current_schema", Support: FuncRewritten, SQLiteForm: "'public'"},
	{Name: "session_user", Support: FuncRewritten, SQLiteForm: "'postgres'"},
	{Name: "user", Support: FuncRewritten, SQLiteForm: "'postgres'"},
	{Name: "inet_client_addr", Support: FuncUnsupported, Notes: "returns NULL"},
	{Name: "inet_server_addr", Support: FuncUnsupported, Notes: "returns NULL"},
	{Name: "pg_backend_pid", Support: FuncRewritten, SQLiteForm: "0"},
	{Name: "txid_current", Support: FuncRewritten, SQLiteForm: "0"},
	{Name: "pg_advisory_lock", Support: FuncUnsupported},
	{Name: "pg_advisory_unlock", Support: FuncUnsupported},
}

// FuncLookup provides O(1) lookup by lowercase name.
var FuncLookup map[string]*FuncInfo

func init() {
	FuncLookup = make(map[string]*FuncInfo, len(Functions))
	for i := range Functions {
		FuncLookup[strings.ToLower(Functions[i].Name)] = &Functions[i]
	}
}

// Lookup returns the FuncInfo for a function name (case-insensitive).
// Returns nil if the function is not in the table.
func Lookup(name string) *FuncInfo {
	return FuncLookup[strings.ToLower(name)]
}

// IsNative returns true if the function is natively supported by SQLite.
func IsNative(name string) bool {
	if info := Lookup(name); info != nil {
		return info.Support == FuncNative
	}
	return false
}
