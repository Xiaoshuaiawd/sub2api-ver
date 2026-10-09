-- Multi-group plans keep one subscription usage ledger. Existing rows remain
-- single-group subscriptions; new payment orders snapshot the selected groups
-- and shared caps so later plan edits cannot alter an in-flight purchase.
ALTER TABLE subscription_plans
    ADD COLUMN IF NOT EXISTS group_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN IF NOT EXISTS daily_limit_usd DECIMAL(20,8),
    ADD COLUMN IF NOT EXISTS weekly_limit_usd DECIMAL(20,8),
    ADD COLUMN IF NOT EXISTS monthly_limit_usd DECIMAL(20,8);

UPDATE subscription_plans AS p
SET group_ids = jsonb_build_array(p.group_id),
    daily_limit_usd = g.daily_limit_usd,
    weekly_limit_usd = g.weekly_limit_usd,
    monthly_limit_usd = g.monthly_limit_usd
FROM groups AS g
WHERE p.group_id = g.id AND p.group_ids = '[]'::jsonb;

ALTER TABLE payment_orders
    ADD COLUMN IF NOT EXISTS subscription_id BIGINT,
    ADD COLUMN IF NOT EXISTS subscription_group_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN IF NOT EXISTS subscription_limits_snapshot BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS subscription_daily_limit_usd DECIMAL(20,8),
    ADD COLUMN IF NOT EXISTS subscription_weekly_limit_usd DECIMAL(20,8),
    ADD COLUMN IF NOT EXISTS subscription_monthly_limit_usd DECIMAL(20,8);

-- Historical orders keep the empty JSON default. Fulfillment and refunds fall
-- back to subscription_group_id for those orders, avoiding a large backfill.

ALTER TABLE user_subscriptions
    ADD COLUMN IF NOT EXISTS group_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN IF NOT EXISTS plan_id BIGINT,
    ADD COLUMN IF NOT EXISTS daily_limit_usd DECIMAL(20,8),
    ADD COLUMN IF NOT EXISTS weekly_limit_usd DECIMAL(20,8),
    ADD COLUMN IF NOT EXISTS monthly_limit_usd DECIMAL(20,8);

-- Historical subscriptions keep the empty JSON default. Membership queries
-- also check the indexed primary group_id, avoiding a large backfill.

CREATE INDEX IF NOT EXISTS idx_user_subscriptions_group_ids
    ON user_subscriptions USING GIN (group_ids jsonb_path_ops)
    WHERE deleted_at IS NULL;
