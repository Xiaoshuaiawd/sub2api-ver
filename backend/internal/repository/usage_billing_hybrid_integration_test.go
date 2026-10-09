//go:build integration

package repository

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func createHybridBillingFixture(t *testing.T) (int64, int64, int64) {
	t.Helper()
	client := testEntClient(t)
	user := mustCreateUser(t, client, &service.User{
		Email: fmt.Sprintf("hybrid-billing-%s@example.com", uuid.NewString()), PasswordHash: "hash", Balance: 100,
	})
	limit := 1.0
	group := mustCreateGroup(t, client, &service.Group{
		Name: "hybrid-billing-" + uuid.NewString(), Platform: service.PlatformOpenAI,
		SubscriptionType: service.SubscriptionTypeSubscriptionBalance, RateMultiplier: 1, DailyLimitUSD: &limit,
	})
	key := mustCreateApiKey(t, client, &service.APIKey{
		UserID: user.ID, GroupID: &group.ID, Key: "sk-hybrid-" + uuid.NewString(), Name: "hybrid-billing",
	})
	sub := mustCreateSubscription(t, client, &service.UserSubscription{UserID: user.ID, GroupID: group.ID})
	_, err := integrationDB.ExecContext(context.Background(), `UPDATE user_subscriptions SET daily_window_start = NOW(), weekly_window_start = NOW(), monthly_window_start = NOW() WHERE id = $1`, sub.ID)
	require.NoError(t, err)
	return user.ID, key.ID, sub.ID
}

func hybridBillingCommand(userID, keyID, subID, groupID int64, requestID string, cost float64) *service.UsageBillingCommand {
	return &service.UsageBillingCommand{
		RequestID: requestID, UserID: userID, APIKeyID: keyID, SubscriptionID: &subID,
		BillingType: service.BillingTypeSubscription, SubscriptionCost: cost,
		HybridGroupID: groupID, HybridCostUSD: cost,
	}
}

func TestUsageBillingRepositoryApply_HybridUsesSubscriptionThenWholeBalance(t *testing.T) {
	ctx := context.Background()
	userID, keyID, subID := createHybridBillingFixture(t)
	var groupID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT group_id FROM user_subscriptions WHERE id = $1`, subID).Scan(&groupID))
	repo := NewUsageBillingRepository(testEntClient(t), integrationDB)

	first, err := repo.Apply(ctx, hybridBillingCommand(userID, keyID, subID, groupID, uuid.NewString(), 0.6))
	require.NoError(t, err)
	require.True(t, first.Applied)
	require.Equal(t, service.BillingTypeSubscription, first.BillingType)
	require.NotNil(t, first.SubscriptionID)
	require.Equal(t, subID, *first.SubscriptionID)

	secondCommand := hybridBillingCommand(userID, keyID, subID, groupID, uuid.NewString(), 0.6)
	second, err := repo.Apply(ctx, secondCommand)
	require.NoError(t, err)
	require.True(t, second.Applied)
	require.Equal(t, service.BillingTypeBalance, second.BillingType)
	require.Nil(t, second.SubscriptionID)

	replayed, err := repo.Apply(ctx, secondCommand)
	require.NoError(t, err)
	require.False(t, replayed.Applied)
	require.Equal(t, service.BillingTypeBalance, replayed.BillingType)
	require.Nil(t, replayed.SubscriptionID)
	_, err = integrationDB.ExecContext(ctx, `
		INSERT INTO usage_billing_dedup_archive
		    (request_id, api_key_id, request_fingerprint, billing_type, subscription_id, created_at)
		SELECT request_id, api_key_id, request_fingerprint, billing_type, subscription_id, created_at
		FROM usage_billing_dedup WHERE request_id = $1 AND api_key_id = $2
	`, secondCommand.RequestID, keyID)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `DELETE FROM usage_billing_dedup WHERE request_id = $1 AND api_key_id = $2`, secondCommand.RequestID, keyID)
	require.NoError(t, err)
	archivedReplay, err := repo.Apply(ctx, secondCommand)
	require.NoError(t, err)
	require.False(t, archivedReplay.Applied)
	require.Equal(t, service.BillingTypeBalance, archivedReplay.BillingType)
	require.Nil(t, archivedReplay.SubscriptionID)

	var dailyUsage, balance float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT daily_usage_usd FROM user_subscriptions WHERE id = $1`, subID).Scan(&dailyUsage))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT balance FROM users WHERE id = $1`, userID).Scan(&balance))
	require.InDelta(t, 0.6, dailyUsage, 0.00000001)
	require.InDelta(t, 99.4, balance, 0.00000001)
}

func TestUsageBillingRepositoryApply_HybridConcurrentCapHasOneWalletFallback(t *testing.T) {
	userID, keyID, subID := createHybridBillingFixture(t)
	var groupID int64
	require.NoError(t, integrationDB.QueryRowContext(context.Background(), `SELECT group_id FROM user_subscriptions WHERE id = $1`, subID).Scan(&groupID))
	repo := NewUsageBillingRepository(testEntClient(t), integrationDB)
	type outcome struct {
		result *service.UsageBillingApplyResult
		err    error
	}
	results := make(chan outcome, 2)
	var workers sync.WaitGroup
	for i := 0; i < 2; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			result, err := repo.Apply(ctx, hybridBillingCommand(userID, keyID, subID, groupID, uuid.NewString(), 0.6))
			results <- outcome{result: result, err: err}
		}()
	}
	workers.Wait()
	close(results)
	var subscriptionCount, balanceCount int
	for item := range results {
		require.NoError(t, item.err)
		require.NotNil(t, item.result)
		switch item.result.BillingType {
		case service.BillingTypeSubscription:
			subscriptionCount++
		case service.BillingTypeBalance:
			balanceCount++
		}
	}
	require.Equal(t, 1, subscriptionCount)
	require.Equal(t, 1, balanceCount)
}

func TestUsageBillingRepositoryApply_HybridRefreshesWindowsAtSettlement(t *testing.T) {
	ctx := context.Background()
	userID, keyID, subID := createHybridBillingFixture(t)
	var groupID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT group_id FROM user_subscriptions WHERE id = $1`, subID).Scan(&groupID))
	_, err := integrationDB.ExecContext(ctx, `UPDATE groups SET weekly_limit_usd = 1, monthly_limit_usd = 1 WHERE id = $1`, groupID)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `
		UPDATE user_subscriptions
		SET starts_at = NOW() - interval '40 days', expires_at = NOW() + interval '90 days',
		    daily_window_start = NOW() - interval '1 day', weekly_window_start = NOW() - interval '8 days',
		    monthly_window_start = NOW() - interval '31 days',
		    daily_usage_usd = 0.9, weekly_usage_usd = 0.9, monthly_usage_usd = 0.9
		WHERE id = $1
	`, subID)
	require.NoError(t, err)

	repo := NewUsageBillingRepository(testEntClient(t), integrationDB)
	result, err := repo.Apply(ctx, hybridBillingCommand(userID, keyID, subID, groupID, uuid.NewString(), 0.6))
	require.NoError(t, err)
	require.Equal(t, service.BillingTypeSubscription, result.BillingType)
	var daily, weekly, monthly float64
	require.NoError(t, integrationDB.QueryRowContext(ctx,
		`SELECT daily_usage_usd, weekly_usage_usd, monthly_usage_usd FROM user_subscriptions WHERE id = $1`, subID,
	).Scan(&daily, &weekly, &monthly))
	require.InDelta(t, 0.6, daily, 0.00000001)
	require.InDelta(t, 0.6, weekly, 0.00000001)
	require.InDelta(t, 0.6, monthly, 0.00000001)
}

func TestUsageBillingRepositoryApply_HybridWalletWithoutSelectedSubscription(t *testing.T) {
	ctx := context.Background()
	userID, keyID, subID := createHybridBillingFixture(t)
	var groupID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT group_id FROM user_subscriptions WHERE id = $1`, subID).Scan(&groupID))
	cmd := hybridBillingCommand(userID, keyID, subID, groupID, uuid.NewString(), 0.6)
	cmd.SubscriptionID = nil // Admission selected the wallet for this request.
	cmd.BillingType = service.BillingTypeBalance
	cmd.SubscriptionCost = 0
	cmd.BalanceCost = 0.6
	repo := NewUsageBillingRepository(testEntClient(t), integrationDB)
	result, err := repo.Apply(ctx, cmd)
	require.NoError(t, err)
	require.Equal(t, service.BillingTypeBalance, result.BillingType)
	require.Nil(t, result.SubscriptionID)
	var daily, balance float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT daily_usage_usd FROM user_subscriptions WHERE id = $1`, subID).Scan(&daily))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT balance FROM users WHERE id = $1`, userID).Scan(&balance))
	require.Zero(t, daily)
	require.InDelta(t, 99.4, balance, 0.00000001)
}

func TestUsageBillingRepositoryApply_HybridSuspendedSubscriptionDoesNotChargeWallet(t *testing.T) {
	ctx := context.Background()
	userID, keyID, subID := createHybridBillingFixture(t)
	var groupID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT group_id FROM user_subscriptions WHERE id = $1`, subID).Scan(&groupID))
	_, err := integrationDB.ExecContext(ctx, `UPDATE user_subscriptions SET status = 'suspended' WHERE id = $1`, subID)
	require.NoError(t, err)
	repo := NewUsageBillingRepository(testEntClient(t), integrationDB)
	_, err = repo.Apply(ctx, hybridBillingCommand(userID, keyID, subID, groupID, uuid.NewString(), 0.6))
	require.ErrorIs(t, err, service.ErrSubscriptionSuspended)
	var balance float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT balance FROM users WHERE id = $1`, userID).Scan(&balance))
	require.InDelta(t, 100.0, balance, 0.00000001)
}

func TestUsageBillingRepositoryApply_HybridArchiveCleanupKeepsSettledSource(t *testing.T) {
	ctx := context.Background()
	userID, keyID, subID := createHybridBillingFixture(t)
	var groupID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT group_id FROM user_subscriptions WHERE id = $1`, subID).Scan(&groupID))
	cmd := hybridBillingCommand(userID, keyID, subID, groupID, uuid.NewString(), 0.6)
	repo := NewUsageBillingRepository(testEntClient(t), integrationDB)
	result, err := repo.Apply(ctx, cmd)
	require.NoError(t, err)
	require.Equal(t, service.BillingTypeSubscription, result.BillingType)

	_, err = integrationDB.ExecContext(ctx, `UPDATE usage_billing_dedup SET created_at = NOW() - interval '2 days' WHERE request_id = $1 AND api_key_id = $2`, cmd.RequestID, keyID)
	require.NoError(t, err)
	require.NoError(t, newDashboardAggregationRepositoryWithSQL(integrationDB).CleanupUsageBillingDedup(ctx, time.Now().Add(-24*time.Hour)))
	var storedType int8
	var storedSubID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx,
		`SELECT billing_type, subscription_id FROM usage_billing_dedup_archive WHERE request_id = $1 AND api_key_id = $2`,
		cmd.RequestID, keyID,
	).Scan(&storedType, &storedSubID))
	require.Equal(t, service.BillingTypeSubscription, storedType)
	require.Equal(t, subID, storedSubID)

	replay, err := repo.Apply(ctx, cmd)
	require.NoError(t, err)
	require.False(t, replay.Applied)
	require.Equal(t, service.BillingTypeSubscription, replay.BillingType)
	require.NotNil(t, replay.SubscriptionID)
	require.Equal(t, subID, *replay.SubscriptionID)
}
