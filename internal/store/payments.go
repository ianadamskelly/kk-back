package store

import (
	"context"
	"encoding/json"
	"time"
)

// Payment is a single payment attempt against an order through a gateway.
type Payment struct {
	ID           int64           `json:"id" db:"id"`
	OrderID      int64           `json:"orderId" db:"order_id"`
	Gateway      string          `json:"gateway" db:"gateway"`
	TxRef        string          `json:"txRef" db:"tx_ref"`
	ProviderTxID string          `json:"providerTxId" db:"provider_tx_id"`
	AmountCents  int64           `json:"amountCents" db:"amount_cents"`
	Currency     string          `json:"currency" db:"currency"`
	Status       string          `json:"status" db:"status"`
	RawResponse  json.RawMessage `json:"rawResponse" db:"raw_response"`
	CreatedAt    time.Time       `json:"createdAt" db:"created_at"`
	VerifiedAt   *time.Time      `json:"verifiedAt" db:"verified_at"`
}

const paymentSelect = `SELECT id, order_id, gateway, tx_ref, provider_tx_id, amount_cents, currency, status, raw_response, created_at, verified_at FROM payments`

// CreatePayment inserts a new payment attempt.
func (s *Store) CreatePayment(ctx context.Context, p *Payment) error {
	if p.Gateway == "" {
		p.Gateway = "flutterwave"
	}
	if p.Currency == "" {
		p.Currency = "KES"
	}
	if p.Status == "" {
		p.Status = "pending"
	}
	if len(p.RawResponse) == 0 {
		p.RawResponse = json.RawMessage("{}")
	}
	return s.pool.QueryRow(ctx, `
		INSERT INTO payments (order_id, gateway, tx_ref, provider_tx_id, amount_cents, currency, status, raw_response)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, created_at`,
		p.OrderID, p.Gateway, p.TxRef, p.ProviderTxID, p.AmountCents,
		p.Currency, p.Status, p.RawResponse,
	).Scan(&p.ID, &p.CreatedAt)
}

// GetPaymentByTxRef returns the payment with the given transaction reference.
func (s *Store) GetPaymentByTxRef(ctx context.Context, txRef string) (*Payment, error) {
	return queryOne[Payment](ctx, s.pool, paymentSelect+` WHERE tx_ref = $1`, txRef)
}

// GetLatestPaymentForOrder returns the most recent payment attempt for an order.
func (s *Store) GetLatestPaymentForOrder(ctx context.Context, orderID int64) (*Payment, error) {
	return queryOne[Payment](ctx, s.pool,
		paymentSelect+` WHERE order_id = $1 ORDER BY created_at DESC, id DESC LIMIT 1`, orderID)
}

// UpdatePayment persists changes to status, provider id, raw response, and verified_at.
func (s *Store) UpdatePayment(ctx context.Context, p *Payment) error {
	if len(p.RawResponse) == 0 {
		p.RawResponse = json.RawMessage("{}")
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE payments
		SET status = $1, provider_tx_id = $2, raw_response = $3, verified_at = $4
		WHERE id = $5`,
		p.Status, p.ProviderTxID, p.RawResponse, p.VerifiedAt, p.ID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
