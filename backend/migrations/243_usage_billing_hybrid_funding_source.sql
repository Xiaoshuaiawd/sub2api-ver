-- Keep the settled source with the durable idempotency record. NULL marks
-- legacy rows created before hybrid billing was available.
ALTER TABLE usage_billing_dedup
    ADD COLUMN IF NOT EXISTS billing_type SMALLINT,
    ADD COLUMN IF NOT EXISTS subscription_id BIGINT;

ALTER TABLE usage_billing_dedup_archive
    ADD COLUMN IF NOT EXISTS billing_type SMALLINT,
    ADD COLUMN IF NOT EXISTS subscription_id BIGINT;
