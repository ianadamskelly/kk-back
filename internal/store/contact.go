package store

import (
	"context"
	"time"
)

// ContactSubmission is a message sent through the contact form.
type ContactSubmission struct {
	ID        int64     `json:"id" db:"id"`
	Name      string    `json:"name" db:"name"`
	Email     string    `json:"email" db:"email"`
	Phone     string    `json:"phone" db:"phone"`
	Service   string    `json:"service" db:"service"`
	Subject   string    `json:"subject" db:"subject"`
	Message   string    `json:"message" db:"message"`
	Status    string    `json:"status" db:"status"`
	CreatedAt time.Time `json:"createdAt" db:"created_at"`
}

const submissionSelect = `SELECT id, name, email, phone, service, subject, message, status, created_at FROM contact_submissions`

// CreateSubmission stores a new contact form message.
func (s *Store) CreateSubmission(ctx context.Context, c *ContactSubmission) error {
	return s.pool.QueryRow(ctx, `
		INSERT INTO contact_submissions (name, email, phone, service, subject, message)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, status, created_at`,
		c.Name, c.Email, c.Phone, c.Service, c.Subject, c.Message,
	).Scan(&c.ID, &c.Status, &c.CreatedAt)
}

// ListSubmissions returns every contact submission, newest first.
func (s *Store) ListSubmissions(ctx context.Context) ([]ContactSubmission, error) {
	return queryRows[ContactSubmission](ctx, s.pool,
		submissionSelect+` ORDER BY created_at DESC, id DESC`)
}

// UpdateSubmissionStatus changes the triage status of a submission.
func (s *Store) UpdateSubmissionStatus(ctx context.Context, id int64, status string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE contact_submissions SET status = $1 WHERE id = $2`, status, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteSubmission removes a submission by id.
func (s *Store) DeleteSubmission(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM contact_submissions WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
