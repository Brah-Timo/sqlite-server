package wire

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/sqlite-server/sqlite-server/internal/pool"
)

// ─────────────────────────────────────────────────────────────────────────────
//  Session-level types
// ─────────────────────────────────────────────────────────────────────────────

// PreparedStmt holds a named or unnamed prepared statement as received
// through the Extended Query Parse message.
type PreparedStmt struct {
	Name         string
	OriginalSQL  string   // exact text the client sent (PostgreSQL dialect)
	RewrittenSQL string   // after our AST rewriter → SQLite dialect
	ParamOIDs    []uint32 // parameter type hints from the client (may be 0 = unknown)
	Columns      []ColumnDesc
}

// Portal is a bound statement (Bind message) ready for Execute.
type Portal struct {
	Name      string
	Stmt      *PreparedStmt
	Args      []interface{}
	MaxRows   int     // 0 = all rows
	ResultFmt []int16 // 0=text, 1=binary for each result column
}

// TxStatus mirrors PostgreSQL's ReadyForQuery transaction indicator.
type TxStatus byte

const (
	TxIdle   TxStatus = 'I' // not in a transaction
	TxOpen   TxStatus = 'T' // inside a transaction block
	TxFailed TxStatus = 'E' // in a failed transaction (needs ROLLBACK)
)

// ─────────────────────────────────────────────────────────────────────────────
//  Session
// ─────────────────────────────────────────────────────────────────────────────

// Session represents one client connection end-to-end.
type Session struct {
	conn     net.Conn
	reader   *bufio.Reader
	writer   *bufio.Writer
	writerMu sync.Mutex // guards concurrent writes (Cancel, etc.)
	cfg      ServerConfig

	// SQLite connection dedicated to this session.
	dbConn *pool.SQLConn

	// Extended-query protocol state.
	stmts   map[string]*PreparedStmt
	portals map[string]*Portal

	// Transaction state.
	txStatus TxStatus

	// PostgreSQL-compatible session parameters sent to the client during startup.
	params map[string]string

	// Pseudo-process identifiers (used by cancel requests).
	backendPID uint32
	secretKey  uint32

	// Accumulates messages within a pipeline (between Parse and Sync).
	pipelineError *PGError
}

// newSession allocates a Session for the given TCP connection.
func newSession(conn net.Conn, cfg ServerConfig) *Session {
	return &Session{
		conn:     conn,
		reader:   bufio.NewReaderSize(conn, 64*1024),
		writer:   bufio.NewWriterSize(conn, 64*1024),
		cfg:      cfg,
		stmts:    make(map[string]*PreparedStmt),
		portals:  make(map[string]*Portal),
		txStatus: TxIdle,
		params: map[string]string{
			// These mirror what PostgreSQL 14 sends during startup.
			"server_version":                "14.5",
			"server_encoding":               "UTF8",
			"client_encoding":               "UTF8",
			"application_name":              "",
			"default_transaction_isolation": "read committed",
			"DateStyle":                     "ISO, MDY",
			"IntervalStyle":                 "postgres",
			"TimeZone":                      "UTC",
			"integer_datetimes":             "on",
			"standard_conforming_strings":   "on",
		},
		backendPID: generatePID(),
		secretKey:  generateSecretKey(),
	}
}

// ─────────────────────────────────────────────────────────────────────────────
//  Session lifecycle
// ─────────────────────────────────────────────────────────────────────────────

// Run drives the full lifecycle of a session:
//  1. Acquire a SQLite connection from the pool.
//  2. Execute the startup / authentication handshake.
//  3. Enter the command loop.
func (s *Session) Run(ctx context.Context) error {
	defer s.conn.Close()

	// ── 1. Acquire a SQLite connection ────────────────────────────────────
	dbConn, err := s.cfg.ConnPool.Acquire(ctx)
	if err != nil {
		_ = s.sendFatalErrorMsg("53300", "too_many_connections",
			fmt.Sprintf("sorry, too many clients already: %v", err))
		return err
	}
	s.dbConn = dbConn
	defer s.cfg.ConnPool.Release(dbConn)

	// ── 2. Startup + authentication ────────────────────────────────────────
	if err := s.handleStartup(ctx); err != nil {
		return err
	}

	// ── 3. Command loop ────────────────────────────────────────────────────
	return s.commandLoop(ctx)
}

