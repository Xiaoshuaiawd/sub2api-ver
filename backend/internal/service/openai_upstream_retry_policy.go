package service

import (
	"context"
	"sync/atomic"
)

const (
	DefaultOpenAIUpstreamRetryLimit = 3
	MaxOpenAIUpstreamRetryLimit     = 10
)

type openAIUpstreamRetryPolicyContextKey struct{}

type openAIUpstreamRetryPolicy struct {
	limit       int32
	used        atomic.Int32
	requestDone <-chan struct{}
}

func WithOpenAIUpstreamRetryLimit(ctx context.Context, retries int) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if retries < 0 {
		retries = 0
	}
	if retries > MaxOpenAIUpstreamRetryLimit {
		retries = MaxOpenAIUpstreamRetryLimit
	}
	return context.WithValue(ctx, openAIUpstreamRetryPolicyContextKey{}, &openAIUpstreamRetryPolicy{
		limit:       int32(retries),
		requestDone: ctx.Done(),
	})
}

func openAIUpstreamRetryPolicyFromContext(ctx context.Context) *openAIUpstreamRetryPolicy {
	if ctx == nil {
		return nil
	}
	policy, _ := ctx.Value(openAIUpstreamRetryPolicyContextKey{}).(*openAIUpstreamRetryPolicy)
	return policy
}

func OpenAIUpstreamRetryLimitFromContext(ctx context.Context) int {
	policy := openAIUpstreamRetryPolicyFromContext(ctx)
	if policy == nil {
		return 0
	}
	return int(policy.limit)
}

func OpenAIUpstreamRetryDone(ctx context.Context) <-chan struct{} {
	policy := openAIUpstreamRetryPolicyFromContext(ctx)
	if policy == nil {
		return nil
	}
	return policy.requestDone
}

func OpenAIUpstreamRetryCanceled(ctx context.Context) bool {
	done := OpenAIUpstreamRetryDone(ctx)
	if done == nil {
		return false
	}
	select {
	case <-done:
		return true
	default:
		return false
	}
}

// AcquireOpenAIUpstreamRetry consumes one retry from the request-wide budget.
// The returned attempt is one-based and counts retries only, not the initial call.
func AcquireOpenAIUpstreamRetry(ctx context.Context) (attempt int, ok bool) {
	policy := openAIUpstreamRetryPolicyFromContext(ctx)
	if policy == nil || policy.limit <= 0 || OpenAIUpstreamRetryCanceled(ctx) {
		return 0, false
	}
	for {
		used := policy.used.Load()
		if used >= policy.limit {
			return 0, false
		}
		if policy.used.CompareAndSwap(used, used+1) {
			return int(used + 1), true
		}
	}
}
