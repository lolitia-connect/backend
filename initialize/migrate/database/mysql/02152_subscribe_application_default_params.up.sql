-- Add per-client default template params for subscription delivery.
-- Stored in query-string form (e.g. "mode=rule&emoji=1") and layered under the
-- subscription URL's own query string at delivery time.
ALTER TABLE `subscribe_application`
    ADD COLUMN `default_params` VARCHAR(255) NOT NULL DEFAULT '' COMMENT 'Default Template Params' AFTER `output_format`;
