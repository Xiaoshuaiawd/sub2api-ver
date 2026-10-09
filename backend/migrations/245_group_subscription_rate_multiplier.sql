-- A NULL value retains the pre-upgrade behavior for existing groups.
ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS subscription_rate_multiplier DECIMAL(10,4);
