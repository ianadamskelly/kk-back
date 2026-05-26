package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Product is an item sold in the shop.
//
// The Image column is a denormalised "cover" URL — kept in sync with
// the cover row in product_images. List endpoints can read it without
// joining; the gallery endpoints use the product_images table directly.
//
// Kind is 'physical' (default; ships) or 'digital' (downloadable files
// attached via product_downloads). MaxDownloads is nil for unlimited.
type Product struct {
	ID            int64          `json:"id" db:"id"`
	Slug          string         `json:"slug" db:"slug"`
	Name          string         `json:"name" db:"name"`
	Description   string         `json:"description" db:"description"`
	Body          string         `json:"body" db:"body"`
	PriceCents    int64          `json:"priceCents" db:"price_cents"`
	Image         string         `json:"image" db:"image"`
	Category      string         `json:"category" db:"category"`
	Status        string         `json:"status" db:"status"`
	SortOrder     int            `json:"sortOrder" db:"sort_order"`
	Kind          string         `json:"kind" db:"kind"`
	MaxDownloads  *int           `json:"maxDownloads" db:"max_downloads"`
	CreatedAt     time.Time      `json:"createdAt" db:"created_at"`
	UpdatedAt     time.Time      `json:"updatedAt" db:"updated_at"`
	Images        []ProductImage `json:"images" db:"-"`
}

// ProductDownload is one downloadable file attached to a digital
// product. Never exposed on public endpoints — the customer reaches
// the file via a signed token (see /api/downloads in a later phase).
type ProductDownload struct {
	ID        int64     `json:"id" db:"id"`
	ProductID int64     `json:"productId" db:"product_id"`
	URL       string    `json:"url" db:"url"`
	Label     string    `json:"label" db:"label"`
	SizeBytes int64     `json:"sizeBytes" db:"size_bytes"`
	Position  int       `json:"position" db:"position"`
	CreatedAt time.Time `json:"createdAt" db:"created_at"`
}

const productDownloadSelect = `SELECT id, product_id, url, label, size_bytes, position, created_at FROM product_downloads`

// ProductImage is one image in a product's gallery. Exactly one image
// per product has is_cover = true; the cover's URL is also denormalised
// into products.image for cheap listings.
type ProductImage struct {
	ID        int64     `json:"id" db:"id"`
	ProductID int64     `json:"productId" db:"product_id"`
	URL       string    `json:"url" db:"url"`
	Position  int       `json:"position" db:"position"`
	IsCover   bool      `json:"isCover" db:"is_cover"`
	CreatedAt time.Time `json:"createdAt" db:"created_at"`
}

const productImageSelect = `SELECT id, product_id, url, position, is_cover, created_at FROM product_images`

// ProductFilter controls filtering for ListProducts.
type ProductFilter struct {
	Search        string
	Category      string
	PublishedOnly bool
}

