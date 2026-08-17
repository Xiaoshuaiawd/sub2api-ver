package handler

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestOpenAIPromptCacheMetricsRecordOutcome(t *testing.T) {
	before := service.SnapshotOpenAIPromptCacheMetrics()
	result := &service.OpenAIForwardResult{
		Model:         "client-model",
		UpstreamModel: "gpt-5.6-sol",
		Usage: service.OpenAIUsage{
			InputTokens:          1024,
			CacheReadInputTokens: 256,
		},
	}
	recordOpenAIPromptCacheOutcome(nil, &service.Account{Platform: service.PlatformOpenAI}, result)
	recordOpenAIPromptCacheOutcome(nil, &service.Account{Platform: service.PlatformGrok}, result)
	failed := *result
	failed.ResponseStatus = "failed"
	recordOpenAIPromptCacheOutcome(nil, &service.Account{Platform: service.PlatformOpenAI}, &failed)
	after := service.SnapshotOpenAIPromptCacheMetrics()

	require.Equal(t, before.EligibleRequests+1, after.EligibleRequests)
	require.Equal(t, before.EligibleHits+1, after.EligibleHits)
	require.Equal(t, before.EligibleInput+1024, after.EligibleInput)
	require.Equal(t, before.EligibleCached+256, after.EligibleCached)
}
