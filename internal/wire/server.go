// Package wire implements the PostgreSQL wire protocol v3.
// It handles the full lifecycle of a client connection: startup,
// authentication, the Simple Query protocol, the Extended Query
// protocol (Parse/Bind/Describe/Execute/Sync), COPY, and graceful
// termination.  Every accepted TCP connection gets its own goroutine
// and its own dedicated SQLite connection from the pool.
package wire

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sqlite-server/sqlite-server/internal/pool"
)

// ─────────────────────────────────────────────────────────────────────────────
//  Server configuration
// ─────────────────────────────────────────────────────────────────────────────

// ServerConfig holds every tunable parameter for the wire server.
type ServerConfig struct {
	// Addr is the TCP address to listen on, e.g. "0.0.0.0:5432".
	Addr string

	// ConnPool is the shared SQLite connection pool.
	ConnPool *pool.ConnPool

	// NoAuth disables password authentication (useful for local dev).
	NoAuth bool

	// TLSCert / TLSKey are optional paths to a TLS certificate / key pair.
	// When both are non-empty the server accepts SSL/TLS connections.
	TLSCert string
	TLSKey  string

	// LogLevel controls verbosity: "debug" | "info" | "warn" | "error".
	LogLevel string

	// Version is the server version string returned to clients.
	Version string

	// ReadTimeout / WriteTimeout for individual client connections.
	// Zero means no timeout.
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
}

// ─────────────────────────────────────────────────────────────────────────────
//  Server
// ─────────────────────────────────────────────────────────────────────────────

// Server is the main TCP listener.  It accepts connections and dispatches
// each one to a goroutine running a Session.
type Server struct {
	cfg         ServerConfig
	listener    net.Listener
	activeConns sync.WaitGroup
	connCount   atomic.Int64
	done        chan struct{}
	closeOnce   sync.Once
	tlsCfg      *tls.Config
}

// NewServer constructs a Server but does not start it.
func NewServer(cfg ServerConfig) *Server {
	s := &Server{
		cfg:  cfg,
		done: make(chan struct{}),
	}
	return s
}

// ListenAndServe starts the TCP listener and blocks until Shutdown is called
// or a fatal error occurs.
func (s *Server) ListenAndServe() error {
	ln, err := s.buildListener()
	if err != nil {
		return fmt.Errorf("listen %s: %w", s.cfg.Addr, err)
	}
	s.listener = ln
	defer ln.Close()

	s.logf("info", "listening on %s", s.cfg.Addr)

	for {
		conn, err := ln.Accept()
		if err != nil {
			// Check whether we were shut down intentionally.
			select {
			case <-s.done:
				s.logf("info", "server stopped — waiting for %d active connections",
					s.connCount.Load())
				s.activeConns.Wait()
				s.logf("info", "all connections closed")
				return nil
			default:
				s.logf("warn", "accept error: %v", err)
				continue
			}
		}

		s.activeConns.Add(1)
		s.connCount.Add(1)

		go func(c net.Conn) {
			defer s.activeConns.Done()
			defer s.connCount.Add(-1)
			s.handleConn(c)
		}(conn)
	}
}

// Shutdown signals the server to stop accepting connections and waits for all
// active sessions to finish.
func (s *Server) Shutdown() {
	s.closeOnce.Do(func() {
		close(s.done)
		if s.listener != nil {
			s.listener.Close()
		}
	})
}

// ActiveConnections returns the number of currently open client connections.
func (s *Server) ActiveConnections() int64 {
	return s.connCount.Load()
}

// ─────────────────────────────────────────────────────────────────────────────
//  Internal helpers
// ─────────────────────────────────────────────────────────────────────────────

// buildListener creates either a plain TCP or a TLS listener.
func (s *Server) buildListener() (net.Listener, error) {
	if s.cfg.TLSCert != "" && s.cfg.TLSKey != "" {
		cert, err := tls.LoadX509KeyPair(s.cfg.TLSCert, s.cfg.TLSKey)
		if err != nil {
			return nil, fmt.Errorf("load TLS keypair: %w", err)
		}
		tlsCfg := &tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS12,
		}
		s.tlsCfg = tlsCfg
		return tls.Listen("tcp", s.cfg.Addr, tlsCfg)
	}
	return net.Listen("tcp", s.cfg.Addr)
}

// handleConn creates a Session, runs it, and logs any error.
func (s *Server) handleConn(c net.Conn) {
	remote := c.RemoteAddr().String()
	s.logf("debug", "new connection from %s (total: %d)", remote, s.connCount.Load())

	sess := newSession(c, s.cfg)
	ctx := context.Background()

	if err := sess.Run(ctx); err != nil {
		// "EOF" and "connection reset by peer" are normal and not worth logging.
		if !isNormalDisconnect(err) {
			s.logf("warn", "session [%s] error: %v", remote, err)
		}
	}

	s.logf("debug", "connection closed [%s]", remote)
}

// logf is a minimal leveled logger.  In production code this would be wired
// to a structured logger (zap, slog, …); for now we print to stdout.
func (s *Server) logf(level, format string, args ...interface{}) {
	order := map[string]int{"debug": 0, "info": 1, "warn": 2, "error": 3}
	if order[level] < order[s.cfg.LogLevel] {
		return
	}
	prefix := fmt.Sprintf("[%s] sqlite-server ", level)
	fmt.Printf(prefix+format+"\n", args...)
}
