-- 0021_idempotent_discount_consumption.sql
-- Make it safe to call recordOrderDiscounts more than once per order.
-- An order may transition into "confirmed" by multiple paths (gateway
-- verify, admin marking confirmed, replayed webhook). The partial
-- UNIQUE indexes below ensure a coupon redemption / credit debit per
-- order is recorded at most once, regardless of how many code paths
-- try to register it.

CREATE UNIQUE INDEX IF NOT EXISTS idx_coupon_redemptions_one_per_order
    ON coupon_redemptions(order_id)
    WHERE order_id IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_credit_tx_one_spend_per_order
    ON credit_transactions(related_order_id)
    WHERE reason = 'order_spend' AND related_order_id IS NOT NULL;
