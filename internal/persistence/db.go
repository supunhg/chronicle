// Package persistence provides SQLite-backed storage for Chronicle.
//
// Phase 13 scope: world_meta, people, world_rules, relationships, and
// memories. Snapshot/Restore round-trip those five tables. The schema
// also defines tables for locations, factions, events, and inventory,
// but Snapshot/Restore do not touch them yet — they are reserved for
// later phases.
package persistence

import (
	"database/sql"
	"fmt"

	// Pure-Go SQLite driver, no CGO. Registers the "sqlite" driver name.
	_ "modernc.org/sqlite"
)

// DB wraps *sql.DB with Chronicle-specific helpers. It is the only type
// in this package that should be constructed directly (via Open); the
// other functions are methods.
type DB struct {
	*sql.DB
	path string
}

// Open opens or creates a Chronicle database at the given path.
// The parent directory must already exist; only the database file is
// created. Foreign keys are enabled on every connection.
//
// Open does NOT run migrations. Call Migrate to bring the schema up to
// the current version.
func Open(path string) (*DB, error) {
	sqlDB, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("persistence: open %s: %w", path, err)
	}
	if err := sqlDB.Ping(); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("persistence: ping %s: %w", path, err)
	}
	// Foreign keys are off by default in SQLite. Enable them.
	if _, err := sqlDB.Exec("PRAGMA foreign_keys = ON"); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("persistence: enable foreign keys: %w", err)
	}
	return &DB{DB: sqlDB, path: path}, nil
}

// Close closes the database. Safe to call on a nil receiver.
func (db *DB) Close() error {
	if db == nil || db.DB == nil {
		return nil
	}
	return db.DB.Close()
}

// Path returns the filesystem path the database was opened from.
func (db *DB) Path() string {
	if db == nil {
		return ""
	}
	return db.path
}
