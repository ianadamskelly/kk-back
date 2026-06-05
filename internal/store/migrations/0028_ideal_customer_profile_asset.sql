-- 0028_ideal_customer_profile_asset.sql
-- Second built-in in-app worksheet product. Kept draft/zero-price by default
-- so admins can set final price/copy/images before publishing.

INSERT INTO products (
    slug, name, description, body, price_cents, image, category, status,
    sort_order, kind, max_downloads, interactive_asset_slug
)
SELECT
    'ideal-customer-profile-template',
    'Ideal Customer Profile Template',
    'A guided in-app template for defining the one customer your business serves best.',
    '<p>Build a vivid profile of your ideal customer, save your answers on your device, and export a watermarked PDF when you are ready.</p>',
    0,
    '',
    'Digital Resources',
    'draft',
    0,
    'digital',
    NULL,
    'ideal-customer-profile-template'
WHERE NOT EXISTS (
    SELECT 1 FROM products WHERE slug = 'ideal-customer-profile-template'
);
