// Package store wraps the PostgreSQL database with typed access methods.
package store

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound is returned when a requested record does not exist.
var ErrNotFound = errors.New("not found")

// ErrDuplicate is returned when a unique constraint is violated.
var ErrDuplicate = errors.New("duplicate")

// ErrDownloadLimit is returned when a customer has reached the
// per-download max_downloads cap on a digital product purchase.
var ErrDownloadLimit = errors.New("download limit reached")

// ErrAssetUseLimit is returned when an interactive asset has no
// remaining metered uses.
var ErrAssetUseLimit = errors.New("asset use limit reached")

// Store wraps the PostgreSQL connection pool.
type Store struct {
	pool *pgxpool.Pool
}

// New opens the connection pool and runs pending migrations.
func New(ctx context.Context, databaseURL string) (*Store, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	s := &Store{pool: pool}
	if err := s.migrate(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return s, nil
}

// Close releases the connection pool.
func (s *Store) Close() { s.pool.Close() }

// queryRows runs a query and decodes every row into a slice of T by column name.
func queryRows[T any](ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) ([]T, error) {
	rows, err := pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, pgx.RowToStructByNameLax[T])
	if err != nil {
		return nil, err
	}
	if out == nil {
		out = []T{}
	}
	return out, nil
}

// queryOne runs a query expected to return exactly one row, decoded into T.
func queryOne[T any](ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) (*T, error) {
	rows, err := pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	v, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByNameLax[T])
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &v, nil
}

// --- Slug helpers ---

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = slugRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		s = "item"
	}
	return s
}

// uniqueSlug appends a counter until the slug is free within the given table.
// excludeID lets a record keep its own slug during updates.
func (s *Store) uniqueSlug(ctx context.Context, table, base string, excludeID int64) (string, error) {
	query := fmt.Sprintf("SELECT id FROM %s WHERE slug = $1", table)
	candidate := base
	for i := 2; ; i++ {
		var id int64
		err := s.pool.QueryRow(ctx, query, candidate).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return candidate, nil
		}
		if err != nil {
			return "", err
		}
		if id == excludeID {
			return candidate, nil
		}
		candidate = fmt.Sprintf("%s-%d", base, i)
	}
}