const productSelect = `SELECT id, slug, name, description, body, price_cents, image, category, status, sort_order, kind, max_downloads, created_at, updated_at FROM products`

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
	if p.Kind == "" {
		p.Kind = "physical"
	}
	return s.pool.QueryRow(ctx, `
		INSERT INTO products (slug, name, description, body, price_cents, image, category, status, sort_order, kind, max_downloads)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING id, created_at, updated_at`,
		p.Slug, p.Name, p.Description, p.Body, p.PriceCents, p.Image, p.Category, p.Status, p.SortOrder, p.Kind, p.MaxDownloads,
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
	if p.Kind == "" {
		p.Kind = "physical"
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE products
		SET slug=$1, name=$2, description=$3, body=$4, price_cents=$5, image=$6,
		    category=$7, status=$8, sort_order=$9, kind=$10, max_downloads=$11, updated_at=now()
		WHERE id=$12`,
		p.Slug, p.Name, p.Description, p.Body, p.PriceCents, p.Image,
		p.Category, p.Status, p.SortOrder, p.Kind, p.MaxDownloads, p.ID)
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

// ListProductImages returns every image attached to a product, ordered
// for gallery display (position asc, cover first when positions tie).
func (s *Store) ListProductImages(ctx context.Context, productID int64) ([]ProductImage, error) {
	return queryRows[ProductImage](ctx, s.pool,
		productImageSelect+` WHERE product_id = $1 ORDER BY is_cover DESC, position ASC, id ASC`,
		productID)
}

// AttachProductImages loads images for each product and attaches them
// to the Images field. Single query for the whole set, suitable for
// admin list pages; public listings should keep using the denormalised
// Image column instead.
func (s *Store) AttachProductImages(ctx context.Context, products []Product) error {
	if len(products) == 0 {
		return nil
	}
	ids := make([]int64, len(products))
	for i, p := range products {
		ids[i] = p.ID
	}
	images, err := queryRows[ProductImage](ctx, s.pool,
		productImageSelect+` WHERE product_id = ANY($1) ORDER BY is_cover DESC, position ASC, id ASC`,
		ids)
	if err != nil {
		return err
	}
	byProduct := map[int64][]ProductImage{}
	for _, img := range images {
		byProduct[img.ProductID] = append(byProduct[img.ProductID], img)
	}
	for i := range products {
		products[i].Images = byProduct[products[i].ID]
		if products[i].Images == nil {
			products[i].Images = []ProductImage{}
		}
	}
	return nil
}

// AddProductImage appends a new image to a product. If the product has
// no cover image yet, the new image becomes the cover and the parent
// product's Image column is updated to match.
func (s *Store) AddProductImage(ctx context.Context, productID int64, url string) (*ProductImage, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var nextPos int
	if err := tx.QueryRow(ctx,
		`SELECT COALESCE(MAX(position), -1) + 1 FROM product_images WHERE product_id = $1`,
		productID).Scan(&nextPos); err != nil {
		return nil, err
	}
	var hasCover bool
	if err := tx.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM product_images WHERE product_id = $1 AND is_cover = TRUE)`,
		productID).Scan(&hasCover); err != nil {
		return nil, err
	}

	img := &ProductImage{ProductID: productID, URL: url, Position: nextPos, IsCover: !hasCover}
	if err := tx.QueryRow(ctx, `
		INSERT INTO product_images (product_id, url, position, is_cover)
		VALUES ($1, $2, $3, $4)
		RETURNING id, created_at`,
		img.ProductID, img.URL, img.Position, img.IsCover,
	).Scan(&img.ID, &img.CreatedAt); err != nil {
		return nil, err
	}
	if img.IsCover {
		if _, err := tx.Exec(ctx,
			`UPDATE products SET image = $1, updated_at = now() WHERE id = $2`,
			img.URL, productID); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return img, nil
}

// SetProductImageOrder rewrites image positions to match the given
// ordering. IDs not in orderedIDs are appended to the end in their
// previous order so a partial reorder doesn't lose anything.
func (s *Store) SetProductImageOrder(ctx context.Context, productID int64, orderedIDs []int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for i, id := range orderedIDs {
		tag, err := tx.Exec(ctx,
			`UPDATE product_images SET position = $1 WHERE id = $2 AND product_id = $3`,
			i, id, productID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
	}
	return tx.Commit(ctx)
}

// SetProductCoverImage marks the given image as cover (clearing the
// previous cover) and updates the parent product's Image column.
func (s *Store) SetProductCoverImage(ctx context.Context, productID, imageID int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var url string
	if err := tx.QueryRow(ctx,
		`SELECT url FROM product_images WHERE id = $1 AND product_id = $2`,
		imageID, productID).Scan(&url); err != nil {
		if err == pgx.ErrNoRows {
			return ErrNotFound
		}
		return err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE product_images SET is_cover = FALSE WHERE product_id = $1 AND is_cover = TRUE`,
		productID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE product_images SET is_cover = TRUE WHERE id = $1`, imageID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE products SET image = $1, updated_at = now() WHERE id = $2`,
		url, productID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// --- Digital downloads ---

// ListProductDownloads returns every downloadable file attached to a
// digital product. Safe to call for physical products; returns empty.
func (s *Store) ListProductDownloads(ctx context.Context, productID int64) ([]ProductDownload, error) {
	return queryRows[ProductDownload](ctx, s.pool,
		productDownloadSelect+` WHERE product_id = $1 ORDER BY position ASC, id ASC`,
		productID)
}

// AddProductDownload attaches an already-uploaded file URL (returned
// by /api/admin/upload-file) to a product.
func (s *Store) AddProductDownload(ctx context.Context, d *ProductDownload) error {
	var nextPos int
	if err := s.pool.QueryRow(ctx,
		`SELECT COALESCE(MAX(position), -1) + 1 FROM product_downloads WHERE product_id = $1`,
		d.ProductID).Scan(&nextPos); err != nil {
		return err
	}
	d.Position = nextPos
	return s.pool.QueryRow(ctx, `
		INSERT INTO product_downloads (product_id, url, label, size_bytes, position)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at`,
		d.ProductID, d.URL, d.Label, d.SizeBytes, d.Position,
	).Scan(&d.ID, &d.CreatedAt)
}

// SetProductDownloadOrder rewrites positions to match the given ordering.
func (s *Store) SetProductDownloadOrder(ctx context.Context, productID int64, orderedIDs []int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for i, id := range orderedIDs {
		tag, err := tx.Exec(ctx,
			`UPDATE product_downloads SET position = $1 WHERE id = $2 AND product_id = $3`,
			i, id, productID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
	}
	return tx.Commit(ctx)
}

// DeleteProductDownload removes one file row. The underlying file in
// the uploads directory is left in place — keeping it lets us recover
// from accidental deletes; a separate sweep can purge orphans later.
func (s *Store) DeleteProductDownload(ctx context.Context, productID, downloadID int64) error {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM product_downloads WHERE id = $1 AND product_id = $2`,
		downloadID, productID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteProductImage removes one image. If it was the cover, the next
// image by position is promoted to cover (and the product's Image
// column is updated). If no images remain, products.image is cleared.
func (s *Store) DeleteProductImage(ctx context.Context, productID, imageID int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var wasCover bool
	if err := tx.QueryRow(ctx,
		`SELECT is_cover FROM product_images WHERE id = $1 AND product_id = $2`,
		imageID, productID).Scan(&wasCover); err != nil {
		if err == pgx.ErrNoRows {
			return ErrNotFound
		}
		return err
	}
	if _, err := tx.Exec(ctx,
		`DELETE FROM product_images WHERE id = $1`, imageID); err != nil {
		return err
	}
	if wasCover {
		// Promote the next image by position to cover (if any).
		var newCoverID int64
		var newCoverURL string
		err := tx.QueryRow(ctx, `
			SELECT id, url FROM product_images
			WHERE product_id = $1
			ORDER BY position ASC, id ASC LIMIT 1`,
			productID).Scan(&newCoverID, &newCoverURL)
		switch err {
		case nil:
			if _, err := tx.Exec(ctx,
				`UPDATE product_images SET is_cover = TRUE WHERE id = $1`, newCoverID); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx,
				`UPDATE products SET image = $1, updated_at = now() WHERE id = $2`,
				newCoverURL, productID); err != nil {
				return err
			}
		case pgx.ErrNoRows:
			// No images left — clear the cover URL.
			if _, err := tx.Exec(ctx,
				`UPDATE products SET image = '', updated_at = now() WHERE id = $1`,
				productID); err != nil {
				return err
			}
		default:
			return err
		}
	}
	return tx.Commit(ctx)
}
