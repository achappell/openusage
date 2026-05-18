package hermes

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"

	_ "github.com/mattn/go-sqlite3"
)

// openReadOnly opens the Hermes state.db using SQLite's read-only, immutable
// file URI. Immutable mode tells SQLite the file will not be modified for
// the lifetime of the connection so it skips taking the shared lock, which
// is the only safe way to read a database that the host tool is actively
// writing to without risking SQLITE_BUSY.
//
// MaxOpenConns is capped at 1 because the queries we run are short and
// serialized.
func openReadOnly(dbPath string) (*sql.DB, error) {
	if dbPath == "" {
		return nil, fmt.Errorf("hermes: empty db path")
	}
	encoded := (&url.URL{Path: dbPath}).EscapedPath()
	dsn := fmt.Sprintf("file:%s?mode=ro&immutable=1", encoded)

	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("hermes: opening state db: %w", err)
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

func pingContext(ctx context.Context, db *sql.DB) error {
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("hermes: pinging state db: %w", err)
	}
	return nil
}
