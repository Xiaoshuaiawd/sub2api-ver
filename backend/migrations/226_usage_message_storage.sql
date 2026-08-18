-- Administrator-visible client request/response body storage.
-- Bodies are compressed before insertion and kept separate from usage_logs so
-- billing and usage list queries never pay the TOAST/WAL cost of message data.
CREATE TABLE IF NOT EXISTS usage_message_captures (
    usage_log_id BIGINT PRIMARY KEY REFERENCES usage_logs(id) ON DELETE CASCADE,
    request_id VARCHAR(64) NOT NULL,
    request_state VARCHAR(16) NOT NULL DEFAULT 'pending',
    response_state VARCHAR(16) NOT NULL DEFAULT 'pending',
    request_raw_bytes BIGINT NOT NULL DEFAULT 0,
    response_raw_bytes BIGINT NOT NULL DEFAULT 0,
    request_stored_bytes BIGINT NOT NULL DEFAULT 0,
    response_stored_bytes BIGINT NOT NULL DEFAULT 0,
    request_sha256 CHAR(64),
    response_sha256 CHAR(64),
    compression VARCHAR(16) NOT NULL DEFAULT 'zstd',
    error_code VARCHAR(64),
    error_message TEXT,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT usage_message_captures_request_state_check CHECK
        (request_state IN ('pending', 'available', 'failed', 'partial', 'too_large', 'expired', 'disabled')),
    CONSTRAINT usage_message_captures_response_state_check CHECK
        (response_state IN ('pending', 'available', 'failed', 'partial', 'too_large', 'expired', 'disabled'))
);

CREATE INDEX IF NOT EXISTS idx_usage_message_captures_expires_at
    ON usage_message_captures (expires_at);
CREATE INDEX IF NOT EXISTS idx_usage_message_captures_request_id
    ON usage_message_captures (request_id);

CREATE TABLE IF NOT EXISTS usage_message_bodies (
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    usage_log_id BIGINT NOT NULL REFERENCES usage_logs(id) ON DELETE CASCADE,
    body_type VARCHAR(8) NOT NULL,
    payload_zstd BYTEA NOT NULL,
    raw_bytes BIGINT NOT NULL,
    stored_bytes BIGINT NOT NULL,
    created_at_inserted TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT usage_message_bodies_body_type_check CHECK (body_type IN ('request', 'response')),
    PRIMARY KEY (created_at, usage_log_id, body_type)
) PARTITION BY RANGE (created_at);

ALTER TABLE usage_message_bodies ALTER COLUMN payload_zstd SET STORAGE EXTERNAL;

DO $$
DECLARE
    offset_days INTEGER;
    partition_day DATE;
BEGIN
    FOR offset_days IN -1..2 LOOP
        partition_day := (CURRENT_DATE + offset_days);
        EXECUTE format(
            'CREATE TABLE IF NOT EXISTS %I PARTITION OF usage_message_bodies FOR VALUES FROM (%L) TO (%L)',
            'usage_message_bodies_' || to_char(partition_day, 'YYYYMMDD'),
            to_char(partition_day, 'YYYY-MM-DD 00:00:00+00'),
            to_char(partition_day + 1, 'YYYY-MM-DD 00:00:00+00')
        );
    END LOOP;
END $$;

CREATE INDEX IF NOT EXISTS idx_usage_message_bodies_usage_log
    ON usage_message_bodies (usage_log_id, created_at);
