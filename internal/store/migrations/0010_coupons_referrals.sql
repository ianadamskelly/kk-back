-- Coupons. discount_type is 'percent' (percent_off 0-100) or 'amount'
-- (amount_off_cents). scope ties the coupon to one revenue stream — 'all',
-- 'shop', 'courses', or 'memberships'.
CREATE TABLE IF NOT EXISTS coupons (
    id                BIGSERIAL PRIMARY KEY,
    code              TEXT NOT NULL UNIQUE,
    description       TEXT NOT NULL DEFAULT '',
    discount_type     TEXT NOT NULL,
    percent_off       SMALLINT NOT NULL DEFAULT 0,
    amount_off_cents  BIGINT NOT NULL DEFAULT 0,
    scope             TEXT NOT NULL DEFAULT 'all',
    min_subtotal_cents BIGINT NOT NULL DEFAULT 0,
    max_uses          INTEGER,
    per_user_max_uses INTEGER,
    used_count        INTEGER NOT NULL DEFAULT 0,
    starts_at         TIMESTAMPTZ,
    expires_at        TIMESTAMPTZ,
    active            BOOLEAN NOT NULL DEFAULT TRUE,
    created_by        BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_coupons_active ON coupons(active);

-- One row per successful coupon application. We track the snapshot
-- amount discounted so an admin edit of the coupon later doesn't rewrite
-- history.
CREATE TABLE IF NOT EXISTS coupon_redemptions (
    id                       BIGSERIAL PRIMARY KEY,
    coupon_id                BIGINT NOT NULL REFERENCES coupons(id) ON DELETE CASCADE,
    user_id                  BIGINT REFERENCES users(id) ON DELETE SET NULL,
    order_id                 BIGINT REFERENCES orders(id) ON DELETE SET NULL,
    amount_discounted_cents  BIGINT NOT NULL,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_coupon_redemptions_coupon ON coupon_redemptions(coupon_id);
CREATE INDEX IF NOT EXISTS idx_coupon_redemptions_user ON coupon_redemptions(user_id);

-- Orders gain price-breakdown columns so we can report and audit how an
-- order's total was arrived at. subtotal = sum of line items; total =
-- subtotal - discount - credit.
ALTER TABLE orders
    ADD COLUMN IF NOT EXISTS subtotal_cents BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS discount_cents BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS credit_cents   BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS coupon_id      BIGINT REFERENCES coupons(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS coupon_code    TEXT NOT NULL DEFAULT '';

-- Back-fill subtotal = total for existing rows so old data stays consistent.
UPDATE orders SET subtotal_cents = total_cents WHERE subtotal_cents = 0;

-- Referral attribution. referral_code is the user's shareable code (we
-- generate it on demand). referred_by_user_id is set at signup when the
-- new user came in through a ?ref=CODE link.
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS referral_code TEXT,
    ADD COLUMN IF NOT EXISTS referred_by_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS referral_rewarded_at TIMESTAMPTZ;
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_referral_code ON users(referral_code)
    WHERE referral_code IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_users_referred_by ON users(referred_by_user_id);

-- Store credit ledger. The current balance for a user is the sum of
-- amount_cents across their transactions; we keep no denormalised
-- "balance" column to avoid drift.
CREATE TABLE IF NOT EXISTS credit_transactions (
    id              BIGSERIAL PRIMARY KEY,
    user_id         BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    amount_cents    BIGINT NOT NULL,
    reason          TEXT NOT NULL,
    related_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    related_order_id BIGINT REFERENCES orders(id) ON DELETE SET NULL,
    note            TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_credit_tx_user ON credit_transactions(user_id);
CREATE INDEX IF NOT EXISTS idx_credit_tx_reason ON credit_transactions(reason);
