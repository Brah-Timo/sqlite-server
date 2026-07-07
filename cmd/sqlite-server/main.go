// Package main is the entry point for the sqlite-server binary.
// sqlite-server exposes a SQLite database over the PostgreSQL wire protocol,
// allowing any PostgreSQL-compatible client (DBeaver, psql, pgAdmin, Npgsql,
// psycopg2, GORM, Hibernate, Entity Framework …) to connect without any
// modification.
package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/sqlite-server/sqlite-server/internal/pool"
	"github.com/sqlite-server/sqlite-server/internal/wire"
)

// Version is injected at build time via -ldflags.
var Version = "dev"

// rootCmd is the top-level cobra command.
var rootCmd = &cobra.Command{
	Use:   "sqlite-server [flags] <database.db>",
	Short: "Expose a SQLite database over the PostgreSQL wire protocol",
	Long: `sqlite-server is a zero-dependency server that speaks the full
PostgreSQL wire protocol (v3) on top of a local SQLite file.

Any PostgreSQL client — DBeaver, psql, Npgsql, psycopg2, GORM,
Hibernate, Entity Framework — connects and behaves as if it were
talking to a real PostgreSQL instance, while all data lives in a
single .db file on disk.

Examples:
  sqlite-server myapp.db
  sqlite-server --addr 127.0.0.1:5433 --max-conn 100 production.db
  sqlite-server --wal=false --no-auth dev.db
  sqlite-server --ssl-cert cert.pem --ssl-key key.pem secure.db
  sqlite-server version`,
	RunE:          runServer,
	SilenceUsage:  true,
	SilenceErrors: true,
}

// CLI flags (bound in init).
var (
	flagAddr        string
	flagMaxConn     int
	flagWAL         bool
	flagNoAuth      bool
	flagSSLCert     string
	flagSSLKey      string
	flagBusyTimeout time.Duration
	flagLogLevel    string
	flagReadOnly    bool
)

func init() {
	rootCmd.Flags().StringVar(&flagAddr, "addr", "0.0.0.0:5432",
		"TCP address to listen on (host:port)")
	rootCmd.Flags().IntVar(&flagMaxConn, "max-conn", 100,
		"Maximum number of concurrent client connections")
	rootCmd.Flags().BoolVar(&flagWAL, "wal", true,
		"Enable WAL journal mode for higher write concurrency")
	rootCmd.Flags().BoolVar(&flagNoAuth, "no-auth", false,
		"Disable password authentication (development mode)")
	rootCmd.Flags().StringVar(&flagSSLCert, "ssl-cert", "",
		"Path to TLS certificate file (PEM); enables TLS when set")
	rootCmd.Flags().StringVar(&flagSSLKey, "ssl-key", "",
		"Path to TLS private key file (PEM); required with --ssl-cert")
	rootCmd.Flags().DurationVar(&flagBusyTimeout, "busy-timeout", 5*time.Second,
		"SQLite busy_timeout — how long to wait when the DB is locked")
	rootCmd.Flags().StringVar(&flagLogLevel, "log-level", "info",
		"Log verbosity: debug | info | warn | error")
	rootCmd.Flags().BoolVar(&flagReadOnly, "read-only", false,
		"Open the database in read-only mode (disables all writes)")

	// version sub-command
	rootCmd.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(_ *cobra.Command, _ []string) {
			fmt.Printf("sqlite-server %s\n", Version)
		},
	})
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

// runServer is called when the user invokes the root command.
func runServer(cmd *cobra.Command, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("missing required argument: <database.db>\n\nUsage:\n  %s", cmd.UseLine())
	}
	dbPath := args[0]

	// ------------------------------------------------------------------
	// 1. Open / create the SQLite connection pool.
	// ------------------------------------------------------------------
	poolCfg := pool.Config{
		MaxConns:    flagMaxConn,
		WALMode:     flagWAL,
		ReadOnly:    flagReadOnly,
		BusyTimeout: flagBusyTimeout,
	}
	cp, err := pool.New(dbPath, poolCfg)
	if err != nil {
		return fmt.Errorf("open database %q: %w", dbPath, err)
	}
	defer cp.Close()

	fmt.Printf("sqlite-server %s\n", Version)
	fmt.Printf("  database  : %s\n", dbPath)
	fmt.Printf("  listen    : %s\n", flagAddr)
	fmt.Printf("  max-conn  : %d\n", flagMaxConn)
	fmt.Printf("  wal-mode  : %v\n", flagWAL)
	fmt.Printf("  read-only : %v\n", flagReadOnly)
	fmt.Printf("  auth      : %v\n", !flagNoAuth)
	if flagSSLCert != "" {
		fmt.Printf("  tls       : %s / %s\n", flagSSLCert, flagSSLKey)
	}

	// ------------------------------------------------------------------
	// 2. Build and start the wire-protocol server.
	// ------------------------------------------------------------------
	srvCfg := wire.ServerConfig{
		Addr:     flagAddr,
		ConnPool: cp,
		NoAuth:   flagNoAuth,
		TLSCert:  flagSSLCert,
		TLSKey:   flagSSLKey,
		LogLevel: flagLogLevel,
		Version:  Version,
	}
	srv := wire.NewServer(srvCfg)

	// ------------------------------------------------------------------
	// 3. Graceful shutdown on SIGINT / SIGTERM.
	// ------------------------------------------------------------------
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigCh
		fmt.Printf("\nreceived signal %s — shutting down gracefully…\n", sig)
		srv.Shutdown()
	}()

	// ------------------------------------------------------------------
	// 4. Block until the server exits.
	// ------------------------------------------------------------------
	fmt.Printf("\nReady. Accepting connections on %s\n\n", flagAddr)
	if err := srv.ListenAndServe(); err != nil {
		return fmt.Errorf("server: %w", err)
	}
	return nil
}
