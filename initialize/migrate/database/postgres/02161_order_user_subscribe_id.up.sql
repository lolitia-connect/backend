ALTER TABLE "order" ADD COLUMN user_subscribe_id BIGINT NOT NULL DEFAULT 0;
CREATE INDEX idx_order_user_subscribe_id ON "order" (user_subscribe_id);