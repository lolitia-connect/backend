-- Truncate first: the old width cannot hold what the up migration allowed.
UPDATE "user_device"
SET "user_agent" = LEFT("user_agent", 64)
WHERE length("user_agent") > 64;

ALTER TABLE "user_device"
    ALTER COLUMN "user_agent" TYPE varchar(64);
