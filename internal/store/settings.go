package store

import "context"

// GetSettings returns all site settings as a key/value map.
func (s *Store) GetSettings(ctx context.Context) (map[string]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT key, value FROM site_settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

// UpdateSettings upserts every key/value pair in the given map.
func (s *Store) UpdateSettings(ctx context.Context, settings map[string]string) error {
	for k, v := range settings {
		if _, err := s.pool.Exec(ctx, `
			INSERT INTO site_settings (key, value) VALUES ($1, $2)
			ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`, k, v); err != nil {
			return err
		}
	}
	return nil
}
