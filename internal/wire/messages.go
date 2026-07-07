package wire

import (
	"encoding/binary"
	"fmt"
	"math"
	"strconv"
	"time"
)

// ─────────────────────────────────────────────────────────────────────────────
//  Core message writer
// ─────────────────────────────────────────────────────────────────────────────

// writeMessage writes a typed message to the buffered writer and flushes.
//
// Wire format: type(1) | length(4, includes itself) | body(n)
func (s *Session) writeMessage(msgType byte, body []byte) error {
	s.writerMu.Lock()
	defer s.writerMu.Unlock()

	header := [5]byte{msgType}
	binary.BigEndian.PutUint32(header[1:], uint32(4+len(body)))

	if _, err := s.writer.Write(header[:]); err != nil {
		return err
	}
	if len(body) > 0 {
		if _, err := s.writer.Write(body); err != nil {
			return err
		}
	}
	return s.writer.Flush()
}

// writeMessages writes multiple messages in a single flush (pipelining).
func (s *Session) writeMessages(msgs []typedMessage) error {
	s.writerMu.Lock()
	defer s.writerMu.Unlock()

	for _, m := range msgs {
		header := [5]byte{m.typ}
		binary.BigEndian.PutUint32(header[1:], uint32(4+len(m.body)))
		if _, err := s.writer.Write(header[:]); err != nil {
			return err
		}
		if len(m.body) > 0 {
			if _, err := s.writer.Write(m.body); err != nil {
				return err
			}
		}
	}
	return s.writer.Flush()
}

type typedMessage struct {
	typ  byte
	body []byte
}

// ─────────────────────────────────────────────────────────────────────────────
//  RowDescription  ('T')
// ─────────────────────────────────────────────────────────────────────────────

// sendRowDescription sends a RowDescription message describing the columns
// of a query result set.
//
// Wire format:
//
//	'T' | int32(len) | int16(numCols) |
//	for each col:
//	  name\0 | int32(tableOID) | int16(colAttrNum) |
//	  int32(typeOID) | int16(typeSize) | int32(typeMod) | int16(format)
func (s *Session) sendRowDescription(cols []ColumnDesc) error {
	buf := make([]byte, 0, 32*len(cols))
	buf = appendInt16(buf, int16(len(cols)))

	for _, col := range cols {
		buf = append(buf, []byte(col.Name)...)
		buf = append(buf, 0x00) // NUL terminator
		buf = appendInt32(buf, int32(col.TableOID))
		buf = appendInt16(buf, col.AttrNum)
		buf = appendInt32(buf, int32(col.TypeOID))
		buf = appendInt16(buf, col.TypeSize)
		buf = appendInt32(buf, col.TypeMod)
		buf = appendInt16(buf, col.Format) // 0=text
	}
	return s.writeMessage('T', buf)
}

// ─────────────────────────────────────────────────────────────────────────────
//  DataRow  ('D')
// ─────────────────────────────────────────────────────────────────────────────

// sendDataRow sends a single DataRow message.
//
// Wire format:
//
//	'D' | int32(len) | int16(numCols) |
//	for each col: int32(colLen=-1 for NULL, >=0 otherwise) | data
func (s *Session) sendDataRow(row []interface{}) error {
	buf := make([]byte, 0, 64)
	buf = appendInt16(buf, int16(len(row)))

	for _, val := range row {
		if val == nil {
			buf = appendInt32(buf, -1) // NULL
			continue
		}
		text := valueToText(val)
		buf = appendInt32(buf, int32(len(text)))
		buf = append(buf, []byte(text)...)
	}
	return s.writeMessage('D', buf)
}

// ─────────────────────────────────────────────────────────────────────────────
//  CommandComplete  ('C')
// ─────────────────────────────────────────────────────────────────────────────

// sendCommandComplete sends a CommandComplete message.
//
// Wire format: 'C' | int32(len) | tag\0
//
// Example tags: "SELECT 5", "INSERT 0 1", "UPDATE 3", "DELETE 2",
//
//	"CREATE TABLE", "BEGIN", "COMMIT", "ROLLBACK"
func (s *Session) sendCommandComplete(tag string) error {
	body := append([]byte(tag), 0x00)
	return s.writeMessage('C', body)
}

// ─────────────────────────────────────────────────────────────────────────────
//  EmptyQueryResponse  ('I')
// ─────────────────────────────────────────────────────────────────────────────

// sendEmptyQueryResponse is sent when the client sends an empty query string.
func (s *Session) sendEmptyQueryResponse() error {
	return s.writeMessage('I', nil)
}

// ─────────────────────────────────────────────────────────────────────────────
//  NoData  ('n')
// ─────────────────────────────────────────────────────────────────────────────

// sendNoData is sent in response to a Describe message when there are no
// columns to describe (e.g. INSERT/UPDATE/DELETE statements).
func (s *Session) sendNoData() error {
	return s.writeMessage('n', nil)
}

// ─────────────────────────────────────────────────────────────────────────────
//  ParseComplete  ('1')
// ─────────────────────────────────────────────────────────────────────────────

// sendParseComplete signals that a Parse message was processed successfully.
func (s *Session) sendParseComplete() error {
	return s.writeMessage('1', nil)
}

// ─────────────────────────────────────────────────────────────────────────────
//  BindComplete  ('2')
// ─────────────────────────────────────────────────────────────────────────────

// sendBindComplete signals that a Bind message was processed successfully.
func (s *Session) sendBindComplete() error {
	return s.writeMessage('2', nil)
}

// ─────────────────────────────────────────────────────────────────────────────
//  CloseComplete  ('3')
// ─────────────────────────────────────────────────────────────────────────────

// sendCloseComplete signals that a Close message was processed successfully.
func (s *Session) sendCloseComplete() error {
	return s.writeMessage('3', nil)
}

// ─────────────────────────────────────────────────────────────────────────────
//  PortalSuspended  ('s')
// ─────────────────────────────────────────────────────────────────────────────

// sendPortalSuspended is sent when Execute reaches its maxRows limit.
func (s *Session) sendPortalSuspended() error {
	return s.writeMessage('s', nil)
}

// ─────────────────────────────────────────────────────────────────────────────
//  CopyInResponse / CopyOutResponse  ('G' / 'H')
// ─────────────────────────────────────────────────────────────────────────────

// sendCopyInResponse tells the client we are ready to receive COPY data.
func (s *Session) sendCopyInResponse(isText bool, numCols int) error {
	buf := make([]byte, 3+2*numCols)
	if isText {
		buf[0] = 0 // overall format: text
	} else {
		buf[0] = 1 // binary
	}
	binary.BigEndian.PutUint16(buf[1:], uint16(numCols))
	// Column format codes (all 0 = text).
	return s.writeMessage('G', buf)
}

// sendCopyOutResponse tells the client we are about to send COPY data.
func (s *Session) sendCopyOutResponse(isText bool, numCols int) error {
	buf := make([]byte, 3+2*numCols)
	if isText {
		buf[0] = 0
	} else {
		buf[0] = 1
	}
	binary.BigEndian.PutUint16(buf[1:], uint16(numCols))
	return s.writeMessage('H', buf)
}

// sendCopyData sends a chunk of COPY data.
func (s *Session) sendCopyData(data []byte) error {
	return s.writeMessage('d', data)
}

// sendCopyDone signals the end of COPY data from the server.
func (s *Session) sendCopyDone() error {
	return s.writeMessage('c', nil)
}

// ─────────────────────────────────────────────────────────────────────────────
//  ParameterDescription  ('t')
// ─────────────────────────────────────────────────────────────────────────────

// sendParameterDescription describes the types of the parameters in a
// prepared statement.  Sent in response to a Describe(Statement) message.
//
// Wire format: 't' | int32(len) | int16(numParams) | [int32 OID…]
func (s *Session) sendParameterDescription(oids []uint32) error {
	buf := make([]byte, 2+4*len(oids))
	binary.BigEndian.PutUint16(buf[0:], uint16(len(oids)))
	for i, oid := range oids {
		binary.BigEndian.PutUint32(buf[2+4*i:], oid)
	}
	return s.writeMessage('t', buf)
}

// ─────────────────────────────────────────────────────────────────────────────
//  NoticeResponse  ('N')
// ─────────────────────────────────────────────────────────────────────────────

// sendNotice sends a NoticeResponse (non-fatal informational message).
func (s *Session) sendNotice(msg string) error {
	buf := encodeNoticeFields("NOTICE", "00000", msg)
	return s.writeMessage('N', buf)
}

// ─────────────────────────────────────────────────────────────────────────────
//  Value serialisation helpers
// ─────────────────────────────────────────────────────────────────────────────

// valueToText converts a Go interface{} value to its PostgreSQL text
// representation.  All DataRow fields are sent in text format by default.
func valueToText(v interface{}) string {
	if v == nil {
		return ""
	}
	switch val := v.(type) {
	case bool:
		if val {
			return "t"
		}
		return "f"
	case int:
		return strconv.Itoa(val)
	case int8:
		return strconv.FormatInt(int64(val), 10)
	case int16:
		return strconv.FormatInt(int64(val), 10)
	case int32:
		return strconv.FormatInt(int64(val), 10)
	case int64:
		return strconv.FormatInt(val, 10)
	case uint:
		return strconv.FormatUint(uint64(val), 10)
	case uint8:
		return strconv.FormatUint(uint64(val), 10)
	case uint16:
		return strconv.FormatUint(uint64(val), 10)
	case uint32:
		return strconv.FormatUint(uint64(val), 10)
	case uint64:
		return strconv.FormatUint(val, 10)
	case float32:
		return formatFloat(float64(val), 32)
	case float64:
		return formatFloat(val, 64)
	case string:
		return val
	case []byte:
		// PostgreSQL hex-escape format: \x<hexdata>
		return fmt.Sprintf(`\x%x`, val)
	case time.Time:
		return val.UTC().Format("2006-01-02 15:04:05.999999999")
	default:
		return fmt.Sprintf("%v", v)
	}
}

// formatFloat formats a float with enough precision to round-trip.
func formatFloat(f float64, bitSize int) string {
	if math.IsNaN(f) {
		return "NaN"
	}
	if math.IsInf(f, 1) {
		return "Infinity"
	}
	if math.IsInf(f, -1) {
		return "-Infinity"
	}
	return strconv.FormatFloat(f, 'f', -1, bitSize)
}

// ─────────────────────────────────────────────────────────────────────────────
//  Binary append helpers
// ─────────────────────────────────────────────────────────────────────────────

func appendInt16(b []byte, v int16) []byte {
	return append(b, byte(v>>8), byte(v))
}

func appendInt32(b []byte, v int32) []byte {
	return append(b, byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
}

func appendUint32(b []byte, v uint32) []byte {
	return append(b, byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
}

func appendFloat64(b []byte, v float64) []byte {
	bits := math.Float64bits(v)
	return append(b,
		byte(bits>>56), byte(bits>>48), byte(bits>>40), byte(bits>>32),
		byte(bits>>24), byte(bits>>16), byte(bits>>8), byte(bits))
}
