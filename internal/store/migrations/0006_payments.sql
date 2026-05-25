CREATE TABLE IF NOT EXISTS payments (
    id             BIGSERIAL PRIMARY KEY,
    order_id       BIGINT NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    gateway        TEXT NOT NULL DEFAULT 'flutterwave',
    tx_ref         TEXT NOT NULL UNIQUE,
    provider_tx_id TEXT NOT NULL DEFAULT '',
    amount_cents   BIGINT NOT NULL,
    currency       TEXT NOT NULL DEFAULT 'KES',
    status         TEXT NOT NULL DEFAULT 'pending',
    raw_response   JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    verified_at    TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_payments_order ON payments(order_id);
CREATE INDEX IF NOT EXISTS idx_payments_status ON payments(status);
