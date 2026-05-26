-- 0014_product_kind_and_downloads.sql
-- Products are either "physical" (default; need shipping) or "digital"
-- (have downloadable files attached). Existing products stay physical.

ALTER TABLE products
    ADD COLUMN IF NOT EXISTS kind          TEXT    NOT NULL DEFAULT 'physical',
    ADD COLUMN IF NOT EXISTS max_downloads INTEGER;
-- max_downloads is nullable: NULL means unlimited, any int N means each
-- customer may download up to N times (enforced when signed download
-- tokens are introduced).

ALTER TABLE products
    DROP CONSTRAINT IF EXISTS products_kind_check;
ALTER TABLE products
    ADD CONSTRAINT products_kind_check CHECK (kind IN ('physical', 'digital'));

-- One row per downloadable file. A digital product can have several
-- (e.g. PDF + EPUB + audio). Physical products have none.
CREATE TABLE IF NOT EXISTS product_downloads (
    id          BIGSERIAL PRIMARY KEY,
    product_id  BIGINT NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    url         TEXT   NOT NULL,
    label       TEXT   NOT NULL DEFAULT '',
    size_bytes  BIGINT NOT NULL DEFAULT 0,
    position    INTEGER NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_product_downloads_product_position
    ON product_downloads(product_id, position);
