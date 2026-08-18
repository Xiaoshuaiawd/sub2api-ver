package migrations

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUsageMessageStorageMigration(t *testing.T) {
	sqlBytes, err := FS.ReadFile("226_usage_message_storage.sql")
	require.NoError(t, err)
	sql := string(sqlBytes)
	require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS usage_message_captures")
	require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS usage_message_bodies")
	require.Contains(t, sql, "PARTITION BY RANGE (created_at)")
	require.Contains(t, sql, "payload_zstd BYTEA NOT NULL")
	require.Contains(t, sql, "CHECK (body_type IN ('request', 'response'))")
	require.Contains(t, sql, "expires_at")
}
