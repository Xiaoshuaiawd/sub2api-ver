package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenAIUpstreamRetryPolicy_AcquiresConfiguredRetryBudget(t *testing.T) {
	ctx := WithOpenAIUpstreamRetryLimit(context.Background(), 3)

	for attempt := 1; attempt <= 3; attempt++ {
		got, ok := AcquireOpenAIUpstreamRetry(ctx)
		require.True(t, ok)
		require.Equal(t, attempt, got)
	}
	_, ok := AcquireOpenAIUpstreamRetry(ctx)
	require.False(t, ok)
}

func TestOpenAIUpstreamRetryPolicy_ClampsRetryLimit(t *testing.T) {
	high := WithOpenAIUpstreamRetryLimit(context.Background(), 999)
	require.Equal(t, MaxOpenAIUpstreamRetryLimit, OpenAIUpstreamRetryLimitFromContext(high))

	negative := WithOpenAIUpstreamRetryLimit(context.Background(), -1)
	require.Equal(t, 0, OpenAIUpstreamRetryLimitFromContext(negative))
}

func TestOpenAIUpstreamRetryPolicy_StopsAfterOriginalRequestCancellation(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	ctx := WithOpenAIUpstreamRetryLimit(parent, 3)
	detached := context.WithoutCancel(ctx)
	cancel()

	require.True(t, OpenAIUpstreamRetryCanceled(detached))
	_, ok := AcquireOpenAIUpstreamRetry(detached)
	require.False(t, ok)
}
