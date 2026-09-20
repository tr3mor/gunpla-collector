package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"path"
	"sort"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// migrate applies every migration file in migrations/ whose number is
// greater than the database's current `PRAGMA user_version`, in filename
// order, each inside its own transaction. Files are numbered
// 0001_*.sql, 0002_*.sql, ... — a file's numeric prefix becomes the new
// user_version once that file's statements commit successfully, so a
// database always records exactly which migrations it has applied and
// re-opening it is a no-op.
//
// Never edit a migration file that has already shipped — add a new one
// instead. Migration 1 is the original CREATE TABLE IF NOT EXISTS schema,
// so it's a safe no-op against a database that already has the tables but
// user_version = 0 (every database created before migrations existed).
func migrate(ctx context.Context, db *sql.DB) error {
	names, err := migrationNames()
	if err != nil {
		return err
	}

	var version int
	if err := db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return fmt.Errorf("read user_version: %w", err)
	}

	for i, name := range names {
		n := i + 1
		if n <= version {
			continue
		}
		if err := applyMigration(ctx, db, name, n); err != nil {
			return err
		}
	}
	return nil
}

func migrationNames() ([]string, error) {
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return nil, fmt.Errorf("read migrations dir: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names) // zero-padded numeric prefix sorts correctly as text
	return names, nil
}

func applyMigration(ctx context.Context, db *sql.DB, name string, version int) error {
	body, err := migrationsFS.ReadFile(path.Join("migrations", name))
	if err != nil {
		return fmt.Errorf("read migration %s: %w", name, err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx for migration %s: %w", name, err)
	}
	defer tx.Rollback() // no-op once committed

	if _, err := tx.ExecContext(ctx, string(body)); err != nil {
		return fmt.Errorf("apply migration %s: %w", name, err)
	}
	// PRAGMA statements don't accept bound parameters; version comes from
	// our own sorted filename list, never external input.
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`PRAGMA user_version = %d`, version)); err != nil {
		return fmt.Errorf("set user_version after migration %s: %w", name, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration %s: %w", name, err)
	}
	return nil
}
