-- 02162 down restores the pre-migration GLOBAL unique key on auth_identifier.
--
-- WHY THIS FILE CHANGED
-- 02162 up deliberately allows the same identifier under different auth types
-- (that is the point of the migration: a "device" row may legitimately carry a
-- string that looks like somebody else's e-mail address or Telegram id). The
-- original down re-added the global key unconditionally, so as soon as one such
-- cross-type duplicate existed the ADD CONSTRAINT UNIQUE failed with a
-- duplicate-key error and golang-migrate left version 02162 dirty, which blocks
-- EVERY later migration from applying. Neutralising the duplicates first is
-- therefore a rollback-availability fix, not a cosmetic one.
--
-- RENAME, NOT DELETE
-- Surplus rows are renamed rather than deleted. A row in user_auth_methods is a
-- login/binding credential: deleting one silently removes a way for a user to
-- sign in and is unrecoverable. A rename keeps the row (so nothing is lost) and
-- keeps the original value inside the new value (so a rollback can be undone by
-- hand without a backup).
--
-- WHICH ROW KEEPS THE ORIGINAL
-- Per identifier the lowest id keeps the original value: that is the oldest row,
-- and therefore the credential the account most likely actually uses. Every
-- other row in the group is renamed. The rule depends only on id, so it is
-- deterministic across runs and engines.
--
-- THE RENAMED FORM
-- left(original, 150) || '#rollback-02162-' || md5(original || ':' || id)
-- (150 + 16 + 32 = 198 characters, inside varchar(255); only identifiers longer
-- than 150 characters are truncated in the renamed copy, and the hash covers the
-- FULL original so two long identifiers sharing a 150-character prefix still
-- produce different results.)
--
-- IDEMPOTENCE
-- md5(original || ':' || id) is unique per row because id is the primary key, so
-- all values written by this statement are distinct from each other. After it
-- runs no identifier group holds more than one row, so a re-run matches nothing
-- and is a no-op. (Only a pre-existing row crafted to already equal some other
-- row's renamed footprint could re-collide; such a row must be resolved by hand
-- before the rollback can finish.) The duplicate set is computed from the
-- statement's snapshot in the FROM subquery, so the MIN(id) rule is applied to
-- the pre-update data.
--
-- KNOWN ONE-WAY PAIR (not fixed here)
-- 02162 up backfills verified = TRUE for non-email/mobile/device rows and this
-- down never reverts it, so a rollback leaves those rows verified.
UPDATE user_auth_methods AS m
SET auth_identifier =
    left(m.auth_identifier, 150)
    || '#rollback-02162-'
    || md5(m.auth_identifier || ':' || m.id::text)
FROM (
    SELECT auth_identifier, MIN(id) AS keep_id
    FROM user_auth_methods
    GROUP BY auth_identifier
    HAVING COUNT(*) > 1
) AS d
WHERE d.auth_identifier = m.auth_identifier
  AND m.id <> d.keep_id;

DROP INDEX IF EXISTS idx_auth_type_identifier;

-- Guarded so the rollback can be re-run for recovery instead of failing on an
-- already restored index (the same class of failure that used to leave version
-- 02162 dirty and block every later migration). Matching on the index name alone
-- accepts either a constraint-backed unique index or a plain one, and avoids a
-- duplicate-name error when the name is already taken.
DO $$
BEGIN
    IF to_regclass('idx_auth_identifier') IS NULL THEN
        ALTER TABLE user_auth_methods
            ADD CONSTRAINT idx_auth_identifier UNIQUE (auth_identifier);
    END IF;
END $$;
