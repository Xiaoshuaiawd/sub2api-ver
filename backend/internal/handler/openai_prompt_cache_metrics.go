package handler

import (
	"strings"
	"sync/atomic"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"go.uber.org/zap"
)

const openAIPromptCacheMetricsLogEvery = 100

var openAIPromptCacheSuccessfulRequests atomic.Uint64

func recordOpenAIPromptCacheOutcome(reqLog *zap.Logger, account *service.Account, result *service.OpenAIForwardResult) {
	if account == nil || account.Platform != service.PlatformOpenAI || result == nil || !result.SucceededForPromptCacheAlias() {
		return
	}
	model := strings.TrimSpace(result.UpstreamModel)
	if model == "" {
		model = strings.TrimSpace(result.UpstreamResponseModel)
	}
	if model == "" {
		model = strings.TrimSpace(result.BillingModel)
	}
	if model == "" {
		model = strings.TrimSpace(result.Model)
	}
	service.RecordOpenAIPromptCacheOutcome(model, result.Usage.InputTokens, result.Usage.CacheReadInputTokens)
	if reqLog == nil || openAIPromptCacheSuccessfulRequests.Add(1)%openAIPromptCacheMetricsLogEvery != 0 {
		return
	}
	snapshot := service.SnapshotOpenAIPromptCacheMetrics()
	reqLog.Info("openai.prompt_cache_eligible_metrics",
		zap.Int64("eligible_requests", snapshot.EligibleRequests),
		zap.Int64("eligible_hits", snapshot.EligibleHits),
		zap.Int64("eligible_input_tokens", snapshot.EligibleInput),
		zap.Int64("eligible_cached_tokens", snapshot.EligibleCached),
		zap.Float64("eligible_hit_rate", snapshot.EligibleHitRate),
		zap.Float64("eligible_token_rate", snapshot.EligibleTokenRate),
	)
}
