package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const reservationTTL = 30 * time.Minute

var (
	ErrReservationExpired     = errors.New("your checkout discount hold has expired; please check out again")
	ErrReservationUnavailable = errors.New("the reserved coupon or credit is no longer available")
)

// CreateOrderWithReservation computes discount availability and creates the
// order and its hold in one transaction. Live holds count against both coupon
// limits and credit balances until they are consumed or released.
func (s *Store) CreateOrderWithReservation(ctx context.Context, o *Order, items []OrderItem, couponCode string, requestedCredit int64, scope string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `
		UPDATE order_discount_reservations
		SET status = 'released', released_at = now()
		WHERE status = 'held' AND payment_started_at IS NULL AND expires_at <= now()`); err != nil {
		return err
	}

	if strings.TrimSpace(couponCode) != "" {
		c, err := couponForUpdate(ctx, tx, couponCode)
		if err == pgx.ErrNoRows {
			return fmt.Errorf("coupon not found")
		}
		if err != nil {
			return err
		}
		if err := validateCouponHold(ctx, tx, c, o.UserID, scope, o.SubtotalCents); err != nil {
			return err
		}
		o.CouponID = &c.ID
		o.CouponCode = c.Code
		o.DiscountCents = reservationDiscount(c, o.SubtotalCents)
	}

	if requestedCredit > 0 && o.UserID != nil {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, *o.UserID); err != nil {
			return err
		}
		available, err := availableCreditTx(ctx, tx, *o.UserID)
		if err != nil {
			return err
		}
		o.CreditCents = requestedCredit
		if o.CreditCents > available {
			o.CreditCents = available
		}
		maxCredit := o.SubtotalCents - o.DiscountCents
		if o.CreditCents > maxCredit {
			o.CreditCents = maxCredit
		}
		if o.CreditCents < 0 {
			o.CreditCents = 0
		}
	}
	o.TotalCents = o.SubtotalCents - o.DiscountCents - o.CreditCents
	if o.TotalCents < 0 {
		o.TotalCents = 0
	}
	if err := createOrderTx(ctx, tx, o, items); err != nil {
		return err
	}
	if o.DiscountCents > 0 || o.CreditCents > 0 {
		if _, err := tx.Exec(ctx, `
			INSERT INTO order_discount_reservations
			    (order_id, user_id, coupon_id, reserved_discount_cents, reserved_credit_cents, status, expires_at)
			VALUES ($1,$2,$3,$4,$5,'held',now() + interval '30 minutes')`,
			o.ID, o.UserID, o.CouponID, o.DiscountCents, o.CreditCents); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func createOrderTx(ctx context.Context, tx pgx.Tx, o *Order, items []OrderItem) error {
	kind := o.Kind
	if kind == "" {
		kind = "shop"
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO orders (user_id, kind, customer_name, customer_email, customer_phone, note,
			subtotal_cents, discount_cents, credit_cents, coupon_id, coupon_code, total_cents, status)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,'pending')
		RETURNING id, kind, status, created_at`,
		o.UserID, kind, o.CustomerName, o.CustomerEmail, o.CustomerPhone, o.Note,
		o.SubtotalCents, o.DiscountCents, o.CreditCents, o.CouponID, o.CouponCode, o.TotalCents,
	).Scan(&o.ID, &o.Kind, &o.Status, &o.CreatedAt); err != nil {
		return err
	}
	for i := range items {
		items[i].OrderID = o.ID
		if err := tx.QueryRow(ctx, `
			INSERT INTO order_items (order_id, product_id, course_id, product_name, unit_price_cents, quantity)
			VALUES ($1,$2,$3,$4,$5,$6) RETURNING id`,
			items[i].OrderID, items[i].ProductID, items[i].CourseID, items[i].ProductName,
			items[i].UnitPriceCents, items[i].Quantity).Scan(&items[i].ID); err != nil {
			return err
		}
	}
	o.Items = items
	return nil
}

func couponForUpdate(ctx context.Context, tx pgx.Tx, code string) (*Coupon, error) {
	var c Coupon
	err := tx.QueryRow(ctx, couponSelect+` WHERE LOWER(code) = LOWER($1) FOR UPDATE`, strings.TrimSpace(code)).Scan(
		&c.ID, &c.Code, &c.Description, &c.DiscountType, &c.PercentOff,
		&c.AmountOffCents, &c.Scope, &c.MinSubtotalCents, &c.MaxUses, &c.PerUserMaxUses,
		&c.UsedCount, &c.StartsAt, &c.ExpiresAt, &c.Active, &c.CreatedBy, &c.CreatedAt, &c.UpdatedAt)
	return &c, err
}

func couponByIDForUpdate(ctx context.Context, tx pgx.Tx, id int64) (*Coupon, error) {
	var c Coupon
	err := tx.QueryRow(ctx, couponSelect+` WHERE id = $1 FOR UPDATE`, id).Scan(
		&c.ID, &c.Code, &c.Description, &c.DiscountType, &c.PercentOff,
		&c.AmountOffCents, &c.Scope, &c.MinSubtotalCents, &c.MaxUses, &c.PerUserMaxUses,
		&c.UsedCount, &c.StartsAt, &c.ExpiresAt, &c.Active, &c.CreatedBy, &c.CreatedAt, &c.UpdatedAt)
	return &c, err
}

func validateCouponHold(ctx context.Context, tx pgx.Tx, c *Coupon, userID *int64, scope string, subtotal int64) error {
	now := time.Now().UTC()
	switch {
	case !c.Active:
		return fmt.Errorf("this coupon is no longer active")
	case c.StartsAt != nil && now.Before(*c.StartsAt):
		return fmt.Errorf("this coupon isn't valid yet")
	case c.ExpiresAt != nil && now.After(*c.ExpiresAt):
		return fmt.Errorf("this coupon has expired")
	case c.Scope != "all" && c.Scope != scope:
		return fmt.Errorf("this coupon doesn't apply to %s", scope)
	case c.MinSubtotalCents > 0 && subtotal < c.MinSubtotalCents:
		return fmt.Errorf("subtotal does not meet this coupon's minimum")
	}
	var holds int
	if err := tx.QueryRow(ctx, `
		SELECT COUNT(*) FROM order_discount_reservations
		WHERE coupon_id = $1 AND status = 'held'
		  AND (payment_started_at IS NOT NULL OR expires_at > now())`, c.ID).Scan(&holds); err != nil {
		return err
	}
	if c.MaxUses != nil && c.UsedCount+holds >= *c.MaxUses {
		return fmt.Errorf("this coupon has been fully redeemed")
	}
	if userID != nil && c.PerUserMaxUses != nil {
		var used, userHolds int
		if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM coupon_redemptions WHERE coupon_id = $1 AND user_id = $2`, c.ID, *userID).Scan(&used); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `
			SELECT COUNT(*) FROM order_discount_reservations
			WHERE coupon_id = $1 AND user_id = $2 AND status = 'held'
			  AND (payment_started_at IS NOT NULL OR expires_at > now())`, c.ID, *userID).Scan(&userHolds); err != nil {
			return err
		}
		if used+userHolds >= *c.PerUserMaxUses {
			return fmt.Errorf("you've already used this coupon the maximum number of times")
		}
	}
	if reservationDiscount(c, subtotal) <= 0 {
		return fmt.Errorf("this coupon doesn't reduce your total")
	}
	return nil
}

