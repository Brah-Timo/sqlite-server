package wire

import (
	"context"
	"encoding/binary"
	"fmt"
	"strings"
)

// ─────────────────────────────────────────────────────────────────────────────
//  Extended Query Protocol
// ─────────────────────────────────────────────────────────────────────────────
//
// The extended query protocol decouples parsing from execution.  It consists
// of five message types that can be pipelined:
//
//   Parse     ('P')  — Parse SQL, store a PreparedStmt by name
//   Bind      ('B')  — Bind parameter values to a portal
//   Describe  ('D')  — Describe a statement or portal (return type info)
//   Execute   ('E')  — Execute a portal, optionally limiting rows
//   Close     ('C')  — Close a named statement or portal
//   Sync      ('S')  — Flush pipeline, send ReadyForQuery
//   Flush     ('H')  — Flush output buffer without ReadyForQuery
//
// Reference: https://www.postgresql.org/docs/14/protocol-flow.html#PROTOCOL-FLOW-EXT-QUERY

// ─────────────────────────────────────────────────────────────────────────────
//  Parse  ('P')
// ─────────────────────────────────────────────────────────────────────────────

// handleParse processes a Parse message.
//
// Wire format:
//
//	name\0 | query\0 | int16(numParamTypes) | [int32 OIDs…]
func (s *Session) handleParse(ctx context.Context, body []byte) error {
	if s.pipelineError != nil {
		return nil // discard until Sync
	}

	offset := 0
	name, n := readCString(body[offset:])
	offset += n

	query, n := readCString(body[offset:])
	offset += n

	if offset+2 > len(body) {
		return s.setPipelineError(newPGError("08P01", "protocol_violation",
			"Parse message too short"))
	}
	numParamTypes := int(binary.BigEndian.Uint16(body[offset:]))
	offset += 2

	paramOIDs := make([]uint32, numParamTypes)
	for i := 0; i < numParamTypes; i++ {
		if offset+4 > len(body) {
			return s.setPipelineError(newPGError("08P01", "protocol_violation",
				"Parse message: not enough bytes for parameter OIDs"))
		}
		paramOIDs[i] = binary.BigEndian.Uint32(body[offset:])
		offset += 4
	}

	// ── Rewrite the query through the AST pipeline ────────────────────────
	rewritten, err := s.cfg.ConnPool.Rewrite(query)
	if err != nil {
		return s.setPipelineError(toPGError(err))
	}

	// ── Store the prepared statement ──────────────────────────────────────
	stmt := &PreparedStmt{
		Name:         name,
		OriginalSQL:  query,
		RewrittenSQL: rewritten,
		ParamOIDs:    paramOIDs,
	}
	s.stmts[name] = stmt

	return s.sendParseComplete()
}

// ─────────────────────────────────────────────────────────────────────────────
//  Bind  ('B')
// ─────────────────────────────────────────────────────────────────────────────

// handleBind processes a Bind message.
//
// Wire format:
//
//	portalName\0 | stmtName\0 |
//	int16(numParamFmtCodes) | [int16 fmtCodes…] |
//	int16(numParams) | for each: int32(len=-1 for NULL or >=0) | data |
//	int16(numResultFmtCodes) | [int16 fmtCodes…]
func (s *Session) handleBind(ctx context.Context, body []byte) error {
	if s.pipelineError != nil {
		return nil
	}

	offset := 0

	portalName, n := readCString(body[offset:])
	offset += n

	stmtName, n := readCString(body[offset:])
	offset += n

	stmt, ok := s.stmts[stmtName]
	if !ok {
		return s.setPipelineError(&PGError{
			Severity: "ERROR",
			Code:     "26000",
			Message:  fmt.Sprintf("prepared statement %q does not exist", stmtName),
		})
	}

	// ── Parameter format codes ─────────────────────────────────────────────
	if offset+2 > len(body) {
		return s.setPipelineError(newPGError("08P01", "protocol_violation", "Bind: short"))
	}
	numFmtCodes := int(binary.BigEndian.Uint16(body[offset:]))
	offset += 2
	fmtCodes := make([]int16, numFmtCodes)
	for i := 0; i < numFmtCodes; i++ {
		fmtCodes[i] = int16(binary.BigEndian.Uint16(body[offset:]))
		offset += 2
	}

	// ── Parameter values ──────────────────────────────────────────────────
	if offset+2 > len(body) {
		return s.setPipelineError(newPGError("08P01", "protocol_violation", "Bind: short"))
	}
	numParams := int(binary.BigEndian.Uint16(body[offset:]))
	offset += 2

	args := make([]interface{}, numParams)
	for i := 0; i < numParams; i++ {
		if offset+4 > len(body) {
			return s.setPipelineError(newPGError("08P01", "protocol_violation", "Bind: param overflow"))
		}
		paramLen := int(int32(binary.BigEndian.Uint32(body[offset:])))
		offset += 4

		if paramLen == -1 {
			args[i] = nil // NULL
			continue
		}
		if offset+paramLen > len(body) {
			return s.setPipelineError(newPGError("08P01", "protocol_violation", "Bind: param data overflow"))
		}
		paramData := body[offset : offset+paramLen]
		offset += paramLen

		isBinary := false
		if numFmtCodes == 1 {
			isBinary = fmtCodes[0] == 1
		} else if i < numFmtCodes {
			isBinary = fmtCodes[i] == 1
		}
		args[i] = decodeParamValue(paramData, isBinary)
	}

	// ── Result format codes ───────────────────────────────────────────────
	if offset+2 > len(body) {
		return s.setPipelineError(newPGError("08P01", "protocol_violation", "Bind: result fmt short"))
	}
	numResultFmtCodes := int(binary.BigEndian.Uint16(body[offset:]))
	offset += 2
	resultFmtCodes := make([]int16, numResultFmtCodes)
	for i := 0; i < numResultFmtCodes; i++ {
		resultFmtCodes[i] = int16(binary.BigEndian.Uint16(body[offset:]))
		offset += 2
	}

	// ── Store the portal ──────────────────────────────────────────────────
	s.portals[portalName] = &Portal{
		Name:      portalName,
		Stmt:      stmt,
		Args:      args,
		ResultFmt: resultFmtCodes,
	}

	return s.sendBindComplete()
}

