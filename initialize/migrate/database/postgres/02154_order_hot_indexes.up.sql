-- Order listing, reconciliation and reporting all filter on status and page by
-- id or created_at. Ported from upstream v1.20.3 (02164/02165 pg order indexes).

CREATE INDEX IF NOT EXISTS idx_order_status_id
    ON "order" (status, id);

CREATE INDEX IF NOT EXISTS idx_order_status_created_at
    ON "order" (status, created_at);

CREATE INDEX IF NOT EXISTS idx_order_user_status_id
    ON "order" (user_id, status, id);

CREATE INDEX IF NOT EXISTS idx_order_subscribe_status_id
    ON "order" (subscribe_id, status, id);
