-- Optional routing group metadata for proxies and accounts.
ALTER TABLE proxies ADD COLUMN IF NOT EXISTS proxy_group VARCHAR(100);
ALTER TABLE accounts ADD COLUMN IF NOT EXISTS proxy_group VARCHAR(100);

CREATE INDEX IF NOT EXISTS idx_proxies_proxy_group_status
    ON proxies(proxy_group, status)
    WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_accounts_proxy_group
    ON accounts(proxy_group)
    WHERE deleted_at IS NULL;
