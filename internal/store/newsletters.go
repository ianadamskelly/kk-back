package store

import (
	"context"
	"time"
)

// Newsletter is one admin-composed email blast. AudienceTags selects a
// slice of subscribers; if AudienceAll is true, tags are ignored and the
// whole active list receives it.
type Newsletter struct {
	ID           int64      `json:"id" db:"id"`
	Subject      string     `json:"subject" db:"subject"`
	Body         string     `json:"body" db:"body"`
	AudienceTags []string   `json:"audienceTags" db:"audience_tags"`
	AudienceAll  bool       `json:"audienceAll" db:"audience_all"`
	Status       string     `json:"status" db:"status"`
	SentCount    int        `json:"sentCount" db:"sent_count"`
	SentAt       *time.Time `json:"sentAt" db:"sent_at"`
	CreatedBy    *int64     `json:"createdBy" db:"created_by"`
	CreatedAt    time.Time  `json:"createdAt" db:"created_at"`
	UpdatedAt    time.Time  `json:"updatedAt" db:"updated_at"`
}

const newsletterSelect = `SELECT id, subject, body, audience_tags, audience_all, status, sent_count, sent_at, created_by, created_at, updated_at FROM newsletters`

// ListNewsletters returns every newsletter, newest first.
func (s *Store) ListNewsletters(ctx context.Context) ([]Newsletter, error) {
	return queryRows[Newsletter](ctx, s.pool,
		newsletterSelect+` ORDER BY created_at DESC, id DESC`)
}

// GetNewsletterByID returns one newsletter.
func (s *Store) GetNewsletterByID(ctx context.Context, id int64) (*Newsletter, error) {
	return queryOne[Newsletter](ctx, s.pool,
		newsletterSelect+` WHERE id = $1`, id)
}

// CreateNewsletter inserts a draft. Caller is responsible for validating
// subject + audience selection.
func (s *Store) CreateNewsletter(ctx context.Context, n *Newsletter) error {
	if n.Status == "" {
		n.Status = "draft"
	}
	n.AudienceTags = normaliseTags(n.AudienceTags)
	return s.pool.QueryRow(ctx, `
		INSERT INTO newsletters (subject, body, audience_tags, audience_all, status, created_by)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, status, sent_count, created_at, updated_at`,
		n.Subject, n.Body, n.AudienceTags, n.AudienceAll, n.Status, n.CreatedBy,
	).Scan(&n.ID, &n.Status, &n.SentCount, &n.CreatedAt, &n.UpdatedAt)
}

// UpdateNewsletter saves draft edits. Sent newsletters are immutable and
// must be guarded by the caller.
func (s *Store) UpdateNewsletter(ctx context.Context, n *Newsletter) error {
	n.AudienceTags = normaliseTags(n.AudienceTags)
	tag, err := s.pool.Exec(ctx, `
		UPDATE newsletters
		SET subject=$1, body=$2, audience_tags=$3, audience_all=$4, updated_at=now()
		WHERE id=$5`,
		n.Subject, n.Body, n.AudienceTags, n.AudienceAll, n.ID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// MarkNewsletterSent records the count + timestamp after a successful send.
func (s *Store) MarkNewsletterSent(ctx context.Context, id int64, sentCount int) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE newsletters
		SET status='sent', sent_count=$1, sent_at=now(), updated_at=now()
		WHERE id=$2`, sentCount, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteNewsletter removes a draft. Caller should refuse delete on sent
// newsletters if you want them retained as history.
func (s *Store) DeleteNewsletter(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM newsletters WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
