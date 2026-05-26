package store

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Review is one rating + comment posted by a user about a product or
// course. Visible to the public only when status = 'published'.
type Review struct {
	ID         int64     `json:"id" db:"id"`
	UserID     int64     `json:"userId" db:"user_id"`
	EntityType string    `json:"entityType" db:"entity_type"` // "product" | "course"
	EntityID   int64     `json:"entityId" db:"entity_id"`
	Rating     int       `json:"rating" db:"rating"`
	Body       string    `json:"body" db:"body"`
	Status     string    `json:"status" db:"status"` // "pending" | "published" | "rejected"
	CreatedAt  time.Time `json:"createdAt" db:"created_at"`
	UpdatedAt  time.Time `json:"updatedAt" db:"updated_at"`

	// Filled in for public list endpoints — never returned by the
	// admin moderation list (which already has the full user row).
	AuthorName string `json:"authorName,omitempty" db:"-"`
}

// ReviewSummary is a small aggregate for product / course detail pages.
type ReviewSummary struct {
	AverageRating float64 `json:"averageRating"`
	Count         int     `json:"count"`
}

const reviewSelect = `SELECT id, user_id, entity_type, entity_id, rating, body, status, created_at, updated_at FROM reviews`

// HasUserPurchasedProduct returns true when the user has any confirmed
// or fulfilled order containing the product. Used to gate review
// creation on products (verified buyers only).
func (s *Store) HasUserPurchasedProduct(ctx context.Context, userID, productID int64) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM order_items oi
			JOIN orders o ON o.id = oi.order_id
			WHERE oi.product_id = $1
			  AND o.user_id    = $2
			  AND o.status     IN ('confirmed', 'fulfilled')
		)`, productID, userID).Scan(&exists)
	return exists, err
}

// HasUserEnrolledInCourse returns true when the user has paid for the
// course OR has an active membership that grants access. (Mirrors the
// same logic as the lesson access gate.)
func (s *Store) HasUserEnrolledInCourse(ctx context.Context, userID, courseID int64) (bool, error) {
	// Paid for the course directly.
	var bought bool
	if err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM order_items oi
			JOIN orders o ON o.id = oi.order_id
			WHERE oi.course_id = $1
			  AND o.user_id    = $2
			  AND o.status     IN ('confirmed', 'fulfilled')
		)`, courseID, userID).Scan(&bought); err != nil {
		return false, err
	}
	if bought {
		return true, nil
	}
	// Active membership unlocks the whole catalogue.
	var member bool
	if err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM memberships
			WHERE user_id    = $1
			  AND status     = 'active'
			  AND (ends_at IS NULL OR ends_at > now())
		)`, userID).Scan(&member); err != nil {
		return false, err
	}
	return member, nil
}

// ListReviewsForEntity returns published reviews for the given entity,
// newest first. Caller supplies entityType ("product" / "course").
// Joins users for the author name so the public list can show it
// without a second round-trip.
func (s *Store) ListReviewsForEntity(ctx context.Context, entityType string, entityID int64) ([]Review, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT r.id, r.user_id, r.entity_type, r.entity_id, r.rating, r.body,
		       r.status, r.created_at, r.updated_at,
		       u.name AS author_name
		FROM reviews r
		JOIN users u ON u.id = r.user_id
		WHERE r.entity_type = $1
		  AND r.entity_id   = $2
		  AND r.status      = 'published'
		ORDER BY r.created_at DESC, r.id DESC`,
		entityType, entityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Review{}
	for rows.Next() {
		var r Review
		if err := rows.Scan(&r.ID, &r.UserID, &r.EntityType, &r.EntityID, &r.Rating, &r.Body,
			&r.Status, &r.CreatedAt, &r.UpdatedAt, &r.AuthorName); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ReviewSummaryForEntity returns the average and count of published reviews.
func (s *Store) ReviewSummaryForEntity(ctx context.Context, entityType string, entityID int64) (*ReviewSummary, error) {
	out := &ReviewSummary{}
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(AVG(rating), 0)::float, COUNT(*)::int
		FROM reviews
		WHERE entity_type = $1 AND entity_id = $2 AND status = 'published'`,
		entityType, entityID).Scan(&out.AverageRating, &out.Count)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// GetUserReview returns the user's existing review for the entity, if any.
func (s *Store) GetUserReview(ctx context.Context, userID int64, entityType string, entityID int64) (*Review, error) {
	r, err := queryOne[Review](ctx, s.pool,
		reviewSelect+` WHERE user_id = $1 AND entity_type = $2 AND entity_id = $3`,
		userID, entityType, entityID)
	if err != nil {
		return nil, err
	}
	return r, nil
}

// UpsertReview inserts a new review or updates an existing one for the
// same (user, entity). Re-editing always knocks the review back to
// 'pending' so an admin can re-moderate after meaningful changes.
func (s *Store) UpsertReview(ctx context.Context, r *Review) error {
	return s.pool.QueryRow(ctx, `
		INSERT INTO reviews (user_id, entity_type, entity_id, rating, body, status)
		VALUES ($1, $2, $3, $4, $5, 'pending')
		ON CONFLICT (user_id, entity_type, entity_id) DO UPDATE
		    SET rating     = EXCLUDED.rating,
		        body       = EXCLUDED.body,
		        status     = 'pending',
		        updated_at = now()
		RETURNING id, status, created_at, updated_at`,
		r.UserID, r.EntityType, r.EntityID, r.Rating, r.Body,
	).Scan(&r.ID, &r.Status, &r.CreatedAt, &r.UpdatedAt)
}

// DeleteOwnReview removes a review only if it belongs to the given user.
func (s *Store) DeleteOwnReview(ctx context.Context, userID, reviewID int64) error {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM reviews WHERE id = $1 AND user_id = $2`,
		reviewID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// AdminListReviews returns reviews for the moderation panel. Optional
// status filter (empty = all). Joins user name + the entity title so
// the admin sees what they're moderating without extra round-trips.
type AdminReview struct {
	Review
	AuthorEmail string `json:"authorEmail"`
	EntityName  string `json:"entityName"`
}

func (s *Store) AdminListReviews(ctx context.Context, statusFilter string) ([]AdminReview, error) {
	conds := []string{}
	args := []any{}
	if statusFilter != "" {
		args = append(args, statusFilter)
		conds = append(conds, fmt.Sprintf("r.status = $%d", len(args)))
	}
	where := ""
	if len(conds) > 0 {
		where = " WHERE " + strings.Join(conds, " AND ")
	}
	rows, err := s.pool.Query(ctx, `
		SELECT r.id, r.user_id, r.entity_type, r.entity_id, r.rating, r.body,
		       r.status, r.created_at, r.updated_at,
		       u.name AS author_name, u.email AS author_email,
		       COALESCE(
		           CASE WHEN r.entity_type = 'product' THEN p.name END,
		           CASE WHEN r.entity_type = 'course'  THEN c.title END,
		           ''
		       ) AS entity_name
		FROM reviews r
		JOIN users u ON u.id = r.user_id
		LEFT JOIN products p ON r.entity_type = 'product' AND p.id = r.entity_id
		LEFT JOIN courses  c ON r.entity_type = 'course'  AND c.id = r.entity_id
		`+where+`
		ORDER BY r.created_at DESC, r.id DESC`,
		args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AdminReview{}
	for rows.Next() {
		var ar AdminReview
		if err := rows.Scan(&ar.ID, &ar.UserID, &ar.EntityType, &ar.EntityID, &ar.Rating, &ar.Body,
			&ar.Status, &ar.CreatedAt, &ar.UpdatedAt,
			&ar.AuthorName, &ar.AuthorEmail, &ar.EntityName); err != nil {
			return nil, err
		}
		out = append(out, ar)
	}
	return out, rows.Err()
}

// SetReviewStatus moves a review between pending / published / rejected
// (admin moderation).
func (s *Store) SetReviewStatus(ctx context.Context, reviewID int64, status string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE reviews SET status = $1, updated_at = now() WHERE id = $2`,
		status, reviewID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// AdminDeleteReview wipes any review by id, regardless of owner.
func (s *Store) AdminDeleteReview(ctx context.Context, reviewID int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM reviews WHERE id = $1`, reviewID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

