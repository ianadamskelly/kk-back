package store

import (
	"context"
	"time"
)

// Comment is a reader comment left on a post.
type Comment struct {
	ID         int64     `json:"id" db:"id"`
	PostID     int64     `json:"postId" db:"post_id"`
	AuthorName string    `json:"authorName" db:"author_name"`
	Body       string    `json:"body" db:"body"`
	CreatedAt  time.Time `json:"createdAt" db:"created_at"`
	// PostTitle and PostSlug are populated only for the admin listing.
	PostTitle string `json:"postTitle,omitempty" db:"post_title"`
	PostSlug  string `json:"postSlug,omitempty" db:"post_slug"`
}

// ListComments returns all comments for a post, newest first.
func (s *Store) ListComments(ctx context.Context, postID int64) ([]Comment, error) {
	return queryRows[Comment](ctx, s.pool,
		`SELECT id, post_id, author_name, body, created_at
		 FROM comments WHERE post_id = $1
		 ORDER BY created_at DESC, id DESC`, postID)
}

// ListAllComments returns every comment with its post's title and slug,
// for the admin moderation view.
func (s *Store) ListAllComments(ctx context.Context) ([]Comment, error) {
	return queryRows[Comment](ctx, s.pool,
		`SELECT c.id, c.post_id, c.author_name, c.body, c.created_at,
		        p.title AS post_title, p.slug AS post_slug
		 FROM comments c
		 JOIN posts p ON p.id = c.post_id
		 ORDER BY c.created_at DESC, c.id DESC`)
}

// CreateComment inserts a new comment and sets its ID and timestamp.
func (s *Store) CreateComment(ctx context.Context, c *Comment) error {
	return s.pool.QueryRow(ctx,
		`INSERT INTO comments (post_id, author_name, body)
		 VALUES ($1, $2, $3) RETURNING id, created_at`,
		c.PostID, c.AuthorName, c.Body,
	).Scan(&c.ID, &c.CreatedAt)
}

// DeleteComment removes a comment by id.
func (s *Store) DeleteComment(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM comments WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
