package postgres

import (
	"strings"
)

// ─────────────────────────────────────────────────────────────────────────────
//  PostgreSQL ↔ SQLite type mapping table
// ─────────────────────────────────────────────────────────────────────────────

// TypeMapping describes how one PostgreSQL type maps to SQLite.
type TypeMapping struct {
	PGName     string   // PostgreSQL type name (canonical)
	PGAliases  []string // alternative names / spellings
	SQLiteName string   // SQLite affinity name
	OID        uint32   // PostgreSQL OID
	Size       int16    // -1 = variable
	Notes      string
}

// TypeMappings is the complete PG → SQLite type mapping table.
var TypeMappings = []TypeMapping{

	// ── Integer types ─────────────────────────────────────────────────────
	{PGName: "smallint", PGAliases: []string{"int2"}, SQLiteName: "INTEGER", OID: 21, Size: 2},
	{PGName: "integer", PGAliases: []string{"int", "int4"}, SQLiteName: "INTEGER", OID: 23, Size: 4},
	{PGName: "bigint", PGAliases: []string{"int8"}, SQLiteName: "INTEGER", OID: 20, Size: 8},
	{PGName: "smallserial", SQLiteName: "INTEGER", OID: 21, Size: 2, Notes: "SERIAL → INTEGER AUTOINCREMENT"},
	{PGName: "serial", PGAliases: []string{"serial4"}, SQLiteName: "INTEGER", OID: 23, Size: 4, Notes: "SERIAL → INTEGER AUTOINCREMENT"},
	{PGName: "bigserial", PGAliases: []string{"serial8"}, SQLiteName: "INTEGER", OID: 20, Size: 8, Notes: "BIGSERIAL → INTEGER AUTOINCREMENT"},

	// ── Floating-point ────────────────────────────────────────────────────
	{PGName: "real", PGAliases: []string{"float4"}, SQLiteName: "REAL", OID: 700, Size: 4},
	{PGName: "double precision", PGAliases: []string{"float8", "float"}, SQLiteName: "REAL", OID: 701, Size: 8},

	// ── Fixed-precision ───────────────────────────────────────────────────
	{PGName: "numeric", PGAliases: []string{"decimal"}, SQLiteName: "NUMERIC", OID: 1700, Size: -1},

	// ── Boolean ───────────────────────────────────────────────────────────
	{PGName: "boolean", PGAliases: []string{"bool"}, SQLiteName: "INTEGER", OID: 16, Size: 1, Notes: "TRUE=1, FALSE=0"},

	// ── Character / text ─────────────────────────────────────────────────
	{PGName: "text", SQLiteName: "TEXT", OID: 25, Size: -1},
	{PGName: "character varying", PGAliases: []string{"varchar"}, SQLiteName: "TEXT", OID: 1043, Size: -1},
	{PGName: "character", PGAliases: []string{"char", "bpchar"}, SQLiteName: "TEXT", OID: 18, Size: -1},
	{PGName: "name", SQLiteName: "TEXT", OID: 19, Size: 64},
	{PGName: "\"char\"", SQLiteName: "TEXT", OID: 18, Size: 1},

	// ── Binary ────────────────────────────────────────────────────────────
	{PGName: "bytea", SQLiteName: "BLOB", OID: 17, Size: -1},

	// ── Date / time ───────────────────────────────────────────────────────
	{PGName: "date", SQLiteName: "TEXT", OID: 1082, Size: 4},
	{PGName: "time", PGAliases: []string{"time without time zone"}, SQLiteName: "TEXT", OID: 1083, Size: 8},
	{PGName: "timetz", PGAliases: []string{"time with time zone"}, SQLiteName: "TEXT", OID: 1266, Size: 12},
	{PGName: "timestamp", PGAliases: []string{"timestamp without time zone"}, SQLiteName: "DATETIME", OID: 1114, Size: 8},
	{PGName: "timestamptz", PGAliases: []string{"timestamp with time zone"}, SQLiteName: "DATETIME", OID: 1184, Size: 8},
	{PGName: "interval", SQLiteName: "TEXT", OID: 1186, Size: 16, Notes: "stored as ISO 8601 text"},

	// ── Network ───────────────────────────────────────────────────────────
	{PGName: "inet", SQLiteName: "TEXT", OID: 869, Size: -1},
	{PGName: "cidr", SQLiteName: "TEXT", OID: 650, Size: -1},
	{PGName: "macaddr", SQLiteName: "TEXT", OID: 829, Size: 6},
	{PGName: "macaddr8", SQLiteName: "TEXT", OID: 774, Size: 8},

	// ── UUID ──────────────────────────────────────────────────────────────
	{PGName: "uuid", SQLiteName: "TEXT", OID: 2950, Size: 16},

	// ── JSON ──────────────────────────────────────────────────────────────
	{PGName: "json", SQLiteName: "TEXT", OID: 114, Size: -1},
	{PGName: "jsonb", SQLiteName: "TEXT", OID: 3802, Size: -1},

	// ── XML ───────────────────────────────────────────────────────────────
	{PGName: "xml", SQLiteName: "TEXT", OID: 142, Size: -1},

	// ── Arrays ────────────────────────────────────────────────────────────
	{PGName: "integer[]", PGAliases: []string{"int[]", "_int4"}, SQLiteName: "TEXT", OID: 1007, Size: -1, Notes: "stored as JSON array"},
	{PGName: "text[]", PGAliases: []string{"_text"}, SQLiteName: "TEXT", OID: 1009, Size: -1, Notes: "stored as JSON array"},
	{PGName: "boolean[]", PGAliases: []string{"_bool"}, SQLiteName: "TEXT", OID: 1000, Size: -1},
	{PGName: "float8[]", PGAliases: []string{"_float8"}, SQLiteName: "TEXT", OID: 1022, Size: -1},

	// ── Geometric / range types (not stored) ─────────────────────────────
	{PGName: "point", SQLiteName: "TEXT", OID: 600, Size: 16},
	{PGName: "line", SQLiteName: "TEXT", OID: 628, Size: 32},
	{PGName: "box", SQLiteName: "TEXT", OID: 603, Size: 32},
	{PGName: "path", SQLiteName: "TEXT", OID: 602, Size: -1},
	{PGName: "polygon", SQLiteName: "TEXT", OID: 604, Size: -1},
	{PGName: "circle", SQLiteName: "TEXT", OID: 718, Size: 24},
	{PGName: "int4range", SQLiteName: "TEXT", OID: 3904, Size: -1},
	{PGName: "int8range", SQLiteName: "TEXT", OID: 3926, Size: -1},
	{PGName: "numrange", SQLiteName: "TEXT", OID: 3906, Size: -1},
	{PGName: "daterange", SQLiteName: "TEXT", OID: 3912, Size: -1},
	{PGName: "tsrange", SQLiteName: "TEXT", OID: 3908, Size: -1},
	{PGName: "tstzrange", SQLiteName: "TEXT", OID: 3910, Size: -1},

	// ── Miscellaneous ─────────────────────────────────────────────────────
	{PGName: "money", SQLiteName: "NUMERIC", OID: 790, Size: 8},
	{PGName: "bit", SQLiteName: "TEXT", OID: 1560, Size: -1},
	{PGName: "bit varying", PGAliases: []string{"varbit"}, SQLiteName: "TEXT", OID: 1562, Size: -1},
	{PGName: "oid", SQLiteName: "INTEGER", OID: 26, Size: 4},
	{PGName: "void", SQLiteName: "TEXT", OID: 2278, Size: 4},
	{PGName: "unknown", SQLiteName: "TEXT", OID: 705, Size: -1},
	{PGName: "tsvector", SQLiteName: "TEXT", OID: 3614, Size: -1},
	{PGName: "tsquery", SQLiteName: "TEXT", OID: 3615, Size: -1},
}

