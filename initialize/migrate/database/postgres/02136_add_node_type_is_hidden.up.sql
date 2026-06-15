ALTER TABLE "nodes"
    ADD COLUMN "node_type" varchar(20) NOT NULL DEFAULT 'landing',
    ADD COLUMN "is_hidden" BOOLEAN NOT NULL DEFAULT false;

UPDATE "nodes"
SET "node_type" = 'landing'
WHERE "node_type" = '';
