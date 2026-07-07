package wire

import "encoding/binary"

// ─────────────────────────────────────────────────────────────────────────────
//  ReadyForQuery  ('Z')
// ─────────────────────────────────────────────────────────────────────────────

// sendReadyForQuery sends a ReadyForQuery message indicating the server is
// ready for the next command.
//
// Wire format: 'Z' | int32(5) | txStatusByte
//
// txStatusByte values:
//
//	'I' — idle, not in a transaction
//	'T' — inside a transaction block
//	'E' — in a failed transaction (needs ROLLBACK)
func (s *Session) sendReadyForQuery(status TxStatus) error {
	body := [5]byte{}
	binary.BigEndian.PutUint32(body[0:], 5)
	body[4] = byte(status)
	return s.writeMessage('Z', body[4:5])
}
