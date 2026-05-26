package store

import (
	"context"
	"time"
)

// CreditTransaction is one entry in a user's credit ledger. Positive
// amounts add credit; negative amounts spend it. The current balance for
// a user is the sum of amount_cents across their rows.
type CreditTransaction struct {
	ID             int64     `json:"id" db:"id"`
	UserID         int64     `json:"userId" db:"user_id"`
	AmountCents    int64     `json:"amountCents" db:"amount_cents"`
	Reason         string    `json:"reason" db:"reason"`
	RelatedUserID  *int64    `json:"relatedUserId" db:"related_user_id"`
	RelatedOrderID *int64    `json:"relatedOrderId" db:"related_order_id"`
	Note           string    `json:"note" db:"note"`
	CreatedAt      time.Time `json:"createdAt" db:"created_at"`
}

const creditSelect = `SELECT id, user_id, amount_cents, reason, related_user_id, related_order_id, note, created_at FROM credit_transactions`

// GetCreditBalance returns the user's net store-credit balance in cents.
// Always returns 0 (not an error) when the user has no transactions.
func (s *Store) GetCreditBalance(ctx context.Context, userID int64) (int64, error) {
	var balance int64
	err := s.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(amount_cents), 0) FROM credit_transactions WHERE user_id = $1`,
		userID).Scan(&balance)
	return balance, err
}

// ListUserCreditTransactions returns the user's ledger, newest first.
func (s *Store) ListUserCreditTransactions(ctx context.Context, userID int64) ([]CreditTransaction, error) {
	return queryRows[CreditTransaction](ctx, s.pool,
		creditSelect+` WHERE user_id = $1 ORDER BY created_at DESC, id DESC`, userID)
}

// AddCreditTransaction writes one ledger entry. Use a positive amount for
// credit, negative for spend. Caller is responsible for not letting the
// balance go negative (use HasEnoughCredit first).
func (s *Store) AddCreditTransaction(ctx context.Context, tx *CreditTransaction) error {
	return s.pool.QueryRow(ctx, `
		INSERT INTO credit_transactions (user_id, amount_cents, reason, related_user_id, related_order_id, note)
		VALUES ($1,$2,$3,$4,$5,$6) RETURNING id, created_at`,
		tx.UserID, tx.AmountCents, tx.Reason, tx.RelatedUserID, tx.RelatedOrderID, tx.Note,
	).Scan(&tx.ID, &tx.CreatedAt)
}

// HasOrderSpend reports whether a credit "order_spend" row already
// exists for the given order. Used by recordOrderDiscounts to stay
// idempotent across the "gateway verified" and "admin marked
// confirmed" paths. (The partial UNIQUE index in migration 0021 is
// the hard backstop; this helper avoids the round-trip into Postgres
// and a noisy 23505 in the application logs.)
func (s *Store) HasOrderSpend(ctx context.Context, orderID int64) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS (
		    SELECT 1 FROM credit_transactions
		    WHERE related_order_id = $1 AND reason = 'order_spend'
		)`, orderID).Scan(&exists)
	return exists, err
}

// RecentCreditTransactions returns the latest N entries across all users —
// used by the admin /rewards page.
func (s *Store) RecentCreditTransactions(ctx context.Context, limit int) ([]CreditTransaction, error) {
	if limit <= 0 {
		limit = 25
	}
	return queryRows[CreditTransaction](ctx, s.pool,
		creditSelect+` ORDER BY created_at DESC LIMIT $1`, limit)
}
