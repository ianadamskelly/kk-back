-- Orders gain a `kind` so we can split revenue by source: shop / course /
-- membership / service. Existing rows are shop orders.
ALTER TABLE orders
    ADD COLUMN IF NOT EXISTS kind TEXT NOT NULL DEFAULT 'shop';
CREATE INDEX IF NOT EXISTS idx_orders_kind ON orders(kind);

-- Order items can now point at a course instead of (or in addition to) a
-- product, so course purchases reuse the same orders/payments plumbing.
ALTER TABLE order_items
    ADD COLUMN IF NOT EXISTS course_id BIGINT REFERENCES courses(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_order_items_course ON order_items(course_id);

-- One row per member. current_period_end is the source of truth for
-- "are they currently active?"; status tracks lifecycle.
CREATE TABLE IF NOT EXISTS memberships (
    id                  BIGSERIAL PRIMARY KEY,
    user_id             BIGINT NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
    status              TEXT NOT NULL DEFAULT 'active',
    started_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    current_period_end  TIMESTAMPTZ NOT NULL,
    cancelled_at        TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_memberships_period_end ON memberships(current_period_end);

-- Manual log of service income — for offline-invoiced consulting work that
-- doesn't flow through the online payment gateways.
CREATE TABLE IF NOT EXISTS service_revenue (
    id           BIGSERIAL PRIMARY KEY,
    service_id   BIGINT REFERENCES services(id) ON DELETE SET NULL,
    service_name TEXT NOT NULL DEFAULT '',
    client_name  TEXT NOT NULL DEFAULT '',
    amount_cents BIGINT NOT NULL,
    currency     TEXT NOT NULL DEFAULT 'KES',
    occurred_at  DATE NOT NULL DEFAULT CURRENT_DATE,
    note         TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_service_revenue_occurred ON service_revenue(occurred_at);
