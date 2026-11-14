-- Modern browser User-Agent strings routinely exceed the original varchar(64),
-- which made every device bind fail with "value too long for type character
-- varying(64)". Ported from upstream v1.20.3 (02149_user_device_user_agent).

ALTER TABLE "user_device"
    ALTER COLUMN "user_agent" TYPE varchar(512);
