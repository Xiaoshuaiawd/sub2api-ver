//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHybridBillingFingerprintDoesNotDependOnFundingSource(t *testing.T) {
	subID := int64(71)
	subscriptionCandidate := &UsageBillingCommand{
		RequestID: "request-1", APIKeyID: 10, UserID: 20, AccountID: 30,
		Model: "public-model", HybridGroupID: 40, HybridCostUSD: 0.6,
		BillingType: BillingTypeSubscription, SubscriptionID: &subID, SubscriptionCost: 0.6,
	}
	walletCandidate := &UsageBillingCommand{
		RequestID: "request-1", APIKeyID: 10, UserID: 20, AccountID: 30,
		Model: "public-model", HybridGroupID: 40, HybridCostUSD: 0.6,
		BillingType: BillingTypeBalance, BalanceCost: 0.6,
	}
	subscriptionCandidate.Normalize()
	walletCandidate.Normalize()
	require.Equal(t, subscriptionCandidate.RequestFingerprint, walletCandidate.RequestFingerprint)
	require.NotEmpty(t, subscriptionCandidate.RequestFingerprint)
	require.Equal(t, 0.6, subscriptionCandidate.HybridCostUSD)
}

func TestBuildUsageBillingCommandCarriesHybridCandidate(t *testing.T) {
	groupID, subID := int64(40), int64(71)
	group := &Group{ID: groupID, SubscriptionType: SubscriptionTypeSubscriptionBalance}
	p := &postUsageBillingParams{
		Cost: &CostBreakdown{TotalCost: 0.4, ActualCost: 0.6}, User: &User{ID: 20},
		APIKey: &APIKey{ID: 10, GroupID: &groupID, Group: group}, Account: &Account{ID: 30},
		Subscription: &UserSubscription{ID: subID}, IsSubscriptionBill: true,
	}
	cmd := buildUsageBillingCommand("request-1", nil, p)
	require.NotNil(t, cmd)
	require.Equal(t, groupID, cmd.HybridGroupID)
	require.Equal(t, 0.6, cmd.HybridCostUSD)
	require.Equal(t, 0.6, cmd.SubscriptionCost)
	require.Zero(t, cmd.BalanceCost)
}

func TestCommittedHybridFundingSourceControlsLogAndCacheBranch(t *testing.T) {
	groupID, subID := int64(40), int64(71)
	p := &postUsageBillingParams{
		Cost:         &CostBreakdown{ActualCost: 0.6},
		APIKey:       &APIKey{ID: 10, GroupID: &groupID, Group: &Group{ID: groupID, SubscriptionType: SubscriptionTypeSubscriptionBalance}},
		Subscription: &UserSubscription{ID: subID}, IsSubscriptionBill: true,
	}
	log := &UsageLog{BillingType: BillingTypeSubscription, SubscriptionID: &subID}
	applyCommittedBillingSource(log, p, &UsageBillingApplyResult{Applied: true, BillingType: BillingTypeBalance})
	require.Equal(t, BillingTypeBalance, log.BillingType)
	require.Nil(t, log.SubscriptionID)
	require.False(t, p.IsSubscriptionBill)
	require.Nil(t, p.Subscription)

	// A duplicate request retains the originally settled source even if
	// the admission snapshot would now choose subscription.
	p.Subscription = &UserSubscription{ID: subID}
	p.IsSubscriptionBill = true
	log.SubscriptionID = &subID
	applyCommittedBillingSource(log, p, &UsageBillingApplyResult{Applied: false, BillingType: BillingTypeBalance})
	require.Equal(t, BillingTypeBalance, log.BillingType)
	require.Nil(t, log.SubscriptionID)
}
