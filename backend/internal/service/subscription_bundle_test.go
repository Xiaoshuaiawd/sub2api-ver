//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizePlanGroupIDs(t *testing.T) {
	groups, err := normalizePlanGroupIDs(0, []int64{8, 3, 8})
	require.NoError(t, err)
	require.Equal(t, []int64{8, 3}, groups)

	groups, err = normalizePlanGroupIDs(5, nil)
	require.NoError(t, err)
	require.Equal(t, []int64{5}, groups)

	_, err = normalizePlanGroupIDs(0, []int64{3, 0})
	require.Error(t, err)
	_, err = normalizePlanGroupIDs(0, nil)
	require.Error(t, err)
}

func TestUserSubscriptionBundleUsesOneSharedLimit(t *testing.T) {
	groupA := &Group{ID: 8, DailyLimitUSD: float64Pointer(100)}
	groupB := &Group{ID: 3, DailyLimitUSD: float64Pointer(200)}
	planID := int64(19)
	sub := &UserSubscription{
		GroupID: 8, GroupIDs: []int64{8, 3}, PlanID: &planID,
		DailyLimitUSD: float64Pointer(10), DailyUsageUSD: 9,
	}
	require.True(t, sub.CanUseGroup(8))
	require.True(t, sub.CanUseGroup(3))
	require.False(t, sub.CanUseGroup(5))
	require.False(t, sub.CheckDailyLimit(groupA, 1.01))
	require.False(t, sub.CheckDailyLimit(groupB, 1.01))
	require.True(t, sub.CheckDailyLimit(groupB, 0.5))
}

func TestPaidRenewalPreservesActiveBundleRights(t *testing.T) {
	oldPlanID, newPlanID := int64(1), int64(2)
	oldLimit, newLimit := 10.0, 5.0
	previous := &UserSubscription{GroupID: 8, GroupIDs: []int64{8, 3}, PlanID: &oldPlanID, DailyLimitUSD: &oldLimit}
	current := &UserSubscription{GroupID: 8}
	applyPurchasedBundle(current, previous, &AssignSubscriptionInput{GroupIDs: []int64{8, 4}, PlanID: &newPlanID, DailyLimitUSD: &newLimit}, true)
	require.Equal(t, []int64{8, 3, 4}, current.GroupIDs)
	require.Equal(t, &oldLimit, current.DailyLimitUSD)
	require.Equal(t, &newPlanID, current.PlanID)

	expired := &UserSubscription{GroupID: 8}
	applyPurchasedBundle(expired, previous, &AssignSubscriptionInput{GroupIDs: []int64{8, 4}, PlanID: &newPlanID, DailyLimitUSD: &newLimit}, false)
	require.Equal(t, []int64{8, 4}, expired.GroupIDs)
	require.Equal(t, &newLimit, expired.DailyLimitUSD)
}

func float64Pointer(value float64) *float64 { return &value }
