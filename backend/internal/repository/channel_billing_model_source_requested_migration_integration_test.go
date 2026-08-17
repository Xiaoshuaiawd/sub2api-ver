//go:build integration

package repository

import (
	"context"
	"testing"

	dbmigrations "github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

func TestMigration225DefaultsAndBackfillsRequestedBillingModelSource(t *testing.T) {
	tx := testTx(t)
	ctx := context.Background()
	migrationSQL, err := dbmigrations.FS.ReadFile("225_channel_billing_model_source_requested.sql")
	require.NoError(t, err)

	_, err = tx.ExecContext(ctx, "ALTER TABLE channels ALTER COLUMN billing_model_source SET DEFAULT 'channel_mapped'")
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `
INSERT INTO channels (name, billing_model_source)
VALUES
    ('migration-225-channel-mapped', 'channel_mapped'),
    ('migration-225-requested', 'requested'),
    ('migration-225-upstream', 'upstream'),
    ('migration-225-response-model', 'response_model')
`)
	require.NoError(t, err)

	_, err = tx.ExecContext(ctx, string(migrationSQL))
	require.NoError(t, err)

	var columnDefault string
	require.NoError(t, tx.QueryRowContext(ctx, `
SELECT column_default
FROM information_schema.columns
WHERE table_schema = 'public'
  AND table_name = 'channels'
  AND column_name = 'billing_model_source'
`).Scan(&columnDefault))
	require.Contains(t, columnDefault, "'requested'")

	rows, err := tx.QueryContext(ctx, `
SELECT name, billing_model_source
FROM channels
WHERE name LIKE 'migration-225-%'
`)
	require.NoError(t, err)
	defer rows.Close()

	got := make(map[string]string)
	for rows.Next() {
		var name, source string
		require.NoError(t, rows.Scan(&name, &source))
		got[name] = source
	}
	require.NoError(t, rows.Err())
	require.Equal(t, map[string]string{
		"migration-225-channel-mapped": "requested",
		"migration-225-requested":      "requested",
		"migration-225-upstream":       "upstream",
		"migration-225-response-model": "response_model",
	}, got)
}
