// Package pgproto contains shared types used by the wire, engine, catalog,
// and pool packages.  It has NO imports from any other internal package,
// which is what breaks the import cycle:
//
//	Before: wire → pool → engine → catalog → wire  ❌
//	After:  everything → pgproto (leaf node)        ✅
package pgproto

import "strings"

// ─────────────────────────────────────────────────────────────────────────────
//  PostgreSQL OID constants
// ─────────────────────────────────────────────────────────────────────────────

const (
	OIDBool        uint32 = 16
	OIDByteA       uint32 = 17
	OIDChar        uint32 = 18
	OIDName        uint32 = 19
	OIDInt8        uint32 = 20 // bigint
	OIDInt2        uint32 = 21 // smallint
	OIDInt2Vector  uint32 = 22
	OIDInt4        uint32 = 23 // integer
	OIDRegProc     uint32 = 24
	OIDText        uint32 = 25
	OIDFloat4      uint32 = 700 // real
	OIDFloat8      uint32 = 701 // double precision
	OIDMoney       uint32 = 790
	OIDVarChar     uint32 = 1043
	OIDDate        uint32 = 1082
	OIDTime        uint32 = 1083
	OIDTimestamp   uint32 = 1114
	OIDTimestampTZ uint32 = 1184
	OIDInterval    uint32 = 1186
	OIDTimeTZ      uint32 = 1266
	OIDNumeric     uint32 = 1700
	OIDUUID        uint32 = 2950
	OIDJSONB       uint32 = 3802
	OIDJSON        uint32 = 114
	OIDXml         uint32 = 142
	OIDVoid        uint32 = 2278
	OIDUnknown     uint32 = 705

	// Array OIDs
	OIDInt4Array   uint32 = 1007
	OIDTextArray   uint32 = 1009
	OIDFloat8Array uint32 = 1022
	OIDBoolArray   uint32 = 1000
)

// ─────────────────────────────────────────────────────────────────────────────
//  ColumnDesc
// ─────────────────────────────────────────────────────────────────────────────

// ColumnDesc holds all metadata needed to build a PostgreSQL RowDescription message.
type ColumnDesc struct {
	Name     string
	TableOID uint32 // 0 if not from a named table
	AttrNum  int16  // attribute number (0 if not a physical column)
	TypeOID  uint32 // PostgreSQL OID of the data type
	TypeSize int16  // size of the type (-1 = variable)
	TypeMod  int32  // type modifier (-1 = none)
	Format   int16  // 0 = text, 1 = binary
}

// ─────────────────────────────────────────────────────────────────────────────
//  QueryResult
// ─────────────────────────────────────────────────────────────────────────────

// QueryResult holds the outcome of executing a SQL statement.
// It is serialised by the wire layer into PostgreSQL protocol messages.
type QueryResult struct {
	// Tag is the command tag, e.g. "SELECT 5", "INSERT 0 1", "UPDATE 3".
	Tag string

	// Columns holds the result-set column descriptors (nil for non-SELECT).
	Columns []ColumnDesc

	// Rows holds the result rows (nil for non-SELECT).
	Rows [][]interface{}

	// RowsAffected is set for INSERT/UPDATE/DELETE.
	RowsAffected int64
}

// IsSelect returns true when the result carries rows.
func (r *QueryResult) IsSelect() bool {
	return len(r.Columns) > 0
}

// ─────────────────────────────────────────────────────────────────────────────
//  SQLite type → PostgreSQL OID mapping
// ─────────────────────────────────────────────────────────────────────────────

