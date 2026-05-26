-- 0013_product_images.sql
-- Products can have multiple images displayed as a gallery. We keep
-- the original products.image column as a denormalised "cover" URL
-- so listing endpoints don't need to join, but the gallery (and the
-- admin uploader) operate on the product_images table.

CREATE TABLE IF NOT EXISTS product_images (
    id          BIGSERIAL PRIMARY KEY,
    product_id  BIGINT NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    url         TEXT NOT NULL,
    position    INTEGER NOT NULL DEFAULT 0,
    is_cover    BOOLEAN NOT NULL DEFAULT FALSE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_product_images_product_position
    ON product_images(product_id, position);

-- At most one cover per product.
CREATE UNIQUE INDEX IF NOT EXISTS idx_product_images_one_cover_per_product
    ON product_images(product_id) WHERE is_cover = TRUE;

-- Backfill: every existing product with a non-empty image becomes
-- a single cover row in product_images. Idempotent — re-running this
-- migration won't double-insert because we skip products that already
-- have any image rows.
INSERT INTO product_images (product_id, url, position, is_cover)
SELECT p.id, p.image, 0, TRUE
FROM products p
WHERE p.image IS NOT NULL
  AND p.image <> ''
  AND NOT EXISTS (
      SELECT 1 FROM product_images pi WHERE pi.product_id = p.id
  );
