package store

import (
	"context"
	"time"
)

// LibraryResource is a downloadable or linked item in the resource library.
type LibraryResource struct {
	ID          int64     `json:"id" db:"id"`
	Slug        string    `json:"slug" db:"slug"`
	Title       string    `json:"title" db:"title"`
	Description string    `json:"description" db:"description"`
	Type        string    `json:"type" db:"type"`
	Category    string    `json:"category" db:"category"`
	URL         string    `json:"url" db:"url"`
	Image       string    `json:"image" db:"image"`
	Status      string    `json:"status" db:"status"`
	SortOrder   int       `json:"sortOrder" db:"sort_order"`
	CreatedAt   time.Time `json:"createdAt" db:"created_at"`
	UpdatedAt   time.Time `json:"updatedAt" db:"updated_at"`
}

const librarySelect = `SELECT id, slug, title, description, type, category, url, image, status, sort_order, created_at, updated_at FROM library_resources`

// ListLibrary returns library resources ordered for display.
func (s *Store) ListLibrary(ctx context.Context, publishedOnly bool) ([]LibraryResource, error) {
	q := librarySelect
	if publishedOnly {
		q += ` WHERE status = 'published'`
	}
	q += ` ORDER BY sort_order, created_at DESC`
	return queryRows[LibraryResource](ctx, s.pool, q)
}

// GetLibraryResource returns one resource by id.
func (s *Store) GetLibraryResource(ctx context.Context, id int64) (*LibraryResource, error) {
	return queryOne[LibraryResource](ctx, s.pool, librarySelect+` WHERE id = $1`, id)
}

// GetLibraryResourceBySlug returns one published resource by slug for the
// public detail page.
func (s *Store) GetLibraryResourceBySlug(ctx context.Context, slug string) (*LibraryResource, error) {
	return queryOne[LibraryResource](ctx, s.pool, librarySelect+` WHERE slug = $1 AND status = 'published'`, slug)
}

// CreateLibraryResource inserts a resource, generating a unique slug.
func (s *Store) CreateLibraryResource(ctx context.Context, r *LibraryResource) error {
	base := r.Slug
	if base == "" {
		base = r.Title
	}
	slug, err := s.uniqueSlug(ctx, "library_resources", slugify(base), 0)
	if err != nil {
		return err
	}
	r.Slug = slug
	if r.Status == "" {
		r.Status = "published"
	}
	if r.Type == "" {
		r.Type = "Guide"
	}
	return s.pool.QueryRow(ctx, `
		INSERT INTO library_resources (slug, title, description, type, category, url, image, status, sort_order)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, created_at, updated_at`,
		r.Slug, r.Title, r.Description, r.Type, r.Category, r.URL, r.Image, r.Status, r.SortOrder,
	).Scan(&r.ID, &r.CreatedAt, &r.UpdatedAt)
}

// UpdateLibraryResource saves changes to an existing resource.
func (s *Store) UpdateLibraryResource(ctx context.Context, r *LibraryResource) error {
	base := r.Slug
	if base == "" {
		base = r.Title
	}
	slug, err := s.uniqueSlug(ctx, "library_resources", slugify(base), r.ID)
	if err != nil {
		return err
	}
	r.Slug = slug
	if r.Status == "" {
		r.Status = "published"
	}
	if r.Type == "" {
		r.Type = "Guide"
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE library_resources
		SET slug=$1, title=$2, description=$3, type=$4, category=$5, url=$6, image=$7, status=$8, sort_order=$9, updated_at=now()
		WHERE id=$10`,
		r.Slug, r.Title, r.Description, r.Type, r.Category, r.URL, r.Image, r.Status, r.SortOrder, r.ID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteLibraryResource removes a resource by id.
func (s *Store) DeleteLibraryResource(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM library_resources WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