// ─────────────────────────────────────────────────────────────────────────────
//  Lookup maps
// ─────────────────────────────────────────────────────────────────────────────

// TypeByPGName provides O(1) lookup by PostgreSQL type name (all aliases
// resolved to canonical name).
var TypeByPGName map[string]*TypeMapping

// TypeByOID provides O(1) lookup by PostgreSQL OID.
var TypeByOID map[uint32]*TypeMapping

func init() {
	TypeByPGName = make(map[string]*TypeMapping, len(TypeMappings)*3)
	TypeByOID = make(map[uint32]*TypeMapping, len(TypeMappings))

	for i := range TypeMappings {
		m := &TypeMappings[i]
		TypeByPGName[strings.ToLower(m.PGName)] = m
		TypeByOID[m.OID] = m
		for _, alias := range m.PGAliases {
			TypeByPGName[strings.ToLower(alias)] = m
		}
	}
}

// LookupByName returns the TypeMapping for a PostgreSQL type name
// (case-insensitive, aliases resolved).  Returns nil if unknown.
func LookupByName(name string) *TypeMapping {
	return TypeByPGName[strings.ToLower(strings.TrimSpace(name))]
}

// LookupByOID returns the TypeMapping for a PostgreSQL OID.
// Returns nil if unknown.
func LookupByOID(oid uint32) *TypeMapping {
	return TypeByOID[oid]
}

// ToSQLite converts a PostgreSQL type name to the corresponding SQLite
// affinity name and OID.
func ToSQLite(pgName string) (sqliteName string, oid uint32, size int16) {
	if m := LookupByName(pgName); m != nil {
		return m.SQLiteName, m.OID, m.Size
	}
	// Default to TEXT.
	return "TEXT", 25, -1
}
