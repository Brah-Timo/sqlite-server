package errors

import (
	"fmt"
	"strings"
)

// ─────────────────────────────────────────────────────────────────────────────
//  PGError
// ─────────────────────────────────────────────────────────────────────────────

// PGError is a structured PostgreSQL error that can be sent over the wire.
// It implements the standard error interface so it can be used throughout
// the codebase as a regular Go error.
type PGError struct {
	Severity         string
	SeverityInternal string
	Code             string // 5-char SQLSTATE code
	Message          string
	Detail           string
	Hint             string
	Position         string
	InternalPosition string
	InternalQuery    string
	Where            string
	SchemaName       string
	TableName        string
	ColumnName       string
	DataTypeName     string
	ConstraintName   string
	File             string
	Line             string
	Routine          string
}

// Error satisfies the error interface.
func (e *PGError) Error() string {
	if e.Detail != "" {
		return fmt.Sprintf("%s [%s]: %s — %s", e.Severity, e.Code, e.Message, e.Detail)
	}
	return fmt.Sprintf("%s [%s]: %s", e.Severity, e.Code, e.Message)
}

// New creates a new ERROR-severity PGError.
func New(code, message string) *PGError {
	return &PGError{
		Severity:         "ERROR",
		SeverityInternal: "ERROR",
		Code:             code,
		Message:          message,
	}
}

// Fatal creates a FATAL-severity PGError (terminates the session).
func Fatal(code, message string) *PGError {
	return &PGError{
		Severity:         "FATAL",
		SeverityInternal: "FATAL",
		Code:             code,
		Message:          message,
	}
}

// ─────────────────────────────────────────────────────────────────────────────
//  SQLite → PostgreSQL error translation
// ─────────────────────────────────────────────────────────────────────────────

// TranslateSQLiteError converts a raw SQLite driver error (or any Go error)
// into a *PGError with the appropriate SQLSTATE code.
//
// The function inspects the error message string because modernc.org/sqlite
// wraps errors as formatted strings rather than typed error values.
func TranslateSQLiteError(err error) error {
	if err == nil {
		return nil
	}
	// If it's already a *PGError, pass it through.
	if pg, ok := err.(*PGError); ok {
		return pg
	}

	msg := err.Error()
	lower := strings.ToLower(msg)

	// ── Constraint violations ──────────────────────────────────────────────
	if strings.Contains(lower, "unique constraint failed") {
		colRef := extractConstraintColumn(msg, "unique constraint failed: ")
		pg := &PGError{
			Severity:         "ERROR",
			SeverityInternal: "ERROR",
			Code:             CodeUniqueViolation,
			Message:          "duplicate key value violates unique constraint",
			Detail:           msg,
		}
		if colRef != "" {
			// Extract table and column from "table.column".
			parts := strings.SplitN(colRef, ".", 2)
			if len(parts) == 2 {
				pg.TableName = parts[0]
				pg.ColumnName = parts[1]
				pg.ConstraintName = parts[0] + "_" + parts[1] + "_key"
			}
		}
		return pg
	}

	if strings.Contains(lower, "foreign key constraint failed") {
		return &PGError{
			Severity:         "ERROR",
			SeverityInternal: "ERROR",
			Code:             CodeForeignKeyViolation,
			Message:          "insert or update on table violates foreign key constraint",
			Detail:           msg,
		}
	}

	if strings.Contains(lower, "not null constraint failed") {
		colRef := extractConstraintColumn(msg, "not null constraint failed: ")
		pg := &PGError{
			Severity:         "ERROR",
			SeverityInternal: "ERROR",
			Code:             CodeNotNullViolation,
			Message:          "null value in column violates not-null constraint",
			Detail:           msg,
		}
		if colRef != "" {
			parts := strings.SplitN(colRef, ".", 2)
			if len(parts) == 2 {
				pg.TableName = parts[0]
				pg.ColumnName = parts[1]
			}
		}
		return pg
	}

	if strings.Contains(lower, "check constraint failed") {
		return &PGError{
			Severity:         "ERROR",
			SeverityInternal: "ERROR",
			Code:             CodeCheckViolation,
			Message:          "new row for relation violates check constraint",
			Detail:           msg,
		}
	}

	// ── Table / column not found ───────────────────────────────────────────
	if strings.Contains(lower, "no such table") {
		tableName := extractAfter(msg, "no such table: ")
		pg := &PGError{
			Severity:         "ERROR",
			SeverityInternal: "ERROR",
			Code:             CodeUndefinedTable,
			Message:          fmt.Sprintf("relation %q does not exist", tableName),
		}
		if tableName != "" {
			pg.TableName = tableName
		}
		return pg
	}

	if strings.Contains(lower, "no such column") {
		colName := extractAfter(msg, "no such column: ")
		pg := &PGError{
			Severity:         "ERROR",
			SeverityInternal: "ERROR",
			Code:             CodeUndefinedColumn,
			Message:          fmt.Sprintf("column %q does not exist", colName),
		}
		if colName != "" {
			pg.ColumnName = colName
		}
		return pg
	}

	if strings.Contains(lower, "table") && strings.Contains(lower, "already exists") {
		return &PGError{
			Severity:         "ERROR",
			SeverityInternal: "ERROR",
			Code:             CodeDuplicateTable,
			Message:          msg,
		}
	}

	if strings.Contains(lower, "no such index") {
		return &PGError{
			Severity:         "ERROR",
			SeverityInternal: "ERROR",
			Code:             CodeUndefinedObject,
			Message:          msg,
		}
	}

	// ── Transaction / locking ─────────────────────────────────────────────
	if strings.Contains(lower, "database is locked") ||
		strings.Contains(lower, "sqlite_busy") {
		return &PGError{
			Severity:         "ERROR",
			SeverityInternal: "ERROR",
			Code:             CodeSerializationFailure,
			Message:          "could not serialize access due to concurrent update",
			Detail:           msg,
			Hint:             "Retry the transaction.",
		}
	}

	if strings.Contains(lower, "cannot commit") ||
		strings.Contains(lower, "cannot start a transaction") {
		return &PGError{
			Severity:         "ERROR",
			SeverityInternal: "ERROR",
			Code:             CodeInvalidTransactionState,
			Message:          msg,
		}
	}

	// ── Syntax errors ─────────────────────────────────────────────────────
	if strings.Contains(lower, "syntax error") {
		return &PGError{
			Severity:         "ERROR",
			SeverityInternal: "ERROR",
			Code:             CodeSyntaxError,
			Message:          msg,
		}
	}

	// ── Arithmetic ────────────────────────────────────────────────────────
	if strings.Contains(lower, "division by zero") ||
		strings.Contains(lower, "divide by zero") {
		return &PGError{
			Severity:         "ERROR",
			SeverityInternal: "ERROR",
			Code:             CodeDivisionByZero,
			Message:          "division by zero",
		}
	}

	if strings.Contains(lower, "out of range") ||
		strings.Contains(lower, "overflow") {
		return &PGError{
			Severity:         "ERROR",
			SeverityInternal: "ERROR",
			Code:             CodeNumericValueOutOfRange,
			Message:          msg,
		}
	}

	// ── I/O ───────────────────────────────────────────────────────────────
	if strings.Contains(lower, "disk i/o error") ||
		strings.Contains(lower, "disk is full") {
		return &PGError{
			Severity:         "FATAL",
			SeverityInternal: "FATAL",
			Code:             CodeIOError,
			Message:          msg,
		}
	}

	if strings.Contains(lower, "no such file or directory") ||
		strings.Contains(lower, "unable to open database") {
		return &PGError{
			Severity:         "FATAL",
			SeverityInternal: "FATAL",
			Code:             CodeUndefinedFile,
			Message:          fmt.Sprintf("could not open database: %s", msg),
		}
	}

	// ── Catch-all ─────────────────────────────────────────────────────────
	return &PGError{
		Severity:         "ERROR",
		SeverityInternal: "ERROR",
		Code:             CodeInternalError,
		Message:          msg,
		Routine:          "sqlite_exec",
	}
}

