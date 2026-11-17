-- V2 checkout: order state version, create idempotency, and the order event outbox.
--
-- Both MySQL and PostgreSQL allow repeated NULLs in a unique index, so the
-- unique key below does not conflict with V1 or historical orders, which never
-- set idempotency_key.
--
-- Every object creation below is guarded with IF NOT EXISTS so the migration is
-- re-runnable: an install that already carries these objects (from a previous
-- partial run, or from a schema created out of band) must not abort here and
-- leave version 02160 dirty, which would block every later migration. The guards
-- are required on the ALTER and the unique index too, not just the table,
-- because this file runs top to bottom: if an earlier run stopped at the table,
-- the column adds and the index had already been applied and would otherwise
-- fail first on the retry.

ALTER TABLE "order"
    ADD COLUMN IF NOT EXISTS state_version BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS idempotency_key VARCHAR(128) NULL,
    ADD COLUMN IF NOT EXISTS idempotency_hash CHAR(64) NULL,
    ADD COLUMN IF NOT EXISTS guest_checkout_token_hash CHAR(64) NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_order_idempotency_key ON "order" (idempotency_key);

CREATE TABLE IF NOT EXISTS order_event (
    id BIGSERIAL PRIMARY KEY,
    order_id BIGINT NOT NULL,
    order_no VARCHAR(255) NOT NULL,
    event_type VARCHAR(64) NOT NULL,
    payload TEXT NOT NULL,
    created_at TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    published_at TIMESTAMP(3) NULL DEFAULT NULL
);

CREATE INDEX IF NOT EXISTS idx_order_event_order_id_id ON order_event (order_id, id);
CREATE INDEX IF NOT EXISTS idx_order_event_order_no_id ON order_event (order_no, id);
CREATE INDEX IF NOT EXISTS idx_order_event_published_at_id ON order_event (published_at, id);