// SQLiteTypeToOID converts a SQLite column type name to the most appropriate
// PostgreSQL OID and type size.
func SQLiteTypeToOID(sqliteType string) (oid uint32, size int16) {
	t := strings.ToUpper(strings.TrimSpace(sqliteType))

	switch t {
	case "INTEGER", "INT", "INT4", "SIGNED":
		return OIDInt4, 4
	case "TINYINT", "INT1":
		return OIDInt2, 2
	case "SMALLINT", "INT2":
		return OIDInt2, 2
	case "MEDIUMINT":
		return OIDInt4, 4
	case "BIGINT", "INT8", "UNSIGNED BIG INT":
		return OIDInt8, 8
	case "BOOL", "BOOLEAN":
		return OIDBool, 1
	case "REAL", "FLOAT", "FLOAT4":
		return OIDFloat4, 4
	case "DOUBLE", "DOUBLE PRECISION", "FLOAT8":
		return OIDFloat8, 8
	case "NUMERIC", "DECIMAL", "NUMBER", "DEC":
		return OIDNumeric, -1
	case "TEXT", "CLOB", "MEDIUMTEXT", "LONGTEXT", "TINYTEXT":
		return OIDText, -1
	case "UUID":
		return OIDUUID, 16
	case "BLOB", "NONE", "BINARY", "VARBINARY":
		return OIDByteA, -1
	case "DATE":
		return OIDDate, 4
	case "TIME":
		return OIDTime, 8
	case "TIMETZ", "TIMEWITHTZ":
		return OIDTimeTZ, 12
	case "DATETIME", "TIMESTAMP", "TIMESTAMPTZ":
		return OIDTimestamp, 8
	case "TIMESTAMPWITHTIMEZONE":
		return OIDTimestampTZ, 8
	case "JSON":
		return OIDJSON, -1
	case "JSONB":
		return OIDJSONB, -1
	case "XML":
		return OIDXml, -1
	}

	if strings.HasPrefix(t, "VARCHAR") || strings.HasPrefix(t, "CHARACTER VARYING") {
		return OIDVarChar, -1
	}
	if strings.HasPrefix(t, "CHAR") || strings.HasPrefix(t, "CHARACTER") ||
		strings.HasPrefix(t, "NCHAR") {
		return OIDVarChar, -1
	}
	if strings.HasPrefix(t, "NVARCHAR") || strings.HasPrefix(t, "NATIONAL") {
		return OIDVarChar, -1
	}

	// Fallback: SQLite type affinity rules
	switch sqliteAffinity(t) {
	case "INTEGER":
		return OIDInt8, 8
	case "REAL":
		return OIDFloat8, 8
	case "NUMERIC":
		return OIDNumeric, -1
	case "TEXT":
		return OIDText, -1
	default:
		return OIDByteA, -1
	}
}

// OIDToTypeName returns a human-readable PostgreSQL type name for an OID.
func OIDToTypeName(oid uint32) string {
	switch oid {
	case OIDBool:
		return "boolean"
	case OIDByteA:
		return "bytea"
	case OIDInt2:
		return "smallint"
	case OIDInt4:
		return "integer"
	case OIDInt8:
		return "bigint"
	case OIDFloat4:
		return "real"
	case OIDFloat8:
		return "double precision"
	case OIDNumeric:
		return "numeric"
	case OIDText:
		return "text"
	case OIDVarChar:
		return "character varying"
	case OIDChar:
		return "character"
	case OIDDate:
		return "date"
	case OIDTime:
		return "time without time zone"
	case OIDTimeTZ:
		return "time with time zone"
	case OIDTimestamp:
		return "timestamp without time zone"
	case OIDTimestampTZ:
		return "timestamp with time zone"
	case OIDInterval:
		return "interval"
	case OIDUUID:
		return "uuid"
	case OIDJSON:
		return "json"
	case OIDJSONB:
		return "jsonb"
	case OIDXml:
		return "xml"
	case OIDVoid:
		return "void"
	case OIDUnknown:
		return "unknown"
	default:
		return "text"
	}
}

// decodeParamValue converts a raw parameter byte slice from a Bind message.
func DecodeParamValue(data []byte, _ bool) interface{} {
	return string(data)
}

// sqliteAffinity returns the SQLite type affinity for a given type name.
func sqliteAffinity(typeName string) string {
	t := strings.ToUpper(typeName)
	switch {
	case strings.Contains(t, "INT"):
		return "INTEGER"
	case strings.Contains(t, "CHAR"), strings.Contains(t, "CLOB"), strings.Contains(t, "TEXT"):
		return "TEXT"
	case t == "" || strings.Contains(t, "BLOB"):
		return "BLOB"
	case strings.Contains(t, "REAL"), strings.Contains(t, "FLOA"), strings.Contains(t, "DOUB"):
		return "REAL"
	default:
		return "NUMERIC"
	}
}
