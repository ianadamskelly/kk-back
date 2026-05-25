package store

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Product is an item sold in the shop.
type Product struct {
	ID          int64     `json:"id" db:"id"`
	Slug        string    `json:"slug" db:"slug"`
	Name        string    `json:"name" db:"name"`
	Description string    `json:"description" db:"description"`
	Body        string    `json:"body" db:"body"`
	PriceCents  int64     `json:"priceCents" db:"price_cents"`
	Image       string    `json:"image" db:"image"`
	Category    string    `json:"category" db:"category"`
	Status      string    `json:"status" db:"status"`
	SortOrder   int       `json:"sortOrder" db:"sort_order"`
	CreatedAt   time.Time `json:"createdAt" db:"created_at"`
	UpdatedAt   time.Time `json:"updatedAt" db:"updated_at"`
}

// ProductFilter controls filtering for ListProducts.
type ProductFilter struct {
	Search        string
	Category      string
	PublishedOnly bool
}

const productSelect = `SELECT id, slug, name, description, body, price_cents, image, category, status, sort_order, created_at, updated_at FROM products`

// ListProducts returns products matching the filter, ordered for display.
func (s *Store) ListProducts(ctx context.Context, f ProductFilter) ([]Product, error) {
	conds := []string{}
	args := []any{}
	if f.PublishedOnly {
		conds = append(conds, "status = 'published'")
	}
	if f.Search != "" {
		args = append(args, "%"+f.Search+"%")
		n := len(args)
		conds = append(conds, fmt.Sprintf("(name ILIKE $%d OR description ILIKE $%d)", n, n))
	}
	if f.Category != "" {
		args = append(args, f.Category)
		conds = append(conds, fmt.Sprintf("category = $%d", len(args)))
	}
	q := productSelect
	if len(conds) > 0 {
		q += " WHERE " + strings.Join(conds, " AND ")
	}
	q += " ORDER BY sort_order, created_at DESC"
	return queryRows[Product](ctx, s.pool, q, args...)
}

// GetProductByID returns one product by numeric id.
func (s *Store) GetProductByID(ctx context.Context, id int64) (*Product, error) {
	return queryOne[Product](ctx, s.pool, productSelect+` WHERE id = $1`, id)
}

// GetProductBySlug returns one product by slug.
func (s *Store) GetProductBySlug(ctx context.Context, slug string, publishedOnly bool) (*Product, error) {
	q := productSelect + ` WHERE slug = $1`
	if publishedOnly {
		q += ` AND status = 'published'`
	}
	return queryOne[Product](ctx, s.pool, q, slug)
}

// CreateProduct inserts a product, generating a unique slug.
func (s *Store) CreateProduct(ctx context.Context, p *Product) error {
	base := p.Slug
	if base == "" {
		base = p.Name
	}
	slug, err := s.uniqueSlug(ctx, "products", slugify(base), 0)
	if err != nil {
		return err
	}
	p.Slug = slug
	if p.Status == "" {
		p.Status = "published"
	}
	return s.pool.QueryRow(ctx, `
		INSERT INTO products (slug, name, description, body, price_cents, image, category, status, sort_order)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, created_at, updated_at`,
		p.Slug, p.Name, p.Description, p.Body, p.PriceCents, p.Image, p.Category, p.Status, p.SortOrder,
	).Scan(&p.ID, &p.CreatedAt, &p.UpdatedAt)
}

// UpdateProduct saves changes to an existing product.
func (s *Store) UpdateProduct(ctx context.Context, p *Product) error {
	base := p.Slug
	if base == "" {
		base = p.Name
	}
	slug, err := s.uniqueSlug(ctx, "products", slugify(base), p.ID)
	if err != nil {
		return err
	}
	p.Slug = slug
	if p.Status == "" {
		p.Status = "published"
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE products
		SET slug=$1, name=$2, description=$3, body=$4, price_cents=$5, image=$6,
		    category=$7, status=$8, sort_order=$9, updated_at=now()
		WHERE id=$10`,
		p.Slug, p.Name, p.Description, p.Body, p.PriceCents, p.Image,
		p.Category, p.Status, p.SortOrder, p.ID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteProduct removes a product by id.
func (s *Store) DeleteProduct(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM products WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
