package service

import "time"

type hybridPriceCandidates struct {
	subscriptionCost float64
	balanceCost      float64
	subscriptionRate float64
	balanceRate      float64
}

// Group multipliers affect ActualCost linearly. Reusing the already selected
// model/tier price keeps response-model, Free Fast, channel pricing and long-
// context decisions identical for both possible funding sources.
func calculateHybridPriceCandidates(apiKey *APIKey, imageCount, videoCount int, cost *CostBreakdown, pricingAt time.Time, subscriptionBase, balanceBase float64, selectedSubscription bool) hybridPriceCandidates {
	if apiKey == nil || apiKey.Group == nil || !apiKey.Group.AllowsBalanceFallback() || cost == nil {
		return hybridPriceCandidates{}
	}
	subscriptionRate := effectiveUsageRateForCost(apiKey, imageCount, videoCount, cost, pricingAt, subscriptionBase)
	balanceRate := effectiveUsageRateForCost(apiKey, imageCount, videoCount, cost, pricingAt, balanceBase)
	selectedRate := balanceRate
	if selectedSubscription {
		selectedRate = subscriptionRate
	}
	rescale := func(rate float64) float64 {
		if selectedRate <= 0 {
			return 0
		}
		return QuantizeUsageBillingAmount(cost.ActualCost * rate / selectedRate)
	}
	return hybridPriceCandidates{subscriptionCost: rescale(subscriptionRate), balanceCost: rescale(balanceRate), subscriptionRate: subscriptionRate, balanceRate: balanceRate}
}

func effectiveUsageRateForCost(apiKey *APIKey, imageCount, videoCount int, cost *CostBreakdown, pricingAt time.Time, base float64) float64 {
	textRate, imageRate := computePeakAwareMultipliers(apiKey, base, pricingAt)
	if cost != nil && cost.BillingMode != string(BillingModeToken) {
		if videoCount > 0 {
			return resolveVideoRateMultiplier(apiKey, base)
		}
		if imageCount > 0 {
			return imageRate
		}
	}
	return textRate
}