func reservationDiscount(c *Coupon, subtotal int64) int64 {
	var discount int64
	if c.DiscountType == "percent" {
		discount = subtotal * int64(c.PercentOff) / 100
	} else {
		discount = c.AmountOffCents
	}
	if discount > subtotal {
		discount = subtotal
	}
	if discount < 0 {
		return 0
	}
	return discount
}

func availableCreditTx(ctx context.Context, tx pgx.Tx, userID int64) (int64, error) {
	var balance, held int64
	if err := tx.QueryRow(ctx, `SELECT COALESCE(SUM(amount_cents), 0) FROM credit_transactions WHERE user_id = $1`, userID).Scan(&balance); err != nil {
		return 0, err
	}
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(SUM(reserved_credit_cents), 0)
		FROM order_discount_reservations
		WHERE user_id = $1 AND status = 'held'
		  AND (payment_started_at IS NOT NULL OR expires_at > now())`, userID).Scan(&held); err != nil {
		return 0, err
	}
	if balance <= held {
		return 0, nil
	}
	return balance - held, nil
}

// StartPayment inserts a payment attempt while converting an unexpired
// checkout hold into an in-progress hold that does not time out mid-payment.
func (s *Store) StartPayment(ctx context.Context, p *Payment) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var discount, credit int64
	var orderStatus string
	if err := tx.QueryRow(ctx, `SELECT discount_cents, credit_cents, status FROM orders WHERE id = $1 FOR UPDATE`, p.OrderID).Scan(&discount, &credit, &orderStatus); err != nil {
		return err
	}
	if orderStatus != "pending" {
		return ErrReservationUnavailable
	}
	if discount > 0 || credit > 0 {
		var status string
		var expires time.Time
		var started *time.Time
		err := tx.QueryRow(ctx, `
			SELECT status, expires_at, payment_started_at
			FROM order_discount_reservations WHERE order_id = $1 FOR UPDATE`, p.OrderID).Scan(&status, &expires, &started)
		if err == pgx.ErrNoRows || status != "held" {
			return ErrReservationExpired
		}
		if err != nil {
			return err
		}
		if started == nil && !expires.After(time.Now().UTC()) {
			if _, err := tx.Exec(ctx, `
				UPDATE order_discount_reservations SET status = 'released', released_at = now()
				WHERE order_id = $1`, p.OrderID); err != nil {
				return err
			}
			if err := tx.Commit(ctx); err != nil {
				return err
			}
			return ErrReservationExpired
		}
		if _, err := tx.Exec(ctx, `
			UPDATE order_discount_reservations
			SET payment_started_at = COALESCE(payment_started_at, now())
			WHERE order_id = $1`, p.OrderID); err != nil {
			return err
		}
	}
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
	if err := tx.QueryRow(ctx, `
		INSERT INTO payments (order_id, gateway, tx_ref, provider_tx_id, amount_cents, currency, status, raw_response)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		RETURNING id, created_at`,
		p.OrderID, p.Gateway, p.TxRef, p.ProviderTxID, p.AmountCents, p.Currency, p.Status, p.RawResponse).Scan(&p.ID, &p.CreatedAt); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// FinalizeSuccessfulPayment records provider settlement and confirms an order
// once. reviewed is true when money settled but protected value cannot safely
// be fulfilled automatically.
func (s *Store) FinalizeSuccessfulPayment(ctx context.Context, p *Payment) (newlyConfirmed bool, reviewed bool, err error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, false, err
	}
	defer tx.Rollback(ctx)
	if len(p.RawResponse) == 0 {
		p.RawResponse = json.RawMessage("{}")
	}
	if _, err := tx.Exec(ctx, `
		UPDATE payments SET status = 'successful', provider_tx_id = $1, raw_response = $2, verified_at = $3
		WHERE id = $4`, p.ProviderTxID, p.RawResponse, p.VerifiedAt, p.ID); err != nil {
		return false, false, err
	}
	newlyConfirmed, reviewed, err = confirmOrderTx(ctx, tx, p.OrderID, false, true)
	if err != nil {
		return false, false, err
	}
	return newlyConfirmed, reviewed, tx.Commit(ctx)
}

// ConfirmOrderManually confirms an offline-paid order, reacquiring an expired
// checkout reservation only when the quoted coupon and credit are still free.
func (s *Store) ConfirmOrderManually(ctx context.Context, orderID int64) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	confirmed, _, err := confirmOrderTx(ctx, tx, orderID, true, false)
	if err != nil {
		return false, err
	}
	return confirmed, tx.Commit(ctx)
}

func confirmOrderTx(ctx context.Context, tx pgx.Tx, orderID int64, allowReacquire, providerPaid bool) (bool, bool, error) {
	o, err := orderForUpdate(ctx, tx, orderID)
	if err != nil {
		return false, false, err
	}
	if o.Status == "confirmed" || o.Status == "fulfilled" {
		return false, false, nil
	}
	if providerPaid && o.Status != "pending" {
		if _, err := tx.Exec(ctx, `UPDATE orders SET status = 'payment_review' WHERE id = $1`, orderID); err != nil {
			return false, false, err
		}
		return false, true, nil
	}
	if o.DiscountCents > 0 || o.CreditCents > 0 {
		held, err := consumeHeldReservationTx(ctx, tx, o)
		if err != nil {
			return false, false, err
		}
		if !held && allowReacquire {
			if err := reacquireReservationTx(ctx, tx, o); err != nil {
				return false, false, err
			}
			held, err = consumeHeldReservationTx(ctx, tx, o)
			if err != nil {
				return false, false, err
			}
		}
		if !held {
			if providerPaid {
				if _, err := tx.Exec(ctx, `UPDATE orders SET status = 'payment_review' WHERE id = $1`, orderID); err != nil {
					return false, false, err
				}
				return false, true, nil
			}
			return false, false, ErrReservationUnavailable
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE orders SET status = 'confirmed' WHERE id = $1`, orderID); err != nil {
		return false, false, err
	}
	return true, false, nil
}

