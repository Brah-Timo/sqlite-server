// Package pool manages SQLite connections for the wire server.
//
// Design principles:
//
//  1. ONE writer at a time.
//     Even in WAL mode, SQLite allows only one concurrent writer.
//     We enforce this with a WriterScheduler that serialises all write
//     operations through a single goroutine holding the writer lock.
//
//  2. MANY concurrent readers.
//     Read transactions in WAL mode do not block writers (they see a
//     consistent snapshot), so we allow up to MaxConns read connections
//     in parallel.
//
//  3. Per-session connections.
//     Each client session gets its own *sql.Conn from the pool.
//     This gives it a dedicated SQLite connection so that transaction
//     state is isolated.
//
//  4. Executor integration.
//     The pool exposes Execute() and Rewrite() methods so that the wire
//     session does not need to import the engine package directly —
//     this breaks the import cycle.
package pool

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite" // CGO-free pure-Go SQLite driver

	"github.com/sqlite-server/sqlite-server/internal/engine"
	"github.com/sqlite-server/sqlite-server/internal/pgproto"
)

// ─────────────────────────────────────────────────────────────────────────────
//  Configuration
// ─────────────────────────────────────────────────────────────────────────────

// Config holds the connection-pool configuration.
type Config struct {
	// MaxConns is the maximum number of concurrent client connections.
	// Each connection occupies one *sql.Conn from the underlying pool.
	MaxConns int

	// WALMode enables WAL journalling for higher write concurrency.
	// Recommended: true for any multi-client workload.
	WALMode bool

	// ReadOnly opens the database in read-only mode.
	ReadOnly bool

	// BusyTimeout is how long SQLite waits before returning SQLITE_BUSY.
	BusyTimeout time.Duration
}

// SQLConn is a dedicated SQLite connection belonging to one session.
type SQLConn = sql.Conn

// ─────────────────────────────────────────────────────────────────────────────
//  ConnPool
// ─────────────────────────────────────────────────────────────────────────────

// ConnPool wraps a *sql.DB and exposes session-level connection management,
// the write scheduler, and the executor.
type ConnPool struct {
	db       *sql.DB
	cfg      Config
	executor *engine.Executor
	wSched   *writerScheduler
	mu       sync.Mutex
	open     int // currently open sessions
}

// New opens the SQLite database and initialises the connection pool.
func New(dbPath string, cfg Config) (*ConnPool, error) {
	if cfg.MaxConns <= 0 {
		cfg.MaxConns = 100
	}
	if cfg.BusyTimeout <= 0 {
		cfg.BusyTimeout = 5 * time.Second
	}

	// ── Build the DSN ─────────────────────────────────────────────────────
	dsn := buildDSN(dbPath, cfg)

	// ── Open the database ─────────────────────────────────────────────────
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open %q: %w", dbPath, err)
	}

	// ── Connection pool settings ──────────────────────────────────────────
	// In WAL mode there can be many readers but only 1 writer.
	// We allow up to MaxConns open connections; the write scheduler handles
	// the write exclusivity.
	db.SetMaxOpenConns(cfg.MaxConns)
	db.SetMaxIdleConns(cfg.MaxConns / 2)
	db.SetConnMaxLifetime(30 * time.Minute)
	db.SetConnMaxIdleTime(10 * time.Minute)

	// ── Verify connectivity ───────────────────────────────────────────────
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping %q: %w", dbPath, err)
	}

	// ── One-time initialisation PRAGMAs ───────────────────────────────────
	if err := applyInitPragmas(ctx, db, cfg); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("init pragmas: %w", err)
	}

	cp := &ConnPool{
		db:       db,
		cfg:      cfg,
		executor: engine.New(),
		wSched:   newWriterScheduler(),
	}
	return cp, nil
}

// ─────────────────────────────────────────────────────────────────────────────
//  Session-level connection management
// ─────────────────────────────────────────────────────────────────────────────

// Acquire obtains a dedicated *sql.Conn for one client session.
// The caller MUST call Release when the session ends.
func (cp *ConnPool) Acquire(ctx context.Context) (*SQLConn, error) {
	cp.mu.Lock()
	if cp.open >= cp.cfg.MaxConns {
		cp.mu.Unlock()
		return nil, fmt.Errorf("too many connections (max=%d)", cp.cfg.MaxConns)
	}
	cp.open++
	cp.mu.Unlock()

	conn, err := cp.db.Conn(ctx)
	if err != nil {
		cp.mu.Lock()
		cp.open--
		cp.mu.Unlock()
		return nil, err
	}

	// Apply per-session PRAGMAs that must be set on the specific connection.
	if err := setSessionPragmas(ctx, conn, cp.cfg); err != nil {
		_ = conn.Close()
		cp.mu.Lock()
		cp.open--
		cp.mu.Unlock()
		return nil, err
	}

	return conn, nil
}

// Release returns a session connection to the pool.
func (cp *ConnPool) Release(conn *SQLConn) {
	if conn == nil {
		return
	}
	_ = conn.Close()
	cp.mu.Lock()
	cp.open--
	cp.mu.Unlock()
}

// Close shuts down the pool and releases all resources.
func (cp *ConnPool) Close() error {
	cp.wSched.Stop()
	return cp.db.Close()
}

// ─────────────────────────────────────────────────────────────────────────────
//  Execute / Rewrite (delegated to the engine)
// ─────────────────────────────────────────────────────────────────────────────

