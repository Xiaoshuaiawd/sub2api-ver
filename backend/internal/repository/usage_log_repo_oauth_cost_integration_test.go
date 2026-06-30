//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUsageLog_GetOAuthCostSummary_OnlyActiveOAuthAccounts(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	client := tx.Client()
	repo := newUsageLogRepositoryWithSQL(client, tx)

	user := mustCreateUser(t, client, &service.User{Email: "oauth-cost-summary@test.com"})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-oauth-cost", Name: "oauth-cost"})

	activeOAuth := mustCreateAccount(t, client, &service.Account{Name: "oauth-cost-active", Type: service.AccountTypeOAuth})
	oauthWithoutLogs := mustCreateAccount(t, client, &service.Account{Name: "oauth-cost-empty", Type: service.AccountTypeOAuth})
	apiKeyAccount := mustCreateAccount(t, client, &service.Account{Name: "oauth-cost-apikey", Type: service.AccountTypeAPIKey})
	setupTokenAccount := mustCreateAccount(t, client, &service.Account{Name: "oauth-cost-setup", Type: service.AccountTypeSetupToken})
	deletedOAuth := mustCreateAccount(t, client, &service.Account{Name: "oauth-cost-deleted", Type: service.AccountTypeOAuth})
	_, err := client.Account.UpdateOneID(deletedOAuth.ID).SetDeletedAt(time.Now().UTC()).Save(ctx)
	require.NoError(t, err)

	multiplierOnePointFive := 1.5
	accountStatsCost := 4.0
	multiplierTwo := 2.0
	_, err = repo.Create(ctx, &service.UsageLog{
		UserID: user.ID, APIKeyID: apiKey.ID, AccountID: activeOAuth.ID,
		Model: "claude-3", InputTokens: 1, OutputTokens: 2, CacheCreationTokens: 3, CacheReadTokens: 4,
		TotalCost: 10, ActualCost: 10, AccountRateMultiplier: &multiplierOnePointFive,
		CreatedAt: time.Now().UTC(),
	})
	require.NoError(t, err)
	_, err = repo.Create(ctx, &service.UsageLog{
		UserID: user.ID, APIKeyID: apiKey.ID, AccountID: activeOAuth.ID,
		Model: "claude-3", InputTokens: 5, OutputTokens: 6, CacheCreationTokens: 7, CacheReadTokens: 8,
		TotalCost: 99, ActualCost: 99, AccountStatsCost: &accountStatsCost, AccountRateMultiplier: &multiplierTwo,
		CreatedAt: time.Now().UTC(),
	})
	require.NoError(t, err)

	for _, accountID := range []int64{apiKeyAccount.ID, setupTokenAccount.ID, deletedOAuth.ID} {
		_, err = repo.Create(ctx, &service.UsageLog{
			UserID: user.ID, APIKeyID: apiKey.ID, AccountID: accountID,
			Model: "excluded", InputTokens: 1000, OutputTokens: 1000,
			TotalCost: 1000, ActualCost: 1000, AccountRateMultiplier: &multiplierTwo,
			CreatedAt: time.Now().UTC(),
		})
		require.NoError(t, err)
	}

	summary, err := repo.GetOAuthCostSummary(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(2), summary.AccountCount, "active OAuth accounts include accounts without usage logs")
	require.Equal(t, int64(2), summary.TotalRequests)
	require.Equal(t, int64(36), summary.TotalTokens)
	require.InDelta(t, 23.0, summary.TotalConsumedQuota, 1e-9)

	require.NotZero(t, oauthWithoutLogs.ID)
}
