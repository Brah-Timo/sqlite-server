package wire

import (
	"fmt"
)

// ─────────────────────────────────────────────────────────────────────────────
//  PGError — PostgreSQL error representation
// ─────────────────────────────────────────────────────────────────────────────

// PGError is a structured PostgreSQL error.  It maps directly to the fields
// of an ErrorResponse message ('E').
type PGError struct {
	Severity         string // "ERROR", "FATAL", "PANIC", "WARNING", "NOTICE", "DEBUG", "INFO", "LOG"
	SeverityInternal string // Same, non-localised (field 'V')
	Code             string // 5-character SQLSTATE code
	Message          string // Primary message ('M')
	Detail           string // Optional detail ('D')
	Hint             string // Optional hint ('H')
	Position         string // Cursor position in the query ('P')
	InternalPosition string // Internal position ('p')
	InternalQuery    string // Internal query ('q')
	Where            string // Context information ('W')
	SchemaName       string // Schema name ('s')
	TableName        string // Table name ('t')
	ColumnName       string // Column name ('c')
	DataTypeName     string // Data type name ('d')
	ConstraintName   string // Constraint name ('n')
	File             string // Source file ('F')
	Line             string // Source line ('L')
	Routine          string // Source routine ('R')
}

// Error satisfies the error interface.
func (e *PGError) Error() string {
	if e.Detail != "" {
		return fmt.Sprintf("%s [%s]: %s — %s", e.Severity, e.Code, e.Message, e.Detail)
	}
	return fmt.Sprintf("%s [%s]: %s", e.Severity, e.Code, e.Message)
}

// newPGError constructs a basic PGError.
func newPGError(code, routine, message string) *PGError {
	return &PGError{
		Severity:         "ERROR",
		SeverityInternal: "ERROR",
		Code:             code,
		Message:          message,
		Routine:          routine,
	}
}

// newFatalPGError constructs a FATAL PGError (terminates the session).
func newFatalPGError(code, routine, message string) *PGError {
	return &PGError{
		Severity:         "FATAL",
		SeverityInternal: "FATAL",
		Code:             code,
		Message:          message,
		Routine:          routine,
	}
}

// ─────────────────────────────────────────────────────────────────────────────
//  ErrorResponse  ('E')
// ─────────────────────────────────────────────────────────────────────────────

// sendError sends an ErrorResponse message for the given error.
// If the error is not already a *PGError it is wrapped with a generic code.
func (s *Session) sendError(err error) error {
	if err == nil {
		return nil
	}
	pgErr := toPGError(err)
	body := encodeErrorFields(pgErr)
	return s.writeMessage('E', body)
}

// sendFatalErrorMsg constructs and sends a FATAL ErrorResponse, then returns
// the error so callers can propagate it.
func (s *Session) sendFatalErrorMsg(code, routine, message string) error {
	err := newFatalPGError(code, routine, message)
	_ = s.sendError(err)
	return err
}

// toPGError converts any error to a *PGError.
func toPGError(err error) *PGError {
	if err == nil {
		return nil
	}
	if pg, ok := err.(*PGError); ok {
		return pg
	}
	return &PGError{
		Severity:         "ERROR",
		SeverityInternal: "ERROR",
		Code:             "XX000", // internal_error
		Message:          err.Error(),
		Routine:          "execute",
	}
}

// ─────────────────────────────────────────────────────────────────────────────
//  ErrorResponse / NoticeResponse encoding
// ─────────────────────────────────────────────────────────────────────────────

// encodeErrorFields serialises a PGError into the wire-format field list
// used by both ErrorResponse and NoticeResponse.
//
// Wire format for each non-empty field:
//
//	byte(fieldCode) | string\0
//
// Terminated by a single NUL byte.
func encodeErrorFields(e *PGError) []byte {
	var buf []byte

	write := func(code byte, value string) {
		if value == "" {
			return
		}
		buf = append(buf, code)
		buf = append(buf, []byte(value)...)
		buf = append(buf, 0x00)
	}

	// Field codes defined by the PostgreSQL wire protocol specification.
	write('S', e.Severity)
	write('V', e.SeverityInternal)
	write('C', e.Code)
	write('M', e.Message)
	write('D', e.Detail)
	write('H', e.Hint)
	write('P', e.Position)
	write('p', e.InternalPosition)
	write('q', e.InternalQuery)
	write('W', e.Where)
	write('s', e.SchemaName)
	write('t', e.TableName)
	write('c', e.ColumnName)
	write('d', e.DataTypeName)
	write('n', e.ConstraintName)
	write('F', e.File)
	write('L', e.Line)
	write('R', e.Routine)

	buf = append(buf, 0x00) // message terminator
	return buf
}

// encodeNoticeFields serialises a notice.
func encodeNoticeFields(severity, code, message string) []byte {
	pg := &PGError{
		Severity:         severity,
		SeverityInternal: severity,
		Code:             code,
		Message:          message,
	}
	return encodeErrorFields(pg)
}
