package store

import (
	"context"
	"strings"
	"time"
)

// Coupon is a promotional code that discounts an order. discount_type is
// 'percent' (PercentOff 1-100) or 'amount' (AmountOffCents). Scope ties
// the coupon to a single revenue stream — 'all', 'shop', 'courses', or
// 'memberships'.
type Coupon struct {
	ID                int64      `json:"id" db:"id"`
	Code              string     `json:"code" db:"code"`
	Description       string     `json:"description" db:"description"`
	DiscountType      string     `json:"discountType" db:"discount_type"`
	PercentOff        int        `json:"percentOff" db:"percent_off"`
	AmountOffCents    int64      `json:"amountOffCents" db:"amount_off_cents"`
	Scope             string     `json:"scope" db:"scope"`
	MinSubtotalCents  int64      `json:"minSubtotalCents" db:"min_subtotal_cents"`
	MaxUses           *int       `json:"maxUses" db:"max_uses"`
	PerUserMaxUses    *int       `json:"perUserMaxUses" db:"per_user_max_uses"`
	UsedCount         int        `json:"usedCount" db:"used_count"`
	StartsAt          *time.Time `json:"startsAt" db:"starts_at"`
	ExpiresAt         *time.Time `json:"expiresAt" db:"expires_at"`
	Active            bool       `json:"active" db:"active"`
	CreatedBy         *int64     `json:"createdBy" db:"created_by"`
	CreatedAt         time.Time  `json:"createdAt" db:"created_at"`
	UpdatedAt         time.Time  `json:"updatedAt" db:"updated_at"`
}

const couponSelect = `SELECT id, code, description, discount_type, percent_off,
	amount_off_cents, scope, min_subtotal_cents, max_uses, per_user_max_uses,
	used_count, starts_at, expires_at, active, created_by, created_at, updated_at
	FROM coupons`

// ListCoupons returns every coupon, newest first.
func (s *Store) ListCoupons(ctx context.Context) ([]Coupon, error) {
	return queryRows[Coupon](ctx, s.pool,
		couponSelect+` ORDER BY created_at DESC, id DESC`)
}

// GetCouponByID returns one coupon.
func (s *Store) GetCouponByID(ctx context.Context, id int64) (*Coupon, error) {
	return queryOne[Coupon](ctx, s.pool, couponSelect+` WHERE id = $1`, id)
}

// GetCouponByCode looks up a coupon by its code (case-insensitive).
func (s *Store) GetCouponByCode(ctx context.Context, code string) (*Coupon, error) {
	return queryOne[Coupon](ctx, s.pool,
		couponSelect+` WHERE LOWER(code) = LOWER($1)`, strings.TrimSpace(code))
}

// CreateCoupon inserts a new coupon. Caller is responsible for validation.
func (s *Store) CreateCoupon(ctx context.Context, c *Coupon) error {
	c.Code = strings.ToUpper(strings.TrimSpace(c.Code))
	return s.pool.QueryRow(ctx, `
		INSERT INTO coupons (code, description, discount_type, percent_off,
			amount_off_cents, scope, min_subtotal_cents, max_uses, per_user_max_uses,
			starts_at, expires_at, active, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
		RETURNING id, used_count, created_at, updated_at`,
		c.Code, c.Description, c.DiscountType, c.PercentOff, c.AmountOffCents,
		c.Scope, c.MinSubtotalCents, c.MaxUses, c.PerUserMaxUses,
		c.StartsAt, c.ExpiresAt, c.Active, c.CreatedBy,
	).Scan(&c.ID, &c.UsedCount, &c.CreatedAt, &c.UpdatedAt)
}

// UpdateCoupon saves changes; used_count and created_at are not touched.
func (s *Store) UpdateCoupon(ctx context.Context, c *Coupon) error {
	c.Code = strings.ToUpper(strings.TrimSpace(c.Code))
	tag, err := s.pool.Exec(ctx, `
		UPDATE coupons SET code=$1, description=$2, discount_type=$3, percent_off=$4,
			amount_off_cents=$5, scope=$6, min_subtotal_cents=$7, max_uses=$8,
			per_user_max_uses=$9, starts_at=$10, expires_at=$11, active=$12, updated_at=now()
		WHERE id=$13`,
		c.Code, c.Description, c.DiscountType, c.PercentOff, c.AmountOffCents,
		c.Scope, c.MinSubtotalCents, c.MaxUses, c.PerUserMaxUses,
		c.StartsAt, c.ExpiresAt, c.Active, c.ID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteCoupon removes a coupon. Existing redemptions cascade delete.
func (s *Store) DeleteCoupon(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM coupons WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// CountCouponUsesByUser returns how many times this user has redeemed this
// coupon. Guests (user_id NULL) are not counted toward per-user limits.
func (s *Store) CountCouponUsesByUser(ctx context.Context, couponID, userID int64) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM coupon_redemptions WHERE coupon_id = $1 AND user_id = $2`,
		couponID, userID).Scan(&n)
	return n, err
}

// RecordCouponRedemption writes the redemption row and increments
// used_count. Done in one transaction to keep the counter accurate even
// under racing redemptions.
func (s *Store) RecordCouponRedemption(ctx context.Context, couponID int64, userID *int64, orderID int64, discountCents int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `
		INSERT INTO coupon_redemptions (coupon_id, user_id, order_id, amount_discounted_cents)
		VALUES ($1,$2,$3,$4)`, couponID, userID, orderID, discountCents); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE coupons SET used_count = used_count + 1, updated_at = now() WHERE id = $1`,
		couponID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// CouponUsageStats returns one usage record for the admin panel.
type CouponUsageStats struct {
	CouponID         int64     `json:"couponId" db:"coupon_id"`
	Code             string    `json:"code" db:"code"`
	RedemptionCount  int       `json:"redemptionCount" db:"redemption_count"`
	TotalDiscounted  int64     `json:"totalDiscountedCents" db:"total_discounted_cents"`
	LastRedeemedAt   *time.Time `json:"lastRedeemedAt" db:"last_redeemed_at"`
}

// ListCouponUsage rolls up redemption counts per coupon for the admin
// dashboard table.
func (s *Store) ListCouponUsage(ctx context.Context) ([]CouponUsageStats, error) {
	return queryRows[CouponUsageStats](ctx, s.pool, `
		SELECT c.id AS coupon_id, c.code,
			COUNT(r.id) AS redemption_count,
			COALESCE(SUM(r.amount_discounted_cents), 0) AS total_discounted_cents,
			MAX(r.created_at) AS last_redeemed_at
		FROM coupons c
		LEFT JOIN coupon_redemptions r ON r.coupon_id = c.id
		GROUP BY c.id, c.code`)
}
