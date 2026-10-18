// Package storage implements PostgreSQL persistence for PulseWatch.
package storage

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Th3K3rn3l/PulseWatch/migrations"
)

// ErrNotFound is returned when a requested entity does not exist.
var ErrNotFound = errors.New("not found")

// Store is a PostgreSQL-backed repository.
type Store struct {
	pool *pgxpool.Pool
}

// New connects to PostgreSQL and verifies the connection.
func New(ctx context.Context, dsn string) (*Store, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("parse dsn: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return &Store{pool: pool}, nil
}

// Close releases all pool connections.
func (s *Store) Close() { s.pool.Close() }

// Migrate applies the embedded schema. The schema is idempotent, so it is
// safe to run on every start. For a bigger project a dedicated migration
// tool (golang-migrate, goose) with versioning would be a better fit.
func (s *Store) Migrate(ctx context.Context) error {
	schema, err := migrations.FS.ReadFile(migrations.SchemaFile)
	if err != nil {
		return fmt.Errorf("read schema: %w", err)
	}

	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire connection: %w", err)
	}
	defer conn.Release()

	// PgConn.Exec uses the simple protocol, which allows multi-statement SQL.
	if _, err := conn.Conn().PgConn().Exec(ctx, string(schema)).ReadAll(); err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}
	return nil
}