// Execute rewrites and runs a PostgreSQL SQL statement.
// Write statements are serialised through the writer scheduler.
func (cp *ConnPool) Execute(ctx context.Context, conn *SQLConn, pgSQL string, args []interface{}) (*pgproto.QueryResult, error) {
	// Classify the statement.
	cmd := commandClass(pgSQL)
	if isWriteCommand(cmd) {
		// Serialise writes.
		var result *pgproto.QueryResult
		var execErr error
		cp.wSched.Submit(func() {
			result, execErr = cp.executor.Execute(ctx, conn, pgSQL, args)
		})
		return result, execErr
	}
	// Reads can run concurrently.
	return cp.executor.Execute(ctx, conn, pgSQL, args)
}

// Rewrite rewrites a PostgreSQL SQL string without executing it.
// Used during the Parse phase of the Extended Query protocol.
func (cp *ConnPool) Rewrite(pgSQL string) (string, error) {
	return cp.executor.Rewrite(pgSQL)
}

// DescribeColumns returns column metadata for a rewritten SELECT.
func (cp *ConnPool) DescribeColumns(ctx context.Context, conn *SQLConn, rewrittenSQL string) ([]pgproto.ColumnDesc, error) {
	return cp.executor.DescribeColumns(ctx, conn, rewrittenSQL)
}

// ─────────────────────────────────────────────────────────────────────────────
//  DSN builder
// ─────────────────────────────────────────────────────────────────────────────

func buildDSN(dbPath string, cfg Config) string {
	params := []string{
		fmt.Sprintf("_busy_timeout=%d", cfg.BusyTimeout.Milliseconds()),
		"_foreign_keys=on",
		"_cache_size=-65536",   // 64 MiB cache
		"_mmap_size=268435456", // 256 MiB memory-mapped I/O
		"_temp_store=memory",
	}
	if cfg.WALMode {
		params = append(params, "_journal_mode=WAL")
		params = append(params, "_synchronous=NORMAL")
		params = append(params, "_wal_autocheckpoint=1000")
	} else {
		params = append(params, "_synchronous=FULL")
	}
	if cfg.ReadOnly {
		params = append(params, "mode=ro")
	}

	return fmt.Sprintf("file:%s?%s", dbPath, strings.Join(params, "&"))
}

// ─────────────────────────────────────────────────────────────────────────────
//  PRAGMA initialisation
// ─────────────────────────────────────────────────────────────────────────────

// applyInitPragmas runs one-time setup PRAGMAs on the shared *sql.DB.
func applyInitPragmas(ctx context.Context, db *sql.DB, cfg Config) error {
	pragmas := []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA cache_size    = -65536",
		"PRAGMA temp_store    = MEMORY",
		"PRAGMA mmap_size     = 268435456",
	}
	if cfg.WALMode {
		pragmas = append(pragmas,
			"PRAGMA journal_mode = WAL",
			"PRAGMA synchronous  = NORMAL",
			"PRAGMA wal_autocheckpoint = 1000",
		)
	}
	for _, p := range pragmas {
		if _, err := db.ExecContext(ctx, p); err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
	}
	return nil
}

// setSessionPragmas runs per-session PRAGMAs on a dedicated *sql.Conn.
func setSessionPragmas(ctx context.Context, conn *sql.Conn, cfg Config) error {
	pragmas := []string{
		"PRAGMA foreign_keys = ON",
	}
	for _, p := range pragmas {
		if _, err := conn.ExecContext(ctx, p); err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
	}
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
//  Write scheduler
// ─────────────────────────────────────────────────────────────────────────────

// writerScheduler serialises write operations.  In WAL mode, SQLite allows
// many readers but only one writer.  We use a single goroutine (the "writer
// thread") that executes all writes sequentially.
//
// Each write is submitted as a closure; the submitter blocks until the
// closure completes.  This gives simple, deadlock-free serialisation.
type writerScheduler struct {
	queue chan writerJob
	done  chan struct{}
}

type writerJob struct {
	fn     func()
	result chan struct{}
}

func newWriterScheduler() *writerScheduler {
	ws := &writerScheduler{
		queue: make(chan writerJob, 128),
		done:  make(chan struct{}),
	}
	go ws.run()
	return ws
}

// Submit executes fn in the writer goroutine and blocks until it completes.
func (ws *writerScheduler) Submit(fn func()) {
	job := writerJob{fn: fn, result: make(chan struct{}, 1)}
	select {
	case ws.queue <- job:
	case <-ws.done:
		return
	}
	<-job.result
}

// Stop shuts down the writer goroutine.
func (ws *writerScheduler) Stop() {
	close(ws.done)
}

func (ws *writerScheduler) run() {
	for {
		select {
		case job := <-ws.queue:
			job.fn()
			job.result <- struct{}{}
		case <-ws.done:
			return
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
//  Command classification helpers
// ─────────────────────────────────────────────────────────────────────────────

// commandClass returns the uppercase first word of a SQL statement.
func commandClass(sql string) string {
	sql = strings.TrimSpace(sql)
	idx := strings.IndexFunc(sql, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n'
	})
	if idx < 0 {
		return strings.ToUpper(sql)
	}
	return strings.ToUpper(sql[:idx])
}

// isWriteCommand returns true for statements that modify database state.
func isWriteCommand(cmd string) bool {
	switch cmd {
	case "INSERT", "UPDATE", "DELETE",
		"CREATE", "DROP", "ALTER", "TRUNCATE",
		"BEGIN", "COMMIT", "ROLLBACK",
		"SAVEPOINT", "RELEASE":
		return true
	}
	return false
}