func orderForUpdate(ctx context.Context, tx pgx.Tx, id int64) (*Order, error) {
	var o Order
	err := tx.QueryRow(ctx, orderSelect+` WHERE id = $1 FOR UPDATE`, id).Scan(
		&o.ID, &o.UserID, &o.Kind, &o.CustomerName, &o.CustomerEmail, &o.CustomerPhone, &o.Note,
		&o.SubtotalCents, &o.DiscountCents, &o.CreditCents, &o.CouponID, &o.CouponCode,
		&o.TotalCents, &o.Status, &o.CreatedAt)
	return &o, err
}

func consumeHeldReservationTx(ctx context.Context, tx pgx.Tx, o *Order) (bool, error) {
	var status string
	err := tx.QueryRow(ctx, `SELECT status FROM order_discount_reservations WHERE order_id = $1 FOR UPDATE`, o.ID).Scan(&status)
	if err == pgx.ErrNoRows || status != "held" {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if o.CouponID != nil && o.DiscountCents > 0 {
		tag, err := tx.Exec(ctx, `
			INSERT INTO coupon_redemptions (coupon_id, user_id, order_id, amount_discounted_cents)
			VALUES ($1,$2,$3,$4)
			ON CONFLICT (order_id) WHERE order_id IS NOT NULL DO NOTHING`,
			*o.CouponID, o.UserID, o.ID, o.DiscountCents)
		if err != nil {
			return false, err
		}
		if tag.RowsAffected() > 0 {
			if _, err := tx.Exec(ctx, `UPDATE coupons SET used_count = used_count + 1, updated_at = now() WHERE id = $1`, *o.CouponID); err != nil {
				return false, err
			}
		}
	}
	if o.CreditCents > 0 && o.UserID != nil {
		if _, err := tx.Exec(ctx, `
			INSERT INTO credit_transactions (user_id, amount_cents, reason, related_order_id, note)
			VALUES ($1,$2,'order_spend',$3,'')
			ON CONFLICT (related_order_id) WHERE reason = 'order_spend' AND related_order_id IS NOT NULL DO NOTHING`,
			*o.UserID, -o.CreditCents, o.ID); err != nil {
			return false, err
		}
	}
	if _, err := tx.Exec(ctx, `
		UPDATE order_discount_reservations SET status = 'consumed', consumed_at = now()
		WHERE order_id = $1`, o.ID); err != nil {
		return false, err
	}
	return true, nil
}

func reacquireReservationTx(ctx context.Context, tx pgx.Tx, o *Order) error {
	if o.CouponID != nil && o.DiscountCents > 0 {
		c, err := couponByIDForUpdate(ctx, tx, *o.CouponID)
		if err != nil {
			return ErrReservationUnavailable
		}
		now := time.Now().UTC()
		if !c.Active || (c.StartsAt != nil && now.Before(*c.StartsAt)) || (c.ExpiresAt != nil && now.After(*c.ExpiresAt)) {
			return ErrReservationUnavailable
		}
		var holds int
		if err := tx.QueryRow(ctx, `
			SELECT COUNT(*) FROM order_discount_reservations
			WHERE coupon_id = $1 AND order_id <> $2 AND status = 'held'
			  AND (payment_started_at IS NOT NULL OR expires_at > now())`, *o.CouponID, o.ID).Scan(&holds); err != nil {
			return err
		}
		if c.MaxUses != nil && c.UsedCount+holds >= *c.MaxUses {
			return ErrReservationUnavailable
		}
		if o.UserID != nil && c.PerUserMaxUses != nil {
			var used, userHolds int
			if err := tx.QueryRow(ctx, `
				SELECT COUNT(*) FROM coupon_redemptions
				WHERE coupon_id = $1 AND user_id = $2`, *o.CouponID, *o.UserID).Scan(&used); err != nil {
				return err
			}
			if err := tx.QueryRow(ctx, `
				SELECT COUNT(*) FROM order_discount_reservations
				WHERE coupon_id = $1 AND user_id = $2 AND order_id <> $3 AND status = 'held'
				  AND (payment_started_at IS NOT NULL OR expires_at > now())`,
				*o.CouponID, *o.UserID, o.ID).Scan(&userHolds); err != nil {
				return err
			}
			if used+userHolds >= *c.PerUserMaxUses {
				return ErrReservationUnavailable
			}
		}
	}
	if o.CreditCents > 0 && o.UserID != nil {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, *o.UserID); err != nil {
			return err
		}
		available, err := availableCreditTx(ctx, tx, *o.UserID)
		if err != nil {
			return err
		}
		if available < o.CreditCents {
			return ErrReservationUnavailable
		}
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO order_discount_reservations
		    (order_id, user_id, coupon_id, reserved_discount_cents, reserved_credit_cents, status, expires_at)
		VALUES ($1,$2,$3,$4,$5,'held',now() + interval '30 minutes')
		ON CONFLICT (order_id) DO UPDATE SET status='held', expires_at=EXCLUDED.expires_at,
		    payment_started_at=NULL, consumed_at=NULL, released_at=NULL`,
		o.ID, o.UserID, o.CouponID, o.DiscountCents, o.CreditCents)
	return err
}

func (s *Store) ReleaseReservation(ctx context.Context, orderID int64) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE order_discount_reservations SET status = 'released', released_at = now()
		WHERE order_id = $1 AND status = 'held'`, orderID)
	return err
}
