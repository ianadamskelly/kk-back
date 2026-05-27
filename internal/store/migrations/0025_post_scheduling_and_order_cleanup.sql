-- 0025_post_scheduling_and_order_cleanup.sql
-- Schedule Insights posts and retain audit records for stale, unconfirmed orders.

ALTER TABLE posts
    ADD COLUMN IF NOT EXISTS scheduled_at TIMESTAMPTZ;

ALTER TABLE orders
    ADD COLUMN IF NOT EXISTS auto_cancelled_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_posts_scheduled_due
    ON posts(scheduled_at)
    WHERE status = 'scheduled' AND scheduled_at IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_orders_stale_pending
    ON orders(created_at)
    WHERE status = 'pending';

CREATE INDEX IF NOT EXISTS idx_orders_auto_cancelled
    ON orders(auto_cancelled_at)
    WHERE auto_cancelled_at IS NOT NULL;
