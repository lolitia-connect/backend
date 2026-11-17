-- Reverse of 02150: restore the historical varchar(100).
--
-- Mirrors mysql/02150_user_password_hash_length.down.sql. Note the engine
-- difference: MySQL truncates (or errors, under STRICT mode) on an over-long
-- value, PostgreSQL always raises "value too long for type character
-- varying(100)". Today's 97-character argon2id hashes still fit, but a value
-- longer than 100 characters would make this rollback fail and leave the
-- version dirty -- narrow the column by hand first in that case.
ALTER TABLE "user" ALTER COLUMN "password" TYPE varchar(100);
