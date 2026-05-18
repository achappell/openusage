package goose

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"

	_ "github.com/mattn/go-sqlite3"
)

// openReadOnly opens the sessions.db at the given path using SQLite's
// read-only, immutable file URI. Immutable mode tells SQLite the file will
// not be modified for the lifetime of the connection so it skips taking the
// shared lock — which is the only safe way to read a database that another
// process (the host AI tool) is actively writing to without risking SQLITE_BUSY.
//
// We also disable cgo's connection pooling expectations by capping
// MaxOpenConns at 1: the queries we run are short and serialized, and a
// single connection avoids surprise SQLITE_BUSY when concurrent goroutines
// inside our process race on the same immutable handle.
func openReadOnly(dbPath string) (*sql.DB, error) {
	if dbPath == "" {
		return nil, fmt.Errorf("goose: empty db path")
	}

	// The SQLite URI parser needs the path component pre-escaped on its
	// own; url.PathEscape leaves the leading "/" intact which is exactly
	// what `file:` URIs want.
	encoded := (&url.URL{Path: dbPath}).EscapedPath()
	dsn := fmt.Sprintf("file:%s?mode=ro&immutable=1", encoded)

	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("goose: opening sessions db: %w", err)
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

// pingContext wraps DB.PingContext so callers always get a
// provider-prefixed error.
func pingContext(ctx context.Context, db *sql.DB) error {
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("goose: pinging sessions db: %w", err)
	}
	return nil
}
