//go:build unit

package repository

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func newOpenAIPromptCacheIdentityTestStore(t *testing.T) (service.OpenAIPromptCacheIdentityStore, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	store, ok := NewGatewayCache(rdb).(service.OpenAIPromptCacheIdentityStore)
	require.True(t, ok)
	return store, mr
}

func newTestUUIDv7(t *testing.T) string {
	t.Helper()
	value, err := uuid.NewV7()
	require.NoError(t, err)
	return value.String()
}

func TestOpenAIPromptCacheIdentityResolveUsesFixedTTLWithoutRefresh(t *testing.T) {
	store, mr := newOpenAIPromptCacheIdentityTestStore(t)
	ctx := context.Background()
	firstCandidate := newTestUUIDv7(t)

	first, err := store.ResolveOpenAIPromptCacheIdentity(ctx, 11, "gpt-5.6", "session:alpha", firstCandidate, 5*time.Minute)
	require.NoError(t, err)
	require.Equal(t, firstCandidate, first.Value)
	require.False(t, first.Hit)
	require.Equal(t, 5*time.Minute, first.RemainingTTL)

	mr.FastForward(2 * time.Minute)
	second, err := store.ResolveOpenAIPromptCacheIdentity(ctx, 11, "gpt-5.6", "session:alpha", newTestUUIDv7(t), 5*time.Minute)
	require.NoError(t, err)
	require.Equal(t, firstCandidate, second.Value)
	require.True(t, second.Hit)
	require.Equal(t, 3*time.Minute, second.RemainingTTL)

	mr.FastForward(2 * time.Minute)
	third, err := store.ResolveOpenAIPromptCacheIdentity(ctx, 11, "gpt-5.6", "session:alpha", newTestUUIDv7(t), 5*time.Minute)
	require.NoError(t, err)
	require.Equal(t, firstCandidate, third.Value)
	require.Equal(t, time.Minute, third.RemainingTTL, "cache hit must not refresh the original five-minute lifetime")
}

func TestOpenAIPromptCacheIdentityResolveReplacesValueAfterExpiry(t *testing.T) {
	store, mr := newOpenAIPromptCacheIdentityTestStore(t)
	ctx := context.Background()
	firstCandidate := newTestUUIDv7(t)
	secondCandidate := newTestUUIDv7(t)

	_, err := store.ResolveOpenAIPromptCacheIdentity(ctx, 12, "gpt-5.6", "session:beta", firstCandidate, 5*time.Minute)
	require.NoError(t, err)
	mr.FastForward(5 * time.Minute)

	replaced, err := store.ResolveOpenAIPromptCacheIdentity(ctx, 12, "gpt-5.6", "session:beta", secondCandidate, 5*time.Minute)
	require.NoError(t, err)
	require.Equal(t, secondCandidate, replaced.Value)
	require.False(t, replaced.Hit)
	require.Equal(t, 5*time.Minute, replaced.RemainingTTL)
}

func TestOpenAIPromptCacheIdentityResolveIsolatesTenantModelAndSource(t *testing.T) {
	store, _ := newOpenAIPromptCacheIdentityTestStore(t)
	ctx := context.Background()

	base, err := store.ResolveOpenAIPromptCacheIdentity(ctx, 21, "gpt-5.6", "session:shared", newTestUUIDv7(t), 5*time.Minute)
	require.NoError(t, err)
	otherTenant, err := store.ResolveOpenAIPromptCacheIdentity(ctx, 22, "gpt-5.6", "session:shared", newTestUUIDv7(t), 5*time.Minute)
	require.NoError(t, err)
	otherModel, err := store.ResolveOpenAIPromptCacheIdentity(ctx, 21, "gpt-5.5", "session:shared", newTestUUIDv7(t), 5*time.Minute)
	require.NoError(t, err)
	otherSource, err := store.ResolveOpenAIPromptCacheIdentity(ctx, 21, "gpt-5.6", "session:other", newTestUUIDv7(t), 5*time.Minute)
	require.NoError(t, err)

	require.NotEqual(t, base.Value, otherTenant.Value)
	require.NotEqual(t, base.Value, otherModel.Value)
	require.NotEqual(t, base.Value, otherSource.Value)
}

func TestOpenAIPromptCacheIdentityResponseAliasKeepsRemainingTTL(t *testing.T) {
	store, mr := newOpenAIPromptCacheIdentityTestStore(t)
	ctx := context.Background()
	value := newTestUUIDv7(t)

	bound, err := store.SetOpenAIPromptCacheResponseAlias(ctx, 31, "gpt-5.6", "resp_alpha", value, 2*time.Minute)
	require.NoError(t, err)
	require.True(t, bound)

	mr.FastForward(30 * time.Second)
	alias, err := store.GetOpenAIPromptCacheResponseAlias(ctx, 31, "gpt-5.6", "resp_alpha")
	require.NoError(t, err)
	require.NotNil(t, alias)
	require.Equal(t, value, alias.Value)
	require.Equal(t, 90*time.Second, alias.RemainingTTL)

	mr.FastForward(90 * time.Second)
	missing, err := store.GetOpenAIPromptCacheResponseAlias(ctx, 31, "gpt-5.6", "resp_alpha")
	require.NoError(t, err)
	require.Nil(t, missing)
}

func TestOpenAIPromptCacheIdentityConcurrentResolveReturnsOneUUID(t *testing.T) {
	store, _ := newOpenAIPromptCacheIdentityTestStore(t)
	ctx := context.Background()
	const workers = 24
	values := make(chan string, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			candidate, candidateErr := uuid.NewV7()
			if candidateErr != nil {
				errs <- candidateErr
				return
			}
			record, resolveErr := store.ResolveOpenAIPromptCacheIdentity(ctx, 41, "gpt-5.6", "session:concurrent", candidate.String(), 5*time.Minute)
			if resolveErr != nil {
				errs <- resolveErr
				return
			}
			values <- record.Value
		}()
	}
	wg.Wait()
	close(values)
	close(errs)

	for err := range errs {
		require.NoError(t, err)
	}
	unique := map[string]struct{}{}
	for value := range values {
		unique[value] = struct{}{}
	}
	require.Len(t, unique, 1)
}
