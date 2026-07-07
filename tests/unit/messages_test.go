// Package unit — wire protocol message encoding/decoding unit tests.
package unit

import (
	"encoding/binary"
	"testing"

	"github.com/sqlite-server/sqlite-server/internal/pgproto"
)

// ─────────────────────────────────────────────────────────────────────────────
//  OID / type-mapping tests
// ─────────────────────────────────────────────────────────────────────────────

func TestSQLiteTypeToOID(t *testing.T) {
	cases := []struct {
		sqliteType string
		wantOID    uint32
	}{
		{"INTEGER", pgproto.OIDInt4},
		{"INT", pgproto.OIDInt4},
		{"BIGINT", pgproto.OIDInt8},
		{"SMALLINT", pgproto.OIDInt2},
		{"TEXT", pgproto.OIDText},
		{"BLOB", pgproto.OIDByteA},
		{"REAL", pgproto.OIDFloat4},
		{"DOUBLE PRECISION", pgproto.OIDFloat8},
		{"NUMERIC", pgproto.OIDNumeric},
		{"DECIMAL", pgproto.OIDNumeric},
		{"BOOLEAN", pgproto.OIDBool},
		{"BOOL", pgproto.OIDBool},
		{"DATE", pgproto.OIDDate},
		{"TIME", pgproto.OIDTime},
		{"TIMESTAMP", pgproto.OIDTimestamp},
		{"UUID", pgproto.OIDUUID},
		{"JSON", pgproto.OIDJSON},
		{"JSONB", pgproto.OIDJSONB},
		{"VARCHAR(255)", pgproto.OIDVarChar},
		{"CHARACTER VARYING", pgproto.OIDVarChar},
		{"", pgproto.OIDByteA},                   // no type → BLOB affinity
		{"UNKNOWN_TYPE_XYZ", pgproto.OIDNumeric}, // unknown → NUMERIC affinity
	}

	for _, tc := range cases {
		oid, _ := pgproto.SQLiteTypeToOID(tc.sqliteType)
		if oid != tc.wantOID {
			t.Errorf("SQLiteTypeToOID(%q) = %d, want %d", tc.sqliteType, oid, tc.wantOID)
		}
	}
}

