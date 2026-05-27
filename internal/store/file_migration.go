package store

import "context"

// ListLegacyProtectedFileURLs returns every distinct URL still in
// the /uploads/ namespace that refers to a non-image payload: digital
// product downloads, library resources, course-task attachments.
// Used by the one-shot startup migration that moves these files into
// the protected dir.
func (s *Store) ListLegacyProtectedFileURLs(ctx context.Context) ([]string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT url FROM (
			SELECT url FROM product_downloads        WHERE url LIKE '/uploads/%'
			UNION
			SELECT url FROM library_resources        WHERE url LIKE '/uploads/%'
			UNION
			SELECT file_url AS url FROM course_task_submissions WHERE file_url LIKE '/uploads/%'
		) t
		WHERE url <> ''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// RetargetProtectedFileURL rewrites every reference to oldURL across
// the three tables that hold protected-file pointers, replacing them
// with newURL. Used by the legacy-files migration after the bytes
// have been copied to the protected dir.
func (s *Store) RetargetProtectedFileURL(ctx context.Context, oldURL, newURL string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx,
		`UPDATE product_downloads SET url = $1 WHERE url = $2`, newURL, oldURL); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE library_resources SET url = $1 WHERE url = $2`, newURL, oldURL); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE course_task_submissions SET file_url = $1 WHERE file_url = $2`, newURL, oldURL); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