// commandLoop reads frontend messages and dispatches them until the client
// sends Terminate ('X') or the connection breaks.
func (s *Session) commandLoop(ctx context.Context) error {
	for {
		// ── Read message type (1 byte) ─────────────────────────────────────
		typeBuf := make([]byte, 1)
		if _, err := io.ReadFull(s.reader, typeBuf); err != nil {
			return err
		}
		msgType := typeBuf[0]

		// ── Read message length (4 bytes, includes the 4-byte length itself) ─
		lenBuf := make([]byte, 4)
		if _, err := io.ReadFull(s.reader, lenBuf); err != nil {
			return err
		}
		bodyLen := int(binary.BigEndian.Uint32(lenBuf)) - 4
		if bodyLen < 0 {
			return fmt.Errorf("protocol violation: negative body length for message '%c'", msgType)
		}

		// ── Read body ─────────────────────────────────────────────────────
		var body []byte
		if bodyLen > 0 {
			body = make([]byte, bodyLen)
			if _, err := io.ReadFull(s.reader, body); err != nil {
				return err
			}
		}

		// ── Dispatch ──────────────────────────────────────────────────────
		if err := s.dispatch(ctx, msgType, body); err != nil {
			if errors.Is(err, errTerminate) {
				return nil // clean termination
			}
			return err
		}
	}
}

// errTerminate is a sentinel returned by handleTerminate to signal a clean exit.
var errTerminate = errors.New("terminate")

// dispatch routes a frontend message to its handler.
func (s *Session) dispatch(ctx context.Context, msgType byte, body []byte) error {
	switch msgType {

	// ── Simple Query protocol ──────────────────────────────────────────────
	case 'Q':
		return s.handleSimpleQuery(ctx, body)

	// ── Extended Query protocol ────────────────────────────────────────────
	case 'P':
		return s.handleParse(ctx, body)
	case 'B':
		return s.handleBind(ctx, body)
	case 'D':
		return s.handleDescribe(ctx, body)
	case 'E':
		return s.handleExecute(ctx, body)
	case 'C':
		return s.handleClose(body)
	case 'S':
		return s.handleSync(ctx)
	case 'H':
		return s.handleFlush()

	// ── COPY ──────────────────────────────────────────────────────────────
	case 'd': // CopyData
		return s.handleCopyData(ctx, body)
	case 'c': // CopyDone
		return s.handleCopyDone(ctx)
	case 'f': // CopyFail
		return s.handleCopyFail(body)

	// ── Session management ────────────────────────────────────────────────
	case 'X': // Terminate
		return s.handleTerminate()

	// ── Function call (legacy, rarely used) ───────────────────────────────
	case 'F':
		return s.handleFunctionCall(ctx, body)

	default:
		err := newPGError("08P01", "protocol_violation",
			fmt.Sprintf("unknown frontend message type '%c' (0x%02X)", msgType, msgType))
		_ = s.sendError(err)
		return fmt.Errorf("protocol violation: unknown message %c", msgType)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
//  Transaction helpers
// ─────────────────────────────────────────────────────────────────────────────

func (s *Session) setTxStatus(status TxStatus) { s.txStatus = status }

func (s *Session) currentTxStatus() TxStatus { return s.txStatus }

// ─────────────────────────────────────────────────────────────────────────────
//  Random helpers
// ─────────────────────────────────────────────────────────────────────────────

var pidRng = rand.New(rand.NewSource(time.Now().UnixNano())) //nolint:gosec
var pidMu sync.Mutex

func generatePID() uint32 {
	pidMu.Lock()
	defer pidMu.Unlock()
	return uint32(pidRng.Int31n(99999) + 1)
}

func generateSecretKey() uint32 {
	pidMu.Lock()
	defer pidMu.Unlock()
	return uint32(pidRng.Int31())
}

// ─────────────────────────────────────────────────────────────────────────────
//  Low-level read helpers
// ─────────────────────────────────────────────────────────────────────────────

// readCString reads a null-terminated string from b starting at offset 0
// and returns the string and the number of bytes consumed (including the \0).
func readCString(b []byte) (string, int) {
	for i, c := range b {
		if c == 0 {
			return string(b[:i]), i + 1
		}
	}
	return string(b), len(b)
}

// ─────────────────────────────────────────────────────────────────────────────
//  Miscellaneous
// ─────────────────────────────────────────────────────────────────────────────

// isNormalDisconnect returns true for errors that indicate the client closed
// the connection without sending Terminate — perfectly normal behaviour.
func isNormalDisconnect(err error) bool {
	if err == nil {
		return true
	}
	msg := err.Error()
	return err == io.EOF ||
		strings.Contains(msg, "connection reset by peer") ||
		strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "use of closed network connection")
}

// handleTerminate processes the Terminate ('X') message.
func (s *Session) handleTerminate() error {
	return errTerminate
}

// handleFunctionCall handles legacy function-call messages.  We send an
// error because no modern client should be sending these; they pre-date
// the extended query protocol and are not worth implementing.
func (s *Session) handleFunctionCall(_ context.Context, _ []byte) error {
	err := newPGError("0A000", "feature_not_supported",
		"function call protocol is not supported; use the extended query protocol")
	return s.sendError(err)
}
