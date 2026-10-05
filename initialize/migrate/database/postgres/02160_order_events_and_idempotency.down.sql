DROP TABLE IF EXISTS order_event;

DROP INDEX IF EXISTS idx_order_idempotency_key;

ALTER TABLE "order"
    DROP COLUMN guest_checkout_token_hash,
    DROP COLUMN idempotency_hash,
    DROP COLUMN idempotency_key,
    DROP COLUMN state_version;
