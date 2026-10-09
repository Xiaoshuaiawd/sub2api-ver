//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPaymentConfigServiceCreatesAndEditsSharedGroupPlan(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	first, err := client.Group.Create().SetName("plan-openai").SetStatus(StatusActive).SetSubscriptionType(SubscriptionTypeSubscription).Save(ctx)
	require.NoError(t, err)
	second, err := client.Group.Create().SetName("plan-gemini").SetStatus(StatusActive).SetSubscriptionType(SubscriptionTypeSubscriptionBalance).Save(ctx)
	require.NoError(t, err)
	svc := NewPaymentConfigService(client, nil, nil)
	limit := 10.0
	plan, err := svc.CreatePlan(ctx, CreatePlanRequest{GroupIDs: []int64{first.ID, second.ID}, Name: "Shared", Price: 9.99, ValidityDays: 30, ValidityUnit: "days", DailyLimitUSD: &limit})
	require.NoError(t, err)
	require.Equal(t, first.ID, plan.GroupID)
	require.Equal(t, []int64{first.ID, second.ID}, plan.GroupIds)
	require.Equal(t, &limit, plan.DailyLimitUsd)

	zero := 0.0
	plan, err = svc.UpdatePlan(ctx, plan.ID, UpdatePlanRequest{GroupIDs: []int64{second.ID, first.ID}, DailyLimitUSD: &zero})
	require.NoError(t, err)
	require.Equal(t, first.ID, plan.GroupID)
	require.Equal(t, []int64{first.ID, second.ID}, plan.GroupIds)
	require.Nil(t, plan.DailyLimitUsd)
}

func TestPaymentConfigServiceLegacySingleGroupPlanKeepsGroupLimit(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	group, err := client.Group.Create().SetName("legacy-plan-group").SetStatus(StatusActive).SetSubscriptionType(SubscriptionTypeSubscription).SetDailyLimitUsd(5).Save(ctx)
	require.NoError(t, err)
	svc := NewPaymentConfigService(client, nil, nil)
	plan, err := svc.CreatePlan(ctx, CreatePlanRequest{GroupID: group.ID, Name: "Legacy", Price: 9.99, ValidityDays: 30, ValidityUnit: "days"})
	require.NoError(t, err)
	require.Equal(t, []int64{group.ID}, plan.GroupIds)
	require.Equal(t, 5.0, *plan.DailyLimitUsd)
}
