package store

import "context"

// Stat is a single headline metric shown in the homepage stats band.
type Stat struct {
	ID        int64  `json:"id" db:"id"`
	Label     string `json:"label" db:"label"`
	Value     string `json:"value" db:"value"`
	SortOrder int    `json:"sortOrder" db:"sort_order"`
}

const statSelect = `SELECT id, label, value, sort_order FROM stats`

// ListStats returns stats ordered for display.
func (s *Store) ListStats(ctx context.Context) ([]Stat, error) {
	return queryRows[Stat](ctx, s.pool, statSelect+` ORDER BY sort_order, id`)
}

// GetStat returns one stat by id.
func (s *Store) GetStat(ctx context.Context, id int64) (*Stat, error) {
	return queryOne[Stat](ctx, s.pool, statSelect+` WHERE id = $1`, id)
}

// CreateStat inserts a new stat.
func (s *Store) CreateStat(ctx context.Context, st *Stat) error {
	return s.pool.QueryRow(ctx, `
		INSERT INTO stats (label, value, sort_order)
		VALUES ($1, $2, $3) RETURNING id`,
		st.Label, st.Value, st.SortOrder,
	).Scan(&st.ID)
}

// UpdateStat saves changes to an existing stat.
func (s *Store) UpdateStat(ctx context.Context, st *Stat) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE stats SET label=$1, value=$2, sort_order=$3 WHERE id=$4`,
		st.Label, st.Value, st.SortOrder, st.ID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteStat removes a stat by id.
func (s *Store) DeleteStat(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM stats WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
