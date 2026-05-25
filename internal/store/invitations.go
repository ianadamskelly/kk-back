package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"
	"time"
)

// Invitation is a single-use, time-limited URL that turns into a user
// account when the invitee sets a password.
type Invitation struct {
	ID         int64      `json:"id" db:"id"`
	Email      string     `json:"email" db:"email"`
	Name       string     `json:"name" db:"name"`
	RoleID     int64      `json:"roleId" db:"role_id"`
	RoleName   string     `json:"roleName" db:"role_name"`
	RoleKey    string     `json:"roleKey" db:"role_key"`
	Token      string     `json:"token" db:"token"`
	ExpiresAt  time.Time  `json:"expiresAt" db:"expires_at"`
	AcceptedAt *time.Time `json:"acceptedAt" db:"accepted_at"`
	CreatedBy  *int64     `json:"createdBy" db:"created_by"`
	CreatedAt  time.Time  `json:"createdAt" db:"created_at"`
}

const invitationSelectWithRole = `
	SELECT i.id, i.email, i.name, i.role_id, i.token, i.expires_at,
	       i.accepted_at, i.created_by, i.created_at,
	       r.name AS role_name, r.key AS role_key
	FROM invitations i
	JOIN roles r ON r.id = i.role_id`

// CreateInvitation makes a new invite for the given email + role. Any
// prior pending invite for the same email is revoked first so the admin
// can re-issue without confusion.
func (s *Store) CreateInvitation(ctx context.Context, inv *Invitation, ttl time.Duration) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Revoke any pending invite for this email.
	if _, err := tx.Exec(ctx, `
		DELETE FROM invitations WHERE email = $1 AND accepted_at IS NULL`,
		strings.ToLower(inv.Email),
	); err != nil {
		return err
	}

	token, err := newInviteToken()
	if err != nil {
		return err
	}
	inv.Token = token
	inv.Email = strings.ToLower(strings.TrimSpace(inv.Email))
	inv.ExpiresAt = time.Now().UTC().Add(ttl)

	if err := tx.QueryRow(ctx, `
		INSERT INTO invitations (email, name, role_id, token, expires_at, created_by)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, created_at`,
		inv.Email, inv.Name, inv.RoleID, inv.Token, inv.ExpiresAt, inv.CreatedBy,
	).Scan(&inv.ID, &inv.CreatedAt); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ListInvitations returns pending + recently-accepted invites for the admin
// inbox. Expired+unaccepted ones are filtered out by the caller if needed.
func (s *Store) ListInvitations(ctx context.Context) ([]Invitation, error) {
	return queryRows[Invitation](ctx, s.pool,
		invitationSelectWithRole+` ORDER BY i.created_at DESC`)
}

// GetInvitationByToken returns one invite by its URL token.
func (s *Store) GetInvitationByToken(ctx context.Context, token string) (*Invitation, error) {
	return queryOne[Invitation](ctx, s.pool,
		invitationSelectWithRole+` WHERE i.token = $1`, token)
}

// MarkInvitationAccepted stamps the row as redeemed.
func (s *Store) MarkInvitationAccepted(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE invitations SET accepted_at = now() WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteInvitation revokes a pending invite by id.
func (s *Store) DeleteInvitation(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM invitations WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func newInviteToken() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
