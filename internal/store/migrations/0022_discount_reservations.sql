-- 0022_discount_reservations.sql
-- Reserve coupon/credit value while a customer completes payment so two
-- concurrent pending orders cannot be paid using the same limited value.

CREATE TABLE IF NOT EXISTS order_discount_reservations (
    order_id                 BIGINT PRIMARY KEY REFERENCES orders(id) ON DELETE CASCADE,
    user_id                  BIGINT REFERENCES users(id) ON DELETE SET NULL,
    coupon_id                BIGINT REFERENCES coupons(id) ON DELETE SET NULL,
    reserved_discount_cents  BIGINT NOT NULL DEFAULT 0,
    reserved_credit_cents    BIGINT NOT NULL DEFAULT 0,
    status                   TEXT NOT NULL DEFAULT 'held'
                                 CHECK (status IN ('held', 'consumed', 'released')),
    expires_at               TIMESTAMPTZ NOT NULL,
    payment_started_at       TIMESTAMPTZ,
    consumed_at              TIMESTAMPTZ,
    released_at              TIMESTAMPTZ,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_order_discount_reservations_live_coupon
    ON order_discount_reservations(coupon_id, status, expires_at);
CREATE INDEX IF NOT EXISTS idx_order_discount_reservations_live_user
    ON order_discount_reservations(user_id, status, expires_at);

-- Pending discounted checkouts created before reservations cannot be trusted:
-- they may duplicate the same coupon or credit. Paid-but-unconfirmed rows need
-- human reconciliation; unpaid rows are cancelled and must be checked out again.
UPDATE orders o
SET status = 'payment_review'
WHERE o.status = 'pending'
  AND (o.discount_cents > 0 OR o.credit_cents > 0)
  AND EXISTS (
      SELECT 1 FROM payments p
      WHERE p.order_id = o.id AND p.status = 'successful'
  );

UPDATE orders o
SET status = 'cancelled'
WHERE o.status = 'pending'
  AND (o.discount_cents > 0 OR o.credit_cents > 0);
