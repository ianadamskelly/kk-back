package store

import (
	"context"
	"time"
)

// Ticket is a customer-raised support thread (the customer dashboard
// labels them "Complaints" but they cover any kind of question).
type Ticket struct {
	ID           int64     `json:"id" db:"id"`
	UserID       int64     `json:"userId" db:"user_id"`
	Subject      string    `json:"subject" db:"subject"`
	Category     string    `json:"category" db:"category"`
	Status       string    `json:"status" db:"status"`
	LastReplyAt  time.Time `json:"lastReplyAt" db:"last_reply_at"`
	CreatedAt    time.Time `json:"createdAt" db:"created_at"`
	UpdatedAt    time.Time `json:"updatedAt" db:"updated_at"`
	UserName     string    `json:"userName" db:"user_name"`
	UserEmail    string    `json:"userEmail" db:"user_email"`
	MessageCount int       `json:"messageCount" db:"message_count"`
}

// TicketMessage is one entry in a ticket thread.
type TicketMessage struct {
	ID         int64     `json:"id" db:"id"`
	TicketID   int64     `json:"ticketId" db:"ticket_id"`
	AuthorID   *int64    `json:"authorId" db:"author_id"`
	AuthorRole string    `json:"authorRole" db:"author_role"`
	AuthorName string    `json:"authorName" db:"author_name"`
	Body       string    `json:"body" db:"body"`
	CreatedAt  time.Time `json:"createdAt" db:"created_at"`
}

const ticketSelect = `
	SELECT t.id, t.user_id, t.subject, t.category, t.status,
	       t.last_reply_at, t.created_at, t.updated_at,
	       u.name AS user_name, u.email AS user_email,
	       (SELECT COUNT(*) FROM ticket_messages WHERE ticket_id = t.id) AS message_count
	FROM tickets t
	JOIN users u ON u.id = t.user_id`

const ticketMessageSelect = `
	SELECT id, ticket_id, author_id, author_role, author_name, body, created_at
	FROM ticket_messages`

// CreateTicket inserts the ticket + the first message in one transaction.
// The first message is the body of the complaint/question itself.
func (s *Store) CreateTicket(ctx context.Context, t *Ticket, firstBody, authorName string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if t.Category == "" {
		t.Category = "general"
	}
	if t.Status == "" {
		t.Status = "open"
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO tickets (user_id, subject, category, status)
		VALUES ($1, $2, $3, $4)
		RETURNING id, last_reply_at, created_at, updated_at`,
		t.UserID, t.Subject, t.Category, t.Status,
	).Scan(&t.ID, &t.LastReplyAt, &t.CreatedAt, &t.UpdatedAt); err != nil {
		return err
	}
	if firstBody != "" {
		uid := t.UserID
		if _, err := tx.Exec(ctx, `
			INSERT INTO ticket_messages (ticket_id, author_id, author_role, author_name, body)
			VALUES ($1, $2, 'customer', $3, $4)`,
			t.ID, uid, authorName, firstBody); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// AddTicketMessage appends a reply. authorRole is 'customer' or 'admin';
// when a customer replies we flip status back to open, when admin
// replies we flip to 'replied' (waiting on customer).
func (s *Store) AddTicketMessage(ctx context.Context, m *TicketMessage) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if err := tx.QueryRow(ctx, `
		INSERT INTO ticket_messages (ticket_id, author_id, author_role, author_name, body)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at`,
		m.TicketID, m.AuthorID, m.AuthorRole, m.AuthorName, m.Body,
	).Scan(&m.ID, &m.CreatedAt); err != nil {
		return err
	}
	newStatus := "open"
	if m.AuthorRole == "admin" {
		newStatus = "replied"
	}
	if _, err := tx.Exec(ctx, `
		UPDATE tickets SET status = $1, last_reply_at = now(), updated_at = now()
		WHERE id = $2`, newStatus, m.TicketID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// SetTicketStatus flips status without recording a message — used by the
// "close" / "reopen" buttons.
func (s *Store) SetTicketStatus(ctx context.Context, id int64, status string) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE tickets SET status = $1, updated_at = now() WHERE id = $2`,
		status, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ListUserTickets returns the tickets owned by one user, newest reply first.
func (s *Store) ListUserTickets(ctx context.Context, userID int64) ([]Ticket, error) {
	return queryRows[Ticket](ctx, s.pool,
		ticketSelect+` WHERE t.user_id = $1 ORDER BY t.last_reply_at DESC`, userID)
}

// ListAdminTickets returns every ticket for the staff inbox.
func (s *Store) ListAdminTickets(ctx context.Context) ([]Ticket, error) {
	return queryRows[Ticket](ctx, s.pool,
		ticketSelect+` ORDER BY t.last_reply_at DESC`)
}

// GetTicket returns one ticket, scoped by user when userID > 0 (so a
// customer can't read another customer's ticket).
func (s *Store) GetTicket(ctx context.Context, id, userID int64) (*Ticket, error) {
	if userID > 0 {
		return queryOne[Ticket](ctx, s.pool,
			ticketSelect+` WHERE t.id = $1 AND t.user_id = $2`, id, userID)
	}
	return queryOne[Ticket](ctx, s.pool,
		ticketSelect+` WHERE t.id = $1`, id)
}

// ListTicketMessages returns the message thread, oldest first.
func (s *Store) ListTicketMessages(ctx context.Context, ticketID int64) ([]TicketMessage, error) {
	return queryRows[TicketMessage](ctx, s.pool,
		ticketMessageSelect+` WHERE ticket_id = $1 ORDER BY created_at, id`, ticketID)
}

// CountOpenTickets is used for the staff inbox badge.
func (s *Store) CountOpenTickets(ctx context.Context) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM tickets WHERE status = 'open'`).Scan(&n)
	return n, err
}
