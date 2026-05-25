package store

import (
	"context"
	"time"
)

// Service is an offering shown on the Services page and its detail page.
type Service struct {
	ID        int64     `json:"id" db:"id"`
	Slug      string    `json:"slug" db:"slug"`
	Title     string    `json:"title" db:"title"`
	Summary   string    `json:"summary" db:"summary"`
	Body      string    `json:"body" db:"body"`
	Icon      string    `json:"icon" db:"icon"`
	SortOrder int       `json:"sortOrder" db:"sort_order"`
	Status    string    `json:"status" db:"status"`
	CreatedAt time.Time `json:"createdAt" db:"created_at"`
	UpdatedAt time.Time `json:"updatedAt" db:"updated_at"`
}

const serviceSelect = `SELECT id, slug, title, summary, body, icon, sort_order, status, created_at, updated_at FROM services`

// ListServices returns services ordered for display. When publishedOnly is set,
// only published services are returned.
func (s *Store) ListServices(ctx context.Context, publishedOnly bool) ([]Service, error) {
	q := serviceSelect
	if publishedOnly {
		q += ` WHERE status = 'published'`
	}
	q += ` ORDER BY sort_order, title`
	return queryRows[Service](ctx, s.pool, q)
}

// GetServiceByID returns one service by numeric id.
func (s *Store) GetServiceByID(ctx context.Context, id int64) (*Service, error) {
	return queryOne[Service](ctx, s.pool, serviceSelect+` WHERE id = $1`, id)
}

// GetServiceBySlug returns one service by slug.
func (s *Store) GetServiceBySlug(ctx context.Context, slug string, publishedOnly bool) (*Service, error) {
	q := serviceSelect + ` WHERE slug = $1`
	if publishedOnly {
		q += ` AND status = 'published'`
	}
	return queryOne[Service](ctx, s.pool, q, slug)
}

// CreateService inserts a service, generating a unique slug.
func (s *Store) CreateService(ctx context.Context, v *Service) error {
	base := v.Slug
	if base == "" {
		base = v.Title
	}
	slug, err := s.uniqueSlug(ctx, "services", slugify(base), 0)
	if err != nil {
		return err
	}
	v.Slug = slug
	if v.Status == "" {
		v.Status = "published"
	}
	return s.pool.QueryRow(ctx, `
		INSERT INTO services (slug, title, summary, body, icon, sort_order, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, created_at, updated_at`,
		v.Slug, v.Title, v.Summary, v.Body, v.Icon, v.SortOrder, v.Status,
	).Scan(&v.ID, &v.CreatedAt, &v.UpdatedAt)
}

// UpdateService saves changes to an existing service.
func (s *Store) UpdateService(ctx context.Context, v *Service) error {
	base := v.Slug
	if base == "" {
		base = v.Title
	}
	slug, err := s.uniqueSlug(ctx, "services", slugify(base), v.ID)
	if err != nil {
		return err
	}
	v.Slug = slug
	if v.Status == "" {
		v.Status = "published"
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE services
		SET slug=$1, title=$2, summary=$3, body=$4, icon=$5, sort_order=$6, status=$7, updated_at=now()
		WHERE id=$8`,
		v.Slug, v.Title, v.Summary, v.Body, v.Icon, v.SortOrder, v.Status, v.ID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteService removes a service by id.
func (s *Store) DeleteService(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM services WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
