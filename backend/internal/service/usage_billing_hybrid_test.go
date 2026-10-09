//go:build unit

package service

import (
	"testing"
	"time"

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

func TestHybridBillingCandidatesUseSeparateSubscriptionAndBalanceRates(t *testing.T) {
	groupID, subID := int64(40), int64(71)
	quotaRate := 1.0
	group := &Group{ID: groupID, SubscriptionType: SubscriptionTypeSubscriptionBalance, RateMultiplier: 0.25, SubscriptionRateMultiplier: &quotaRate}
	p := &postUsageBillingParams{
		Cost: &CostBreakdown{TotalCost: 1, ActualCost: 1}, User: &User{ID: 20},
		APIKey: &APIKey{ID: 10, GroupID: &groupID, Group: group}, Account: &Account{ID: 30},
		Subscription: &UserSubscription{ID: subID}, IsSubscriptionBill: true,
		HybridSplitPricing: true, HybridSubscriptionActualCost: 1, HybridBalanceActualCost: 0.25,
		HybridSubscriptionRateMultiplier: 1, HybridBalanceRateMultiplier: 0.25,
	}
	log := &UsageLog{BillingType: BillingTypeSubscription, SubscriptionID: &subID, ActualCost: 1, RateMultiplier: 1}
	cmd := buildUsageBillingCommand("hybrid-split", log, p)
	require.Equal(t, 1.0, cmd.HybridSubscriptionCostUSD)
	require.Equal(t, 0.25, cmd.HybridBalanceCostUSD)
	applyCommittedBillingSource(log, p, &UsageBillingApplyResult{Applied: true, BillingType: BillingTypeBalance})
	require.Equal(t, 0.25, p.Cost.ActualCost)
	require.Equal(t, 0.25, log.ActualCost)
	require.Equal(t, 0.25, log.RateMultiplier)
}

func TestCalculateHybridPriceCandidatesKeepsIndependentImageRate(t *testing.T) {
	quotaRate := 1.0
	apiKey := &APIKey{Group: &Group{SubscriptionType: SubscriptionTypeSubscriptionBalance, RateMultiplier: 0.25, SubscriptionRateMultiplier: &quotaRate, ImageRateIndependent: true, ImageRateMultiplier: 0.5}}
	pricing := calculateHybridPriceCandidates(apiKey, 1, 0, &CostBreakdown{TotalCost: 2, ActualCost: 1, BillingMode: string(BillingModeImage)}, time.Now(), 1, 0.25, true)
	require.Equal(t, 1.0, pricing.subscriptionCost)
	require.Equal(t, 1.0, pricing.balanceCost)
	require.Equal(t, 0.5, pricing.balanceRate)
}

func TestCalculateHybridPriceCandidatesUsesChargedTierForWallet(t *testing.T) {
	quotaRate := 1.0
	apiKey := &APIKey{Group: &Group{SubscriptionType: SubscriptionTypeSubscriptionBalance, RateMultiplier: 0.25, SubscriptionRateMultiplier: &quotaRate}}
	pricing := calculateHybridPriceCandidates(apiKey, 0, 0, &CostBreakdown{TotalCost: 10, ActualCost: 2, BillingMode: string(BillingModeToken)}, time.Now(), 1, 0.25, true)
	require.Equal(t, 2.0, pricing.subscriptionCost)
	require.Equal(t, 0.5, pricing.balanceCost)
}
