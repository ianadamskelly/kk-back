-- 0027_interactive_assets.sql
-- Interactive products are paid resources rendered inside the app rather
-- than streamed as downloadable files. The product row declares which
-- asset a confirmed shop order should grant.

ALTER TABLE products
    ADD COLUMN IF NOT EXISTS interactive_asset_slug TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_products_interactive_asset_slug
    ON products(interactive_asset_slug)
    WHERE interactive_asset_slug <> '';

CREATE TABLE IF NOT EXISTS interactive_asset_entitlements (
    id              BIGSERIAL PRIMARY KEY,
    user_id         BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    order_id        BIGINT NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    product_id      BIGINT REFERENCES products(id) ON DELETE SET NULL,
    asset_slug      TEXT NOT NULL,
    license_id      TEXT NOT NULL UNIQUE,
    uses_remaining  INTEGER NOT NULL DEFAULT 5,
    expires_at      TIMESTAMPTZ,
    status          TEXT NOT NULL DEFAULT 'active',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, order_id, asset_slug)
);

ALTER TABLE interactive_asset_entitlements
    DROP CONSTRAINT IF EXISTS interactive_asset_entitlements_status_check;
ALTER TABLE interactive_asset_entitlements
    ADD CONSTRAINT interactive_asset_entitlements_status_check
    CHECK (status IN ('active', 'revoked', 'expired'));

CREATE INDEX IF NOT EXISTS idx_asset_entitlements_user_asset
    ON interactive_asset_entitlements(user_id, asset_slug);

CREATE TABLE IF NOT EXISTS interactive_asset_events (
    id             BIGSERIAL PRIMARY KEY,
    entitlement_id BIGINT REFERENCES interactive_asset_entitlements(id) ON DELETE CASCADE,
    user_id        BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    asset_slug     TEXT NOT NULL,
    event_type     TEXT NOT NULL,
    ip_address     TEXT NOT NULL DEFAULT '',
    user_agent     TEXT NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_asset_events_entitlement_created
    ON interactive_asset_events(entitlement_id, created_at DESC);

-- First built-in interactive product. Admins can change price/copy/images
-- from the product screen; this row makes the product available in shops
-- that do not already have it.
INSERT INTO products (
    slug, name, description, body, price_cents, image, category, status,
    sort_order, kind, max_downloads, interactive_asset_slug
)
SELECT
    'brand-clarity-worksheet',
    'Brand Clarity Worksheet',
    'A guided in-app workbook for clarifying your purpose, customer, voice, visuals, and brand promise.',
    '<p>Work through ten guided exercises, save your answers on your device, and export a watermarked PDF when you are ready.</p>',
    0,
    '',
    'Digital Resources',
    'draft',
    0,
    'digital',
    NULL,
    'brand-clarity-worksheet'
WHERE NOT EXISTS (
    SELECT 1 FROM products WHERE slug = 'brand-clarity-worksheet'
);
