ALTER TABLE "node_group"
    ADD COLUMN "group_type" varchar(32) NOT NULL DEFAULT 'common';

UPDATE "node_group"
SET "group_type" = 'common'
WHERE "group_type" = '';
