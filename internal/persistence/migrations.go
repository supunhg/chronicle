package persistence

import (
	"context"
	_ "embed"
	"fmt"
	"time"
)

//go:embed migrations/0001_initial_schema.sql
var schemaSQL string

//go:embed migrations/0002_world_rules.sql
var worldRulesSQL string

// CurrentVersion is the highest schema version Migrate will apply.
// Increment this when adding a new migration in Migrations below.
const CurrentVersion = 2

// Migrations lists every schema migration in version order. Index N is
// migration N+1. Each entry is a self-contained SQL script that, when
// applied to a database at version N, leaves the database at version
// N+1.
//
// Phase 1: v1 creates all 8 entity tables (people, relationships,
//          memories, locations, factions, events, inventory, world_meta).
// Phase 7: v2 adds world_rules for persisting WorldRules.
var Migrations = []string{
	schemaSQL,     // v1
	worldRulesSQL, // v2
}

// versionTableDDL creates the schema_version table if it does not
// exist. It is idempotent and safe to run on every Migrate call.
const versionTableDDL = `
CREATE TABLE IF NOT EXISTS schema_version (
    version    INTEGER PRIMARY KEY,
    applied_at INTEGER NOT NULL
);
`

// Version returns the current schema version of the database. Returns 0
// if the schema_version table does not exist (i.e. the database has
// never been migrated).
func (db *DB) Version() (int, error) {
	if _, err := db.Exec(versionTableDDL); err != nil {
		return 0, fmt.Errorf("persistence: create version table: %w", err)
	}
	var v int
	err := db.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_version").Scan(&v)
	if err != nil {
		return 0, fmt.Errorf("persistence: read version: %w", err)
	}
	return v, nil
}

// Migrate applies all pending migrations, bringing the database up to
// CurrentVersion. It is safe to call on a freshly created database
// (version 0) or on a database that is already at CurrentVersion
// (no-op). Each migration is applied in its own transaction.
func (db *DB) Migrate() error {
	current, err := db.Version()
	if err != nil {
		return err
	}
	if current >= CurrentVersion {
		return nil
	}
	for v := current + 1; v <= CurrentVersion; v++ {
		if err := db.applyMigration(int(v), Migrations[v-1]); err != nil {
			return err
		}
	}
	return nil
}

func (db *DB) applyMigration(version int, sqlText string) error {
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("persistence: begin migration v%d tx: %w", version, err)
	}
	defer func() { _ = tx.Rollback() }() // no-op if Commit succeeds

	if _, err := tx.ExecContext(ctx, sqlText); err != nil {
		return fmt.Errorf("persistence: apply migration v%d: %w", version, err)
	}
	if _, err := tx.ExecContext(ctx,
		"INSERT INTO schema_version (version, applied_at) VALUES (?, ?)",
		version, time.Now().Unix()); err != nil {
		return fmt.Errorf("persistence: record migration v%d: %w", version, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("persistence: commit migration v%d: %w", version, err)
	}
	return nil
}
