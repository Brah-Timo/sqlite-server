// Package wire — type aliases so all wire code can use the short names.
// Actual definitions live in internal/pgproto to break the import cycle.
package wire

import (
	"github.com/sqlite-server/sqlite-server/internal/pgproto"
)

// Re-export pgproto types as aliases so wire package code keeps working.
type ColumnDesc = pgproto.ColumnDesc
type QueryResult = pgproto.QueryResult

// Re-export OID constants.
const (
	OIDBool        = pgproto.OIDBool
	OIDByteA       = pgproto.OIDByteA
	OIDChar        = pgproto.OIDChar
	OIDName        = pgproto.OIDName
	OIDInt8        = pgproto.OIDInt8
	OIDInt2        = pgproto.OIDInt2
	OIDInt2Vector  = pgproto.OIDInt2Vector
	OIDInt4        = pgproto.OIDInt4
	OIDRegProc     = pgproto.OIDRegProc
	OIDText        = pgproto.OIDText
	OIDFloat4      = pgproto.OIDFloat4
	OIDFloat8      = pgproto.OIDFloat8
	OIDMoney       = pgproto.OIDMoney
	OIDVarChar     = pgproto.OIDVarChar
	OIDDate        = pgproto.OIDDate
	OIDTime        = pgproto.OIDTime
	OIDTimestamp   = pgproto.OIDTimestamp
	OIDTimestampTZ = pgproto.OIDTimestampTZ
	OIDInterval    = pgproto.OIDInterval
	OIDTimeTZ      = pgproto.OIDTimeTZ
	OIDNumeric     = pgproto.OIDNumeric
	OIDUUID        = pgproto.OIDUUID
	OIDJSONB       = pgproto.OIDJSONB
	OIDJSON        = pgproto.OIDJSON
	OIDXml         = pgproto.OIDXml
	OIDVoid        = pgproto.OIDVoid
	OIDUnknown     = pgproto.OIDUnknown
	OIDInt4Array   = pgproto.OIDInt4Array
	OIDTextArray   = pgproto.OIDTextArray
	OIDFloat8Array = pgproto.OIDFloat8Array
	OIDBoolArray   = pgproto.OIDBoolArray
)

// SQLiteTypeToOID delegates to pgproto.
func SQLiteTypeToOID(sqliteType string) (uint32, int16) {
	return pgproto.SQLiteTypeToOID(sqliteType)
}

// OIDToTypeName delegates to pgproto.
func OIDToTypeName(oid uint32) string {
	return pgproto.OIDToTypeName(oid)
}

// decodeParamValue delegates to pgproto.
func decodeParamValue(data []byte, isBinary bool) interface{} {
	return pgproto.DecodeParamValue(data, isBinary)
}
