-- Bring PostgreSQL to parity with MySQL 02150: the password column has to hold
-- the fork's argon2id PHC hash, which is 97 characters today
-- ($argon2id$v=19$m=19456,t=2,p=1$<22-char salt>$<43-char key>). The original
-- varchar(100) left only 3 characters of headroom, so any longer salt or
-- parameter block would be rejected outright.
--
-- Widening length is a binary-coercible change, so no USING clause is needed and
-- the rewrite is metadata-only.
ALTER TABLE "user" ALTER COLUMN "password" TYPE varchar(255);
