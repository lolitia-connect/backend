ALTER TABLE user_auth_methods DROP CONSTRAINT IF EXISTS idx_auth_identifier;
DROP INDEX IF EXISTS idx_auth_identifier;
CREATE UNIQUE INDEX IF NOT EXISTS idx_auth_type_identifier ON user_auth_methods (auth_type, auth_identifier);
UPDATE user_auth_methods SET verified = TRUE
WHERE auth_type NOT IN ('email', 'mobile', 'device') AND verified = FALSE;