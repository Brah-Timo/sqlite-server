package wire

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
)

// ─────────────────────────────────────────────────────────────────────────────
//  PostgreSQL startup protocol constants
// ─────────────────────────────────────────────────────────────────────────────

const (
	// protoVersion3 is the 32-bit integer that encodes protocol version 3.0.
	// It equals (3 << 16) | 0 = 196608.
	protoVersion3 = 196608

	// sslRequestCode is the magic number for an SSLRequest message.
	// It equals (1234 << 16) | 5679 = 80877103.
	sslRequestCode = 80877103

	// cancelRequestCode is the magic number for a CancelRequest message.
	// It equals (1234 << 16) | 5678 = 80877102.
	cancelRequestCode = 80877102
)

// ─────────────────────────────────────────────────────────────────────────────
//  handleStartup — the very first thing that runs for a new connection
// ─────────────────────────────────────────────────────────────────────────────

// handleStartup performs the startup handshake:
//
//  1. Read the untyped startup message (length + version + key=value pairs).
//  2. Handle SSLRequest or CancelRequest as a special case.
//  3. Authenticate the client (delegate to auth.go).
//  4. Send ParameterStatus, BackendKeyData, ReadyForQuery.
func (s *Session) handleStartup(ctx context.Context) error {
	for {
		// ── Read total message length (4 bytes) ───────────────────────────
		lenBuf := make([]byte, 4)
		if _, err := io.ReadFull(s.reader, lenBuf); err != nil {
			return fmt.Errorf("startup: read length: %w", err)
		}
		totalLen := int(binary.BigEndian.Uint32(lenBuf))
		if totalLen < 8 {
			return fmt.Errorf("startup: message too short (%d bytes)", totalLen)
		}

		// ── Read body (totalLen - 4 bytes already read) ───────────────────
		bodyLen := totalLen - 4
		body := make([]byte, bodyLen)
		if _, err := io.ReadFull(s.reader, body); err != nil {
			return fmt.Errorf("startup: read body: %w", err)
		}

		// ── Read the protocol version / request code ──────────────────────
		requestCode := binary.BigEndian.Uint32(body[:4])

		switch requestCode {

		// ── SSLRequest ─────────────────────────────────────────────────────
		case sslRequestCode:
			if s.cfg.TLSCert != "" {
				// Server supports TLS — respond 'S' and upgrade the connection.
				if _, err := s.conn.Write([]byte{'S'}); err != nil {
					return err
				}
				// The TLS upgrade is handled at the listener level; just loop.
				continue
			}
			// Server does not support TLS — decline with 'N'.
			if _, err := s.conn.Write([]byte{'N'}); err != nil {
				return err
			}
			// Client will retry with a plain startup message.
			continue

		// ── CancelRequest ──────────────────────────────────────────────────
		case cancelRequestCode:
			s.handleCancelRequest(body[4:])
			return fmt.Errorf("startup: cancel request received — closing")

		// ── Normal startup (protocol 3.0) ──────────────────────────────────
		case protoVersion3:
			params := parseStartupParameters(body[4:])
			if appName, ok := params["application_name"]; ok {
				s.params["application_name"] = appName
			}

			// Authenticate and complete handshake.
			return s.completeStartup(ctx, params)

		default:
			return fmt.Errorf("startup: unsupported protocol version 0x%08X", requestCode)
		}
	}
}

// completeStartup authenticates the client and sends the post-auth messages.
func (s *Session) completeStartup(ctx context.Context, startupParams map[string]string) error {
	// ── Authentication ─────────────────────────────────────────────────────
	if err := s.authenticate(ctx, startupParams); err != nil {
		return err
	}

	// ── ParameterStatus messages ──────────────────────────────────────────
	for key, val := range s.params {
		if err := s.sendParameterStatus(key, val); err != nil {
			return err
		}
	}

	// ── BackendKeyData ────────────────────────────────────────────────────
	if err := s.sendBackendKeyData(); err != nil {
		return err
	}

	// ── ReadyForQuery ─────────────────────────────────────────────────────
	return s.sendReadyForQuery(s.txStatus)
}

// ─────────────────────────────────────────────────────────────────────────────
//  Startup parameter parser
// ─────────────────────────────────────────────────────────────────────────────

// parseStartupParameters decodes the NUL-delimited key=value pairs that
// follow the 4-byte protocol version in a startup message.
//
// Wire format: key\0value\0key\0value\0\0
func parseStartupParameters(data []byte) map[string]string {
	params := make(map[string]string)
	for len(data) > 0 {
		key, n := readCString(data)
		data = data[n:]
		if key == "" {
			break
		}
		val, n := readCString(data)
		data = data[n:]
		params[key] = val
	}
	return params
}

// ─────────────────────────────────────────────────────────────────────────────
//  Cancel request
// ─────────────────────────────────────────────────────────────────────────────

// handleCancelRequest processes a CancelRequest message.
// A cancel request contains the PID and secret key of the session to cancel.
// Because SQLite has no notion of query cancellation, we simply acknowledge
// and close the connection (the server protocol requires the connection to
// be closed immediately after a cancel request, with no response).
func (s *Session) handleCancelRequest(data []byte) {
	if len(data) < 8 {
		return
	}
	// pid := binary.BigEndian.Uint32(data[0:4])  — reserved for future use
	// key := binary.BigEndian.Uint32(data[4:8])
}

// ─────────────────────────────────────────────────────────────────────────────
//  Startup response messages
// ─────────────────────────────────────────────────────────────────────────────

// sendBackendKeyData sends the BackendKeyData message.
//
// Wire format: 'K' | int32(12) | int32(pid) | int32(secretKey)
func (s *Session) sendBackendKeyData() error {
	msg := make([]byte, 12)
	binary.BigEndian.PutUint32(msg[0:], uint32(12))
	binary.BigEndian.PutUint32(msg[4:], s.backendPID)
	binary.BigEndian.PutUint32(msg[8:], s.secretKey)
	return s.writeMessage('K', msg[:8])
}

// sendParameterStatus sends one ParameterStatus message.
//
// Wire format: 'S' | int32(len) | name\0 | value\0
func (s *Session) sendParameterStatus(name, value string) error {
	body := append([]byte(name), 0)
	body = append(body, []byte(value)...)
	body = append(body, 0)
	return s.writeMessage('S', body)
}
