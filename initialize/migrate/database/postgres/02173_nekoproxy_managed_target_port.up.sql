ALTER TABLE nekoproxy_bindings ADD COLUMN managed_target_port INTEGER CHECK (managed_target_port BETWEEN 0 AND 65535);
