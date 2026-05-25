package store

import (
	"context"
	"time"
)

// Testimonial is a client quote shown on the homepage. user_id is set
// when the quote was submitted via the customer dashboard; source
// distinguishes those from admin-typed entries.
type Testimonial struct {
	ID          int64      `json:"id" db:"id"`
	Author      string     `json:"author" db:"author"`
	Role        string     `json:"role" db:"role"`
	Company     string     `json:"company" db:"company"`
	Quote       string     `json:"quote" db:"quote"`
	Avatar      string     `json:"avatar" db:"avatar"`
	SortOrder   int        `json:"sortOrder" db:"sort_order"`
	Status      string     `json:"status" db:"status"`
	UserID      *int64     `json:"userId" db:"user_id"`
	Source      string     `json:"source" db:"source"`
	SubmittedAt *time.Time `json:"submittedAt" db:"submitted_at"`
}

const testimonialSelect = `SELECT id, author, role, company, quote, avatar, sort_order, status, user_id, source, submitted_at FROM testimonials`

// ListTestimonials returns testimonials ordered for display. publishedOnly
// is what the public site uses; the admin sees everything.
func (s *Store) ListTestimonials(ctx context.Context, publishedOnly bool) ([]Testimonial, error) {
	q := testimonialSelect
	if publishedOnly {
		q += ` WHERE status = 'published'`
	}
	q += ` ORDER BY status DESC, sort_order, id`
	return queryRows[Testimonial](ctx, s.pool, q)
}

// GetTestimonial returns one testimonial by id.
func (s *Store) GetTestimonial(ctx context.Context, id int64) (*Testimonial, error) {
	return queryOne[Testimonial](ctx, s.pool, testimonialSelect+` WHERE id = $1`, id)
}

// CreateTestimonial inserts a new testimonial. Admin-typed entries
// default to published; customer-submitted entries (source='customer')
// should be created with status='pending' so an admin reviews before
// they appear on the public site.
func (s *Store) CreateTestimonial(ctx context.Context, t *Testimonial) error {
	if t.Status == "" {
		t.Status = "published"
	}
	if t.Source == "" {
		t.Source = "admin"
	}
	return s.pool.QueryRow(ctx, `
		INSERT INTO testimonials (author, role, company, quote, avatar, sort_order, status, user_id, source, submitted_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id`,
		t.Author, t.Role, t.Company, t.Quote, t.Avatar, t.SortOrder, t.Status,
		t.UserID, t.Source, t.SubmittedAt,
	).Scan(&t.ID)
}

// UpdateTestimonial saves changes to an existing testimonial.
func (s *Store) UpdateTestimonial(ctx context.Context, t *Testimonial) error {
	if t.Status == "" {
		t.Status = "published"
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE testimonials
		SET author=$1, role=$2, company=$3, quote=$4, avatar=$5, sort_order=$6, status=$7
		WHERE id=$8`,
		t.Author, t.Role, t.Company, t.Quote, t.Avatar, t.SortOrder, t.Status, t.ID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteTestimonial removes a testimonial by id.
func (s *Store) DeleteTestimonial(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM testimonials WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ListUserTestimonials returns the testimonials a single user has
// submitted (any status) for their dashboard.
func (s *Store) ListUserTestimonials(ctx context.Context, userID int64) ([]Testimonial, error) {
	return queryRows[Testimonial](ctx, s.pool,
		testimonialSelect+` WHERE user_id = $1 ORDER BY submitted_at DESC, id DESC`,
		userID)
}

// CountPendingTestimonials powers the admin nav badge.
func (s *Store) CountPendingTestimonials(ctx context.Context) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM testimonials WHERE status = 'pending'`).Scan(&n)
	return n, err
}
