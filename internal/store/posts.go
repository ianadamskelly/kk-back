package store

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Post is a single Insights / blog article.
type Post struct {
	ID           int64      `json:"id" db:"id"`
	Title        string     `json:"title" db:"title"`
	Slug         string     `json:"slug" db:"slug"`
	Excerpt      string     `json:"excerpt" db:"excerpt"`
	Content      string     `json:"content" db:"content"`
	CoverImage   string     `json:"coverImage" db:"cover_image"`
	Status       string     `json:"status" db:"status"`
	CategoryID   *int64     `json:"categoryId" db:"category_id"`
	CategoryName string     `json:"categoryName" db:"category_name"`
	CategorySlug string     `json:"categorySlug" db:"category_slug"`
	AuthorID     *int64     `json:"authorId" db:"author_id"`
	AuthorName   string     `json:"authorName" db:"author_name"`
	CreatedAt    time.Time  `json:"createdAt" db:"created_at"`
	UpdatedAt    time.Time  `json:"updatedAt" db:"updated_at"`
	PublishedAt  *time.Time `json:"publishedAt" db:"published_at"`
}

// ListOptions controls filtering and pagination for ListPosts.
type ListOptions struct {
	Search        string
	CategorySlug  string
	PublishedOnly bool
	Page          int
	PerPage       int
}

// PostList is a paginated set of posts.
type PostList struct {
	Posts   []Post `json:"posts"`
	Total   int    `json:"total"`
	Page    int    `json:"page"`
	PerPage int    `json:"perPage"`
	Pages   int    `json:"pages"`
}

const postSelect = `
SELECT p.id, p.title, p.slug, p.excerpt, p.content, p.cover_image, p.status,
       p.category_id, COALESCE(c.name, '') AS category_name, COALESCE(c.slug, '') AS category_slug,
       p.author_id, COALESCE(u.name, '') AS author_name,
       p.created_at, p.updated_at, p.published_at
FROM posts p
LEFT JOIN categories c ON c.id = p.category_id
LEFT JOIN users u ON u.id = p.author_id`

// ListPosts returns a filtered, paginated set of posts.
func (s *Store) ListPosts(ctx context.Context, opts ListOptions) (*PostList, error) {
	conds := []string{}
	args := []any{}
	if opts.PublishedOnly {
		conds = append(conds, "p.status = 'published'")
	}
	if opts.Search != "" {
		args = append(args, "%"+opts.Search+"%")
		n := len(args)
		conds = append(conds, fmt.Sprintf(
			"(p.title ILIKE $%d OR p.excerpt ILIKE $%d OR p.content ILIKE $%d)", n, n, n))
	}
	if opts.CategorySlug != "" {
		args = append(args, opts.CategorySlug)
		conds = append(conds, fmt.Sprintf("c.slug = $%d", len(args)))
	}
	where := "TRUE"
	if len(conds) > 0 {
		where = strings.Join(conds, " AND ")
	}

	var total int
	countSQL := `SELECT COUNT(*) FROM posts p
		LEFT JOIN categories c ON c.id = p.category_id WHERE ` + where
	if err := s.pool.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, err
	}

	if opts.PerPage <= 0 {
		opts.PerPage = 10
	}
	if opts.Page <= 0 {
		opts.Page = 1
	}
	offset := (opts.Page - 1) * opts.PerPage

	args = append(args, opts.PerPage, offset)
	query := postSelect + ` WHERE ` + where +
		fmt.Sprintf(` ORDER BY COALESCE(p.published_at, p.created_at) DESC, p.id DESC LIMIT $%d OFFSET $%d`,
			len(args)-1, len(args))

	posts, err := queryRows[Post](ctx, s.pool, query, args...)
	if err != nil {
		return nil, err
	}

	pages := 0
	if total > 0 {
		pages = (total + opts.PerPage - 1) / opts.PerPage
	}
	return &PostList{Posts: posts, Total: total, Page: opts.Page, PerPage: opts.PerPage, Pages: pages}, nil
}

// GetPostByID returns a single post by its numeric id.
func (s *Store) GetPostByID(ctx context.Context, id int64) (*Post, error) {
	return queryOne[Post](ctx, s.pool, postSelect+` WHERE p.id = $1`, id)
}

// GetPostBySlug returns a single post by slug. When publishedOnly is true,
// draft posts are treated as not found.
func (s *Store) GetPostBySlug(ctx context.Context, slug string, publishedOnly bool) (*Post, error) {
	query := postSelect + ` WHERE p.slug = $1`
	if publishedOnly {
		query += ` AND p.status = 'published'`
	}
	return queryOne[Post](ctx, s.pool, query, slug)
}

// CreatePost inserts a new post, generating a unique slug.
func (s *Store) CreatePost(ctx context.Context, p *Post) error {
	base := p.Slug
	if base == "" {
		base = p.Title
	}
	slug, err := s.uniqueSlug(ctx, "posts", slugify(base), 0)
	if err != nil {
		return err
	}
	p.Slug = slug
	if p.Status != "published" {
		p.Status = "draft"
	}
	var publishedAt *time.Time
	if p.Status == "published" {
		now := time.Now().UTC()
		publishedAt = &now
	}
	err = s.pool.QueryRow(ctx, `
		INSERT INTO posts (title, slug, excerpt, content, cover_image, status, category_id, author_id, published_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, created_at, updated_at`,
		p.Title, p.Slug, p.Excerpt, p.Content, p.CoverImage, p.Status, p.CategoryID, p.AuthorID, publishedAt,
	).Scan(&p.ID, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return err
	}
	p.PublishedAt = publishedAt
	return nil
}

// UpdatePost saves changes to an existing post. The slug is kept stable.
func (s *Store) UpdatePost(ctx context.Context, p *Post) error {
	base := p.Slug
	if base == "" {
		base = p.Title
	}
	slug, err := s.uniqueSlug(ctx, "posts", slugify(base), p.ID)
	if err != nil {
		return err
	}
	p.Slug = slug
	if p.Status != "published" {
		p.Status = "draft"
	}
	var publishedAt *time.Time
	if p.Status == "published" {
		if p.PublishedAt != nil {
			publishedAt = p.PublishedAt
		} else {
			now := time.Now().UTC()
			publishedAt = &now
		}
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE posts
		SET title=$1, slug=$2, excerpt=$3, content=$4, cover_image=$5,
		    status=$6, category_id=$7, updated_at=now(), published_at=$8
		WHERE id=$9`,
		p.Title, p.Slug, p.Excerpt, p.Content, p.CoverImage, p.Status, p.CategoryID, publishedAt, p.ID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	p.PublishedAt = publishedAt
	return nil
}

// DeletePost removes a post by id.
func (s *Store) DeletePost(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM posts WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
