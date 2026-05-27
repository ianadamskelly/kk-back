package store

import (
	"context"
	"strings"
)

// ListProtectedFileURLs returns every distinct stored internal payload URL
// that can require relocation or filename reissuance at startup.
func (s *Store) ListProtectedFileURLs(ctx context.Context) ([]string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT url FROM (
			SELECT url FROM product_downloads
			UNION
			SELECT url FROM library_resources
			UNION
			SELECT file_url AS url FROM course_task_submissions
			UNION
			SELECT url FROM course_resources
		) t
		WHERE url LIKE '/uploads/%' OR url LIKE '/files/%'`)
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

// RetargetProtectedFileURL rewrites every protected-file reference to oldURL
// after its bytes have been copied to a freshly generated protected filename.
func (s *Store) RetargetProtectedFileURL(ctx context.Context, oldURL, newURL string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	aliasURL := oldURL
	if strings.HasPrefix(oldURL, "/files/") {
		aliasURL = "/uploads/protected/" + strings.TrimPrefix(oldURL, "/files/")
	} else if strings.HasPrefix(oldURL, "/uploads/protected/") {
		aliasURL = "/files/" + strings.TrimPrefix(oldURL, "/uploads/protected/")
	}
	if _, err := tx.Exec(ctx,
		`UPDATE product_downloads SET url = $1 WHERE url = $2 OR url = $3`, newURL, oldURL, aliasURL); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE library_resources SET url = $1 WHERE url = $2 OR url = $3`, newURL, oldURL, aliasURL); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE course_task_submissions SET file_url = $1 WHERE file_url = $2 OR file_url = $3`, newURL, oldURL, aliasURL); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE course_resources SET url = $1 WHERE url = $2 OR url = $3`, newURL, oldURL, aliasURL); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
