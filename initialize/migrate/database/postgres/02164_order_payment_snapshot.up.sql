ALTER TABLE "order"
    ADD COLUMN "commission_referer_id" BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN "payment_amount" BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN "payment_currency" VARCHAR(16) NOT NULL DEFAULT '',
    ADD COLUMN "payment_session" TEXT NULL;