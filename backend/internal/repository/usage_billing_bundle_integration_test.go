//go:build integration

package repository

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func TestUsageBillingRepositoryApply_SharedPlanQuotaAcrossGroups(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	user := mustCreateUser(t, client, &service.User{Email: "bundle-" + uuid.NewString() + "@example.com", PasswordHash: "hash", Balance: 10})
	first := mustCreateGroup(t, client, &service.Group{Name: "bundle-a-" + uuid.NewString(), Platform: service.PlatformOpenAI, SubscriptionType: service.SubscriptionTypeSubscription, RateMultiplier: 1})
	second := mustCreateGroup(t, client, &service.Group{Name: "bundle-b-" + uuid.NewString(), Platform: service.PlatformOpenAI, SubscriptionType: service.SubscriptionTypeSubscription, RateMultiplier: 1})
	planID := int64(101)
	limit := 1.0
	sub := mustCreateSubscription(t, client, &service.UserSubscription{UserID: user.ID, GroupID: first.ID, GroupIDs: []int64{first.ID, second.ID}, PlanID: &planID, DailyLimitUSD: &limit, StartsAt: time.Now().Add(-time.Hour), ExpiresAt: time.Now().Add(time.Hour), Status: service.SubscriptionStatusActive})
	_, err := integrationDB.ExecContext(ctx, `UPDATE user_subscriptions SET daily_window_start = NOW(), weekly_window_start = NOW(), monthly_window_start = NOW() WHERE id = $1`, sub.ID)
	require.NoError(t, err)
	firstKey := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, GroupID: &first.ID, Key: "sk-bundle-a-" + uuid.NewString(), Name: "bundle-a"})
	secondKey := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, GroupID: &second.ID, Key: "sk-bundle-b-" + uuid.NewString(), Name: "bundle-b"})
	repo := NewUsageBillingRepository(client, integrationDB)
	for _, item := range []struct{ groupID, keyID int64 }{{first.ID, firstKey.ID}, {second.ID, secondKey.ID}} {
		cmd := &service.UsageBillingCommand{RequestID: uuid.NewString(), UserID: user.ID, APIKeyID: item.keyID, SubscriptionID: &sub.ID, SubscriptionGroupID: item.groupID, BillingType: service.BillingTypeSubscription, SubscriptionCost: 0.5}
		_, err := repo.Apply(ctx, cmd)
		require.NoError(t, err)
	}
	cmd := &service.UsageBillingCommand{RequestID: uuid.NewString(), UserID: user.ID, APIKeyID: secondKey.ID, SubscriptionID: &sub.ID, SubscriptionGroupID: second.ID, BillingType: service.BillingTypeSubscription, SubscriptionCost: 0.01}
	_, err = repo.Apply(ctx, cmd)
	require.ErrorIs(t, err, service.ErrDailyLimitExceeded)
	var usage float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT daily_usage_usd FROM user_subscriptions WHERE id=$1`, sub.ID).Scan(&usage))
	require.InDelta(t, 1.0, usage, 1e-8)
}

func TestUsageBillingRepositoryApply_SharedPlanHybridGroupFallsBackToBalance(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	user := mustCreateUser(t, client, &service.User{Email: "bundle-hybrid-" + uuid.NewString() + "@example.com", PasswordHash: "hash", Balance: 10})
	first := mustCreateGroup(t, client, &service.Group{Name: "bundle-hybrid-a-" + uuid.NewString(), Platform: service.PlatformOpenAI, SubscriptionType: service.SubscriptionTypeSubscription, RateMultiplier: 1})
	second := mustCreateGroup(t, client, &service.Group{Name: "bundle-hybrid-b-" + uuid.NewString(), Platform: service.PlatformOpenAI, SubscriptionType: service.SubscriptionTypeSubscriptionBalance, RateMultiplier: 1})
	planID, limit := int64(102), 0.5
	sub := mustCreateSubscription(t, client, &service.UserSubscription{UserID: user.ID, GroupID: first.ID, GroupIDs: []int64{first.ID, second.ID}, PlanID: &planID, DailyLimitUSD: &limit, StartsAt: time.Now().Add(-time.Hour), ExpiresAt: time.Now().Add(time.Hour), Status: service.SubscriptionStatusActive})
	_, err := integrationDB.ExecContext(ctx, `UPDATE user_subscriptions SET daily_window_start = NOW(), weekly_window_start = NOW(), monthly_window_start = NOW() WHERE id = $1`, sub.ID)
	require.NoError(t, err)
	key := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, GroupID: &second.ID, Key: "sk-bundle-hybrid-" + uuid.NewString(), Name: "bundle-hybrid"})
	repo := NewUsageBillingRepository(client, integrationDB)
	firstCommand := &service.UsageBillingCommand{RequestID: uuid.NewString(), UserID: user.ID, APIKeyID: key.ID, SubscriptionID: &sub.ID, SubscriptionGroupID: second.ID, BillingType: service.BillingTypeSubscription, SubscriptionCost: 0.4, HybridGroupID: second.ID, HybridCostUSD: 0.4}
	firstResult, err := repo.Apply(ctx, firstCommand)
	require.NoError(t, err)
	require.Equal(t, service.BillingTypeSubscription, firstResult.BillingType)
	secondCommand := &service.UsageBillingCommand{RequestID: uuid.NewString(), UserID: user.ID, APIKeyID: key.ID, SubscriptionID: &sub.ID, SubscriptionGroupID: second.ID, BillingType: service.BillingTypeSubscription, SubscriptionCost: 0.2, HybridGroupID: second.ID, HybridCostUSD: 0.2}
	secondResult, err := repo.Apply(ctx, secondCommand)
	require.NoError(t, err)
	require.Equal(t, service.BillingTypeBalance, secondResult.BillingType)
	var usage, balance float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT daily_usage_usd FROM user_subscriptions WHERE id=$1`, sub.ID).Scan(&usage))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT balance FROM users WHERE id=$1`, user.ID).Scan(&balance))
	require.InDelta(t, 0.4, usage, 1e-8)
	require.InDelta(t, 9.8, balance, 1e-8)
}

func TestUsageBillingRepositoryApply_ConcurrentGroupsCannotExceedSharedCap(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	user := mustCreateUser(t, client, &service.User{Email: "bundle-race-" + uuid.NewString() + "@example.com", PasswordHash: "hash", Balance: 10})
	first := mustCreateGroup(t, client, &service.Group{Name: "bundle-race-a-" + uuid.NewString(), Platform: service.PlatformOpenAI, SubscriptionType: service.SubscriptionTypeSubscription, RateMultiplier: 1})
	second := mustCreateGroup(t, client, &service.Group{Name: "bundle-race-b-" + uuid.NewString(), Platform: service.PlatformOpenAI, SubscriptionType: service.SubscriptionTypeSubscription, RateMultiplier: 1})
	planID, limit := int64(103), 1.0
	sub := mustCreateSubscription(t, client, &service.UserSubscription{UserID: user.ID, GroupID: first.ID, GroupIDs: []int64{first.ID, second.ID}, PlanID: &planID, DailyLimitUSD: &limit, StartsAt: time.Now().Add(-time.Hour), ExpiresAt: time.Now().Add(time.Hour), Status: service.SubscriptionStatusActive})
	_, err := integrationDB.ExecContext(ctx, `UPDATE user_subscriptions SET daily_window_start = NOW(), weekly_window_start = NOW(), monthly_window_start = NOW() WHERE id = $1`, sub.ID)
	require.NoError(t, err)
	firstKey := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, GroupID: &first.ID, Key: "sk-bundle-race-a-" + uuid.NewString(), Name: "bundle-race-a"})
	secondKey := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, GroupID: &second.ID, Key: "sk-bundle-race-b-" + uuid.NewString(), Name: "bundle-race-b"})
	repo := NewUsageBillingRepository(client, integrationDB)
	results := make(chan error, 2)
	var wait sync.WaitGroup
	for _, item := range []struct{ groupID, keyID int64 }{{first.ID, firstKey.ID}, {second.ID, secondKey.ID}} {
		wait.Add(1)
		go func(groupID, keyID int64) {
			defer wait.Done()
			_, applyErr := repo.Apply(ctx, &service.UsageBillingCommand{RequestID: uuid.NewString(), UserID: user.ID, APIKeyID: keyID, SubscriptionID: &sub.ID, SubscriptionGroupID: groupID, BillingType: service.BillingTypeSubscription, SubscriptionCost: 0.6})
			results <- applyErr
		}(item.groupID, item.keyID)
	}
	wait.Wait()
	close(results)
	var success, exhausted int
	for applyErr := range results {
		if applyErr == nil {
			success++
		} else if errors.Is(applyErr, service.ErrDailyLimitExceeded) {
			exhausted++
		} else {
			t.Fatalf("unexpected billing error: %v", applyErr)
		}
	}
	require.Equal(t, 1, success)
	require.Equal(t, 1, exhausted)
	var usage float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT daily_usage_usd FROM user_subscriptions WHERE id=$1`, sub.ID).Scan(&usage))
	require.InDelta(t, 0.6, usage, 1e-8)
}
