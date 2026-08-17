package migrations

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMigration225DefaultsAndBackfillsRequestedBillingModelSource(t *testing.T) {
	content, err := FS.ReadFile("225_channel_billing_model_source_requested.sql")
	require.NoError(t, err)

	sql := string(content)
	require.Contains(t, sql, "ALTER COLUMN billing_model_source SET DEFAULT 'requested'")
	require.Contains(t, sql, "UPDATE channels")
	require.Contains(t, sql, "SET billing_model_source = 'requested'")
	require.Contains(t, sql, "WHERE billing_model_source = 'channel_mapped'")
}