// ─────────────────────────────────────────────────────────────────────────────
//  Common PGError constructors
// ─────────────────────────────────────────────────────────────────────────────

// ErrTooManyConnections returns a "too many connections" FATAL error.
func ErrTooManyConnections(max int) *PGError {
	return Fatal(CodeTooManyConnections,
		fmt.Sprintf("sorry, too many clients already (max=%d)", max))
}

// ErrUndefinedTable returns an "undefined table" error.
func ErrUndefinedTable(name string) *PGError {
	return New(CodeUndefinedTable,
		fmt.Sprintf("relation %q does not exist", name))
}

// ErrUndefinedColumn returns an "undefined column" error.
func ErrUndefinedColumn(name string) *PGError {
	return New(CodeUndefinedColumn,
		fmt.Sprintf("column %q does not exist", name))
}

// ErrSyntax returns a syntax error.
func ErrSyntax(msg string) *PGError {
	return New(CodeSyntaxError, msg)
}

// ErrFeatureNotSupported returns a feature-not-supported error.
func ErrFeatureNotSupported(feature string) *PGError {
	return New(CodeFeatureNotSupported,
		fmt.Sprintf("%s is not supported by sqlite-server", feature))
}

// ErrProtocolViolation returns a protocol-violation error.
func ErrProtocolViolation(msg string) *PGError {
	return New(CodeProtocolViolation, msg)
}

// ErrInvalidPassword returns an authentication failure error.
func ErrInvalidPassword(user string) *PGError {
	return Fatal(CodeInvalidPassword,
		fmt.Sprintf("password authentication failed for user %q", user))
}

// ErrInternalError wraps an unexpected internal error.
func ErrInternalError(err error) *PGError {
	return &PGError{
		Severity:         "ERROR",
		SeverityInternal: "ERROR",
		Code:             CodeInternalError,
		Message:          "internal server error",
		Detail:           err.Error(),
	}
}

// ─────────────────────────────────────────────────────────────────────────────
//  Helpers
// ─────────────────────────────────────────────────────────────────────────────

// extractConstraintColumn extracts the "table.column" part from a SQLite
// constraint failure message.
func extractConstraintColumn(msg, prefix string) string {
	lower := strings.ToLower(msg)
	pLower := strings.ToLower(prefix)
	idx := strings.Index(lower, pLower)
	if idx < 0 {
		return ""
	}
	return strings.TrimSpace(msg[idx+len(prefix):])
}

// extractAfter extracts the substring following prefix in msg.
func extractAfter(msg, prefix string) string {
	lower := strings.ToLower(msg)
	pLower := strings.ToLower(prefix)
	idx := strings.Index(lower, pLower)
	if idx < 0 {
		return ""
	}
	return strings.TrimSpace(msg[idx+len(prefix):])
}
