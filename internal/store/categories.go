package store

import "context"

// Category is a post grouping shown on the public site.
type Category struct {
	ID   int64  `json:"id" db:"id"`
	Name string `json:"name" db:"name"`
	Slug string `json:"slug" db:"slug"`
}

// ListCategories returns every category ordered by name.
func (s *Store) ListCategories(ctx context.Context) ([]Category, error) {
	return queryRows[Category](ctx, s.pool, `SELECT id, name, slug FROM categories ORDER BY name`)
}

// CreateCategory inserts a new category, generating a unique slug from its name.
func (s *Store) CreateCategory(ctx context.Context, c *Category) error {
	slug, err := s.uniqueSlug(ctx, "categories", slugify(c.Name), 0)
	if err != nil {
		return err
	}
	c.Slug = slug
	return s.pool.QueryRow(ctx,
		`INSERT INTO categories (name, slug) VALUES ($1, $2) RETURNING id`,
		c.Name, c.Slug,
	).Scan(&c.ID)
}

// DeleteCategory removes a category. Posts in it have their category cleared.
func (s *Store) DeleteCategory(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM categories WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
