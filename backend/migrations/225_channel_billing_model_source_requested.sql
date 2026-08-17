-- Bill model mappings by the client-requested model unless an administrator
-- explicitly selects another billing source after this migration.
ALTER TABLE channels ALTER COLUMN billing_model_source SET DEFAULT 'requested';

UPDATE channels
SET billing_model_source = 'requested'
WHERE billing_model_source = 'channel_mapped';
