package store

import (
	"context"
	"time"
)

// Project is a portfolio case study.
type Project struct {
	ID         int64     `json:"id" db:"id"`
	Slug       string    `json:"slug" db:"slug"`
	Client     string    `json:"client" db:"client"`
	Title      string    `json:"title" db:"title"`
	Summary    string    `json:"summary" db:"summary"`
	Body       string    `json:"body" db:"body"`
	CoverImage string    `json:"coverImage" db:"cover_image"`
	Results    string    `json:"results" db:"results"`
	Category   string    `json:"category" db:"category"`
	SortOrder  int       `json:"sortOrder" db:"sort_order"`
	Status     string    `json:"status" db:"status"`
	CreatedAt  time.Time `json:"createdAt" db:"created_at"`
	UpdatedAt  time.Time `json:"updatedAt" db:"updated_at"`
}

const projectSelect = `SELECT id, slug, client, title, summary, body, cover_image, results, category, sort_order, status, created_at, updated_at FROM projects`

// ListProjects returns projects ordered for display.
func (s *Store) ListProjects(ctx context.Context, publishedOnly bool) ([]Project, error) {
	q := projectSelect
	if publishedOnly {
		q += ` WHERE status = 'published'`
	}
	q += ` ORDER BY sort_order, created_at DESC`
	return queryRows[Project](ctx, s.pool, q)
}

// GetProjectByID returns one project by numeric id.
func (s *Store) GetProjectByID(ctx context.Context, id int64) (*Project, error) {
	return queryOne[Project](ctx, s.pool, projectSelect+` WHERE id = $1`, id)
}

// GetProjectBySlug returns one project by slug.
func (s *Store) GetProjectBySlug(ctx context.Context, slug string, publishedOnly bool) (*Project, error) {
	q := projectSelect + ` WHERE slug = $1`
	if publishedOnly {
		q += ` AND status = 'published'`
	}
	return queryOne[Project](ctx, s.pool, q, slug)
}

// CreateProject inserts a project, generating a unique slug.
func (s *Store) CreateProject(ctx context.Context, p *Project) error {
	base := p.Slug
	if base == "" {
		base = p.Title
	}
	slug, err := s.uniqueSlug(ctx, "projects", slugify(base), 0)
	if err != nil {
		return err
	}
	p.Slug = slug
	if p.Status == "" {
		p.Status = "published"
	}
	return s.pool.QueryRow(ctx, `
		INSERT INTO projects (slug, client, title, summary, body, cover_image, results, category, sort_order, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id, created_at, updated_at`,
		p.Slug, p.Client, p.Title, p.Summary, p.Body, p.CoverImage, p.Results, p.Category, p.SortOrder, p.Status,
	).Scan(&p.ID, &p.CreatedAt, &p.UpdatedAt)
}

// UpdateProject saves changes to an existing project.
func (s *Store) UpdateProject(ctx context.Context, p *Project) error {
	base := p.Slug
	if base == "" {
		base = p.Title
	}
	slug, err := s.uniqueSlug(ctx, "projects", slugify(base), p.ID)
	if err != nil {
		return err
	}
	p.Slug = slug
	if p.Status == "" {
		p.Status = "published"
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE projects
		SET slug=$1, client=$2, title=$3, summary=$4, body=$5, cover_image=$6,
		    results=$7, category=$8, sort_order=$9, status=$10, updated_at=now()
		WHERE id=$11`,
		p.Slug, p.Client, p.Title, p.Summary, p.Body, p.CoverImage,
		p.Results, p.Category, p.SortOrder, p.Status, p.ID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteProject removes a project by id.
func (s *Store) DeleteProject(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM projects WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
