package wire

import (
	"context"
	"strings"
)

// ─────────────────────────────────────────────────────────────────────────────
//  Simple Query Protocol  ('Q')
// ─────────────────────────────────────────────────────────────────────────────
//
// The Simple Query protocol is the original PostgreSQL query mode.  The client
// sends a single 'Q' message containing an optional semicolon-separated list
// of SQL statements.  The server executes all of them in sequence and sends
// the results followed by a final ReadyForQuery.
//
// Reference: https://www.postgresql.org/docs/14/protocol-flow.html#id-1.10.6.7.4

// handleSimpleQuery processes a Simple Query ('Q') message.
func (s *Session) handleSimpleQuery(ctx context.Context, body []byte) error {
	// Strip trailing NUL byte.
	query := strings.TrimRight(string(body), "\x00")
	query = strings.TrimSpace(query)

	if query == "" {
		if err := s.sendEmptyQueryResponse(); err != nil {
			return err
		}
		return s.sendReadyForQuery(s.txStatus)
	}

	// ── Split on semicolons to support multi-statement queries ─────────────
	// DBeaver and psql commonly send "SET ...; SELECT ..." in one message.
	statements := splitStatements(query)

	for _, stmt := range statements {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}

		result, err := s.cfg.ConnPool.Execute(ctx, s.dbConn, stmt, nil)
		if err != nil {
			pgErr := toPGError(err)
			if sendErr := s.sendError(pgErr); sendErr != nil {
				return sendErr
			}
			// After an error in simple query mode, we must still send ReadyForQuery.
			s.setTxStatus(TxFailed)
			return s.sendReadyForQuery(s.txStatus)
		}

		// Track transaction status changes.
		s.updateTxStatus(stmt)

		// ── Send results ─────────────────────────────────────────────────
		if err := s.sendResult(result); err != nil {
			return err
		}
	}

	return s.sendReadyForQuery(s.txStatus)
}

// sendResult dispatches to the appropriate response formatter based on the
// type of result (SELECT vs DML vs DDL vs empty).
func (s *Session) sendResult(r *QueryResult) error {
	if r == nil {
		return nil
	}

	// DML / DDL / TCL — no rows, just a command tag.
	if len(r.Columns) == 0 {
		return s.sendCommandComplete(r.Tag)
	}

	// SELECT / RETURNING — send RowDescription + DataRow* + CommandComplete.
	if err := s.sendRowDescription(r.Columns); err != nil {
		return err
	}
	for _, row := range r.Rows {
		if err := s.sendDataRow(row); err != nil {
			return err
		}
	}
	return s.sendCommandComplete(r.Tag)
}

// updateTxStatus inspects a statement and adjusts the session transaction
// status indicator.  This is necessary because SQLite returns the final
// transaction state only after each EXEC; we need to track it ourselves.
func (s *Session) updateTxStatus(stmt string) {
	upper := strings.ToUpper(strings.TrimSpace(stmt))
	switch {
	case strings.HasPrefix(upper, "BEGIN"),
		strings.HasPrefix(upper, "START TRANSACTION"):
		s.setTxStatus(TxOpen)
	case strings.HasPrefix(upper, "COMMIT"),
		strings.HasPrefix(upper, "END"):
		s.setTxStatus(TxIdle)
	case strings.HasPrefix(upper, "ROLLBACK"):
		s.setTxStatus(TxIdle)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
//  Statement splitter
// ─────────────────────────────────────────────────────────────────────────────

// splitStatements splits a SQL string on ';' boundaries, respecting single-
// quoted string literals and -- line comments.  Dollar-quoted strings
// ($$…$$) are handled for PostgreSQL compatibility.
func splitStatements(sql string) []string {
	var stmts []string
	var current strings.Builder
	inSingleQuote := false
	inLineComment := false
	inDollarQuote := false
	dollarTag := ""
	i := 0

	for i < len(sql) {
		ch := sql[i]

		// ── Line comment ──────────────────────────────────────────────────
		if !inSingleQuote && !inDollarQuote && i+1 < len(sql) &&
			ch == '-' && sql[i+1] == '-' {
			inLineComment = true
			current.WriteByte(ch)
			i++
			continue
		}
		if inLineComment {
			if ch == '\n' {
				inLineComment = false
			}
			current.WriteByte(ch)
			i++
			continue
		}

		// ── Dollar quoting ($tag$…$tag$) ──────────────────────────────────
		if !inSingleQuote && !inDollarQuote && ch == '$' {
			// Look for the closing '$' of the tag.
			end := strings.Index(sql[i+1:], "$")
			if end >= 0 {
				tag := sql[i : i+end+2]
				inDollarQuote = true
				dollarTag = tag
				current.WriteString(tag)
				i += end + 2
				continue
			}
		}
		if inDollarQuote {
			if strings.HasPrefix(sql[i:], dollarTag) {
				current.WriteString(dollarTag)
				i += len(dollarTag)
				inDollarQuote = false
				dollarTag = ""
				continue
			}
			current.WriteByte(ch)
			i++
			continue
		}

		// ── Single-quoted string ──────────────────────────────────────────
		if ch == '\'' && !inSingleQuote {
			inSingleQuote = true
			current.WriteByte(ch)
			i++
			continue
		}
		if inSingleQuote {
			current.WriteByte(ch)
			if ch == '\'' {
				// Handle escaped quote: '' means a literal single quote.
				if i+1 < len(sql) && sql[i+1] == '\'' {
					i++
					current.WriteByte(sql[i])
				} else {
					inSingleQuote = false
				}
			}
			i++
			continue
		}

		// ── Statement separator ───────────────────────────────────────────
		if ch == ';' {
			stmt := strings.TrimSpace(current.String())
			if stmt != "" {
				stmts = append(stmts, stmt)
			}
			current.Reset()
			i++
			continue
		}

		current.WriteByte(ch)
		i++
	}

	// Trailing statement without a semicolon.
	if stmt := strings.TrimSpace(current.String()); stmt != "" {
		stmts = append(stmts, stmt)
	}
	return stmts
}
