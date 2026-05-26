-- 0015_product_download_grants.sql
-- One row per (customer, order, downloadable file). Tracks how many
-- times that customer has fetched the file on that specific order so
-- products.max_downloads can be enforced. Rows are created lazily on
-- the first download attempt.

CREATE TABLE IF NOT EXISTS product_download_grants (
    id                  BIGSERIAL PRIMARY KEY,
    user_id             BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    order_id            BIGINT NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    download_id         BIGINT NOT NULL REFERENCES product_downloads(id) ON DELETE CASCADE,
    download_count      INTEGER NOT NULL DEFAULT 0,
    first_downloaded_at TIMESTAMPTZ,
    last_downloaded_at  TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, order_id, download_id)
);

CREATE INDEX IF NOT EXISTS idx_grants_user_order
    ON product_download_grants(user_id, order_id);
