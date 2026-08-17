package service

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenAIPromptCacheMetricsEligibility(t *testing.T) {
	before := SnapshotOpenAIPromptCacheMetrics()
	RecordOpenAIPromptCacheOutcome("gpt-5.6-sol", 1023, 100)
	RecordOpenAIPromptCacheOutcome("gpt-5.6-sol", 1024, 512)
	RecordOpenAIPromptCacheOutcome("gpt-5.5", 2047, 100)
	RecordOpenAIPromptCacheOutcome("unknown-model", 2048, 3000)
	after := SnapshotOpenAIPromptCacheMetrics()

	require.Equal(t, before.EligibleRequests+2, after.EligibleRequests)
	require.Equal(t, before.EligibleHits+2, after.EligibleHits)
	require.Equal(t, before.EligibleInput+3072, after.EligibleInput)
	require.Equal(t, before.EligibleCached+2560, after.EligibleCached)
	require.InDelta(t, float64(after.EligibleHits-before.EligibleHits)/float64(after.EligibleRequests-before.EligibleRequests), 1, 0.000001)
	require.InDelta(t, float64(after.EligibleCached-before.EligibleCached)/float64(after.EligibleInput-before.EligibleInput), float64(2560)/3072, 0.000001)
}

func TestOpenAIPromptCacheMetricsConcurrentSnapshot(t *testing.T) {
	const workers = 8
	const records = 100
	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < records; i++ {
				RecordOpenAIPromptCacheOutcome("gpt-5.6-sol", 1024, 256)
				_ = SnapshotOpenAIPromptCacheMetrics()
			}
		}()
	}
	wg.Wait()
	require.GreaterOrEqual(t, SnapshotOpenAIPromptCacheMetrics().EligibleRequests, int64(workers*records))
}
