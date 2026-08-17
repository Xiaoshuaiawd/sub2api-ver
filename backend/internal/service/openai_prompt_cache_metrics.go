package service

import (
	"strings"
	"sync/atomic"
)

type OpenAIPromptCacheMetricsSnapshot struct {
	EligibleRequests  int64
	EligibleHits      int64
	EligibleInput     int64
	EligibleCached    int64
	EligibleHitRate   float64
	EligibleTokenRate float64
}

var openAIPromptCacheMetrics struct {
	eligibleRequests atomic.Int64
	eligibleHits     atomic.Int64
	eligibleInput    atomic.Int64
	eligibleCached   atomic.Int64
}

func RecordOpenAIPromptCacheOutcome(model string, inputTokens, cachedTokens int) {
	if inputTokens <= 0 || inputTokens < openAIPromptCacheMinimumInputTokens(model) {
		return
	}
	if cachedTokens < 0 {
		cachedTokens = 0
	}
	if cachedTokens > inputTokens {
		cachedTokens = inputTokens
	}
	openAIPromptCacheMetrics.eligibleRequests.Add(1)
	openAIPromptCacheMetrics.eligibleInput.Add(int64(inputTokens))
	openAIPromptCacheMetrics.eligibleCached.Add(int64(cachedTokens))
	if cachedTokens > 0 {
		openAIPromptCacheMetrics.eligibleHits.Add(1)
	}
}

func SnapshotOpenAIPromptCacheMetrics() OpenAIPromptCacheMetricsSnapshot {
	snapshot := OpenAIPromptCacheMetricsSnapshot{
		EligibleRequests: openAIPromptCacheMetrics.eligibleRequests.Load(),
		EligibleHits:     openAIPromptCacheMetrics.eligibleHits.Load(),
		EligibleInput:    openAIPromptCacheMetrics.eligibleInput.Load(),
		EligibleCached:   openAIPromptCacheMetrics.eligibleCached.Load(),
	}
	if snapshot.EligibleRequests > 0 {
		snapshot.EligibleHitRate = float64(snapshot.EligibleHits) / float64(snapshot.EligibleRequests)
	}
	if snapshot.EligibleInput > 0 {
		snapshot.EligibleTokenRate = float64(snapshot.EligibleCached) / float64(snapshot.EligibleInput)
	}
	return snapshot
}

func openAIPromptCacheMinimumInputTokens(model string) int {
	family := canonicalOpenAIPromptCacheModel(model)
	if strings.HasPrefix(family, "gpt-5.6-") {
		return 1024
	}
	return 2048
}