// ─────────────────────────────────────────────────────────────────────────────
//  Describe  ('D')
// ─────────────────────────────────────────────────────────────────────────────

// handleDescribe processes a Describe message.
//
// Wire format: byte('S' or 'P') | name\0
//
// 'S' = describe prepared statement  → ParameterDescription + RowDescription
// 'P' = describe portal              → RowDescription only
func (s *Session) handleDescribe(ctx context.Context, body []byte) error {
	if s.pipelineError != nil {
		return nil
	}
	if len(body) < 2 {
		return s.setPipelineError(newPGError("08P01", "protocol_violation", "Describe too short"))
	}

	objType := body[0]
	name, _ := readCString(body[1:])

	switch objType {
	case 'S': // describe statement
		stmt, ok := s.stmts[name]
		if !ok {
			return s.setPipelineError(&PGError{
				Severity: "ERROR",
				Code:     "26000",
				Message:  fmt.Sprintf("prepared statement %q does not exist", name),
			})
		}
		// Send parameter type OIDs.
		if err := s.sendParameterDescription(stmt.ParamOIDs); err != nil {
			return err
		}
		// Send row description if the statement returns rows; NoData otherwise.
		if len(stmt.Columns) == 0 {
			// We may not know columns yet — attempt a dry-run EXPLAIN.
			cols, err := s.cfg.ConnPool.DescribeColumns(ctx, s.dbConn, stmt.RewrittenSQL)
			if err == nil {
				stmt.Columns = cols
			}
		}
		if len(stmt.Columns) == 0 {
			return s.sendNoData()
		}
		return s.sendRowDescription(stmt.Columns)

	case 'P': // describe portal
		portal, ok := s.portals[name]
		if !ok {
			return s.setPipelineError(&PGError{
				Severity: "ERROR",
				Code:     "34000",
				Message:  fmt.Sprintf("portal %q does not exist", name),
			})
		}
		if len(portal.Stmt.Columns) == 0 {
			return s.sendNoData()
		}
		return s.sendRowDescription(portal.Stmt.Columns)

	default:
		return s.setPipelineError(newPGError("08P01", "protocol_violation",
			fmt.Sprintf("Describe: unknown object type '%c'", objType)))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
//  Execute  ('E')
// ─────────────────────────────────────────────────────────────────────────────

// handleExecute processes an Execute message.
//
// Wire format: portalName\0 | int32(maxRows)  (0 = unlimited)
func (s *Session) handleExecute(ctx context.Context, body []byte) error {
	if s.pipelineError != nil {
		return nil
	}

	portalName, n := readCString(body)
	maxRows := 0
	if n+4 <= len(body) {
		maxRows = int(int32(binary.BigEndian.Uint32(body[n:])))
	}

	portal, ok := s.portals[portalName]
	if !ok {
		return s.setPipelineError(&PGError{
			Severity: "ERROR",
			Code:     "34000",
			Message:  fmt.Sprintf("portal %q does not exist", portalName),
		})
	}

	result, err := s.cfg.ConnPool.Execute(ctx, s.dbConn, portal.Stmt.RewrittenSQL, portal.Args)
	if err != nil {
		pgErr := toPGError(err)
		if sendErr := s.sendError(pgErr); sendErr != nil {
			return sendErr
		}
		s.pipelineError = pgErr
		return nil
	}

	s.updateTxStatus(portal.Stmt.OriginalSQL)

	// Send rows.
	if len(result.Columns) > 0 {
		rowsSent := 0
		for _, row := range result.Rows {
			if maxRows > 0 && rowsSent >= maxRows {
				return s.sendPortalSuspended()
			}
			if err := s.sendDataRow(row); err != nil {
				return err
			}
			rowsSent++
		}
	}

	return s.sendCommandComplete(result.Tag)
}

// ─────────────────────────────────────────────────────────────────────────────
//  Close  ('C')
// ─────────────────────────────────────────────────────────────────────────────

// handleClose processes a Close message.
//
// Wire format: byte('S' or 'P') | name\0
func (s *Session) handleClose(body []byte) error {
	if len(body) < 2 {
		return nil
	}
	objType := body[0]
	name, _ := readCString(body[1:])

	switch objType {
	case 'S':
		delete(s.stmts, name)
	case 'P':
		delete(s.portals, name)
	}
	return s.sendCloseComplete()
}

// ─────────────────────────────────────────────────────────────────────────────
//  Sync  ('S')
// ─────────────────────────────────────────────────────────────────────────────

// handleSync processes a Sync message.
//
// Sync ends an extended-query pipeline.  If there was a pipeline error, it
// was already sent; we reset the error state and send ReadyForQuery.
func (s *Session) handleSync(ctx context.Context) error {
	if s.pipelineError != nil {
		s.pipelineError = nil
		if s.txStatus == TxOpen {
			s.setTxStatus(TxFailed)
		}
	}
	return s.sendReadyForQuery(s.txStatus)
}

// ─────────────────────────────────────────────────────────────────────────────
//  Flush  ('H')
// ─────────────────────────────────────────────────────────────────────────────

// handleFlush flushes the output buffer without sending ReadyForQuery.
func (s *Session) handleFlush() error {
	s.writerMu.Lock()
	defer s.writerMu.Unlock()
	return s.writer.Flush()
}

// ─────────────────────────────────────────────────────────────────────────────
//  Pipeline error management
// ─────────────────────────────────────────────────────────────────────────────

// setPipelineError records a pipeline-level error, sends it to the client,
// and returns nil so the command loop keeps running until Sync.
func (s *Session) setPipelineError(err *PGError) error {
	s.pipelineError = err
	return s.sendError(err)
}

// ─────────────────────────────────────────────────────────────────────────────
//  COPY protocol  ('d', 'c', 'f')
// ─────────────────────────────────────────────────────────────────────────────

// handleCopyData receives a CopyData message during a COPY IN operation.
// Full COPY support is planned for v0.3; for now we acknowledge and discard.
func (s *Session) handleCopyData(_ context.Context, _ []byte) error {
	return nil
}

// handleCopyDone receives a CopyDone message.
func (s *Session) handleCopyDone(_ context.Context) error {
	return s.sendCommandComplete("COPY 0")
}

// handleCopyFail receives a CopyFail message and rolls back the COPY.
func (s *Session) handleCopyFail(body []byte) error {
	msg, _ := readCString(body)
	if msg == "" {
		msg = "COPY failed"
	}
	pgErr := newPGError("57014", "query_canceled",
		fmt.Sprintf("COPY from stdin failed: %s", msg))
	return s.sendError(pgErr)
}

// isSelect returns true if this result contains a row set.
// QueryResult is defined in internal/pgproto and aliased in types.go.
func isSelect(r *QueryResult) bool {
	return len(r.Columns) > 0
}

// commandTag returns a fully formed PostgreSQL command tag.
func commandTag(cmd string, n int64) string {
	upper := strings.ToUpper(strings.TrimSpace(cmd))
	switch {
	case strings.HasPrefix(upper, "INSERT"):
		return fmt.Sprintf("INSERT 0 %d", n)
	case strings.HasPrefix(upper, "UPDATE"):
		return fmt.Sprintf("UPDATE %d", n)
	case strings.HasPrefix(upper, "DELETE"):
		return fmt.Sprintf("DELETE %d", n)
	case strings.HasPrefix(upper, "SELECT"),
		strings.HasPrefix(upper, "WITH"),
		strings.HasPrefix(upper, "VALUES"):
		return fmt.Sprintf("SELECT %d", n)
	case strings.HasPrefix(upper, "CREATE"):
		return "CREATE"
	case strings.HasPrefix(upper, "DROP"):
		return "DROP"
	case strings.HasPrefix(upper, "ALTER"):
		return "ALTER"
	case strings.HasPrefix(upper, "BEGIN"),
		strings.HasPrefix(upper, "START"):
		return "BEGIN"
	case strings.HasPrefix(upper, "COMMIT"):
		return "COMMIT"
	case strings.HasPrefix(upper, "ROLLBACK"):
		return "ROLLBACK"
	case strings.HasPrefix(upper, "SAVEPOINT"):
		return "SAVEPOINT"
	case strings.HasPrefix(upper, "RELEASE"):
		return "RELEASE"
	case strings.HasPrefix(upper, "SET"):
		return "SET"
	case strings.HasPrefix(upper, "SHOW"):
		return "SHOW"
	default:
		if n > 0 {
			return fmt.Sprintf("%s %d", upper, n)
		}
		return upper
	}
}