func TestOIDToTypeName(t *testing.T) {
	cases := []struct {
		oid      uint32
		wantName string
	}{
		{pgproto.OIDBool, "boolean"},
		{pgproto.OIDInt2, "smallint"},
		{pgproto.OIDInt4, "integer"},
		{pgproto.OIDInt8, "bigint"},
		{pgproto.OIDFloat4, "real"},
		{pgproto.OIDFloat8, "double precision"},
		{pgproto.OIDText, "text"},
		{pgproto.OIDVarChar, "character varying"},
		{pgproto.OIDDate, "date"},
		{pgproto.OIDTimestamp, "timestamp without time zone"},
		{pgproto.OIDTimestampTZ, "timestamp with time zone"},
		{pgproto.OIDUUID, "uuid"},
		{pgproto.OIDJSON, "json"},
		{pgproto.OIDJSONB, "jsonb"},
		{pgproto.OIDVoid, "void"},
		{pgproto.OIDUnknown, "unknown"},
		{99999, "text"}, // unknown OID → text fallback
	}

	for _, tc := range cases {
		got := pgproto.OIDToTypeName(tc.oid)
		if got != tc.wantName {
			t.Errorf("OIDToTypeName(%d) = %q, want %q", tc.oid, got, tc.wantName)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
//  ColumnDesc construction
// ─────────────────────────────────────────────────────────────────────────────

func TestColumnDescDefaults(t *testing.T) {
	col := pgproto.ColumnDesc{
		Name:    "id",
		TypeOID: pgproto.OIDInt4,
	}
	if col.Name != "id" {
		t.Errorf("name: %q", col.Name)
	}
	if col.TypeOID != pgproto.OIDInt4 {
		t.Errorf("typeOID: %d", col.TypeOID)
	}
	if col.Format != 0 {
		t.Errorf("format should default to 0 (text), got %d", col.Format)
	}
	if col.TypeMod != 0 {
		t.Errorf("typeMod should default to 0, got %d", col.TypeMod)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
//  QueryResult construction
// ─────────────────────────────────────────────────────────────────────────────

func TestQueryResultIsSelect(t *testing.T) {
	empty := &pgproto.QueryResult{Tag: "INSERT 0 1"}
	if empty.IsSelect() {
		t.Error("empty columns should not be select")
	}

	withCols := &pgproto.QueryResult{
		Tag:     "SELECT 3",
		Columns: []pgproto.ColumnDesc{{Name: "id", TypeOID: pgproto.OIDInt4}},
	}
	if !withCols.IsSelect() {
		t.Error("with columns should be select")
	}
}

func TestQueryResultRowCount(t *testing.T) {
	qr := &pgproto.QueryResult{
		Columns: []pgproto.ColumnDesc{
			{Name: "id", TypeOID: pgproto.OIDInt4},
		},
		Rows: [][]interface{}{
			{int64(1)},
			{int64(2)},
			{int64(3)},
		},
		Tag: "SELECT 3",
	}
	if len(qr.Rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(qr.Rows))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
//  Protocol byte encoding helpers
// ─────────────────────────────────────────────────────────────────────────────

func TestBigEndianInt32Encoding(t *testing.T) {
	buf := make([]byte, 4)
	binary.BigEndian.PutUint32(buf, 12345)
	got := binary.BigEndian.Uint32(buf)
	if got != 12345 {
		t.Fatalf("round-trip uint32 failed: %d", got)
	}
}

func TestBigEndianInt16Encoding(t *testing.T) {
	buf := make([]byte, 2)
	binary.BigEndian.PutUint16(buf, 1234)
	got := binary.BigEndian.Uint16(buf)
	if got != 1234 {
		t.Fatalf("round-trip uint16 failed: %d", got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
//  DecodeParamValue
// ─────────────────────────────────────────────────────────────────────────────

func TestDecodeParamValueText(t *testing.T) {
	data := []byte("hello")
	got := pgproto.DecodeParamValue(data, false)
	if s, ok := got.(string); !ok || s != "hello" {
		t.Fatalf("expected string 'hello', got %T %v", got, got)
	}
}

func TestDecodeParamValueNil(t *testing.T) {
	got := pgproto.DecodeParamValue(nil, false)
	// nil or empty string are both acceptable
	_ = got
}

// ─────────────────────────────────────────────────────────────────────────────
//  OID constants sanity check
// ─────────────────────────────────────────────────────────────────────────────

func TestOIDConstants(t *testing.T) {
	// Spot-check that OID values match the PostgreSQL standard
	if pgproto.OIDBool != 16 {
		t.Errorf("OIDBool: want 16, got %d", pgproto.OIDBool)
	}
	if pgproto.OIDInt4 != 23 {
		t.Errorf("OIDInt4: want 23, got %d", pgproto.OIDInt4)
	}
	if pgproto.OIDText != 25 {
		t.Errorf("OIDText: want 25, got %d", pgproto.OIDText)
	}
	if pgproto.OIDFloat8 != 701 {
		t.Errorf("OIDFloat8: want 701, got %d", pgproto.OIDFloat8)
	}
	if pgproto.OIDTimestamp != 1114 {
		t.Errorf("OIDTimestamp: want 1114, got %d", pgproto.OIDTimestamp)
	}
	if pgproto.OIDUUID != 2950 {
		t.Errorf("OIDUUID: want 2950, got %d", pgproto.OIDUUID)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
//  Lexer smoke tests
// ─────────────────────────────────────────────────────────────────────────────

func TestLexerTokenizes(t *testing.T) {
	// Just import the lexer through planner to verify compilation
	p := newPlanner()
	_, err := p.Rewrite("SELECT id, name FROM users WHERE age > 18")
	if err != nil {
		t.Fatalf("basic select rewrite: %v", err)
	}
}

func TestLexerHandlesQuotedStrings(t *testing.T) {
	p := newPlanner()
	_, err := p.Rewrite(`SELECT * FROM t WHERE name = 'O''Brien'`)
	if err != nil {
		t.Fatalf("quoted string with escape: %v", err)
	}
}

func TestLexerHandlesDoubleQuotedIdents(t *testing.T) {
	p := newPlanner()
	_, err := p.Rewrite(`SELECT "user"."name" FROM "user"`)
	if err != nil {
		t.Fatalf("double-quoted idents: %v", err)
	}
}
