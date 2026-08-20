package repository

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type schedulerCommandCountHook struct {
	mu     sync.Mutex
	counts map[string]int
}

func newSchedulerCommandCountHook() *schedulerCommandCountHook {
	return &schedulerCommandCountHook{counts: make(map[string]int)}
}

func (h *schedulerCommandCountHook) DialHook(next redis.DialHook) redis.DialHook { return next }

func (h *schedulerCommandCountHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		h.record(cmd)
		return next(ctx, cmd)
	}
}

func (h *schedulerCommandCountHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		for _, cmd := range cmds {
			h.record(cmd)
		}
		return next(ctx, cmds)
	}
}

func (h *schedulerCommandCountHook) record(cmd redis.Cmder) {
	h.mu.Lock()
	h.counts[cmd.Name()]++
	h.mu.Unlock()
}

func (h *schedulerCommandCountHook) reset() {
	h.mu.Lock()
	h.counts = make(map[string]int)
	h.mu.Unlock()
}

func (h *schedulerCommandCountHook) count(name string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.counts[name]
}

func (h *schedulerCommandCountHook) total() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	total := 0
	for _, count := range h.counts {
		total += count
	}
	return total
}

func newSchedulerCacheL1Test(t *testing.T) (*schedulerCache, *schedulerCommandCountHook) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	hook := newSchedulerCommandCountHook()
	rdb.AddHook(hook)
	t.Cleanup(func() { _ = rdb.Close() })
	cache, ok := newSchedulerCacheWithChunkSizes(rdb, 128, 256).(*schedulerCache)
	require.True(t, ok)
	return cache, hook
}

func publishSchedulerL1TestSnapshot(t *testing.T, cache *schedulerCache, bucket service.SchedulerBucket, accounts []service.Account) {
	t.Helper()
	token, err := cache.CaptureBucketWriteToken(context.Background(), bucket)
	require.NoError(t, err)
	require.NoError(t, cache.SetSnapshot(context.Background(), bucket, token, accounts))
}

func TestSchedulerCacheSnapshotL1SkipsPayloadReadsForUnchangedVersion(t *testing.T) {
	ctx := context.Background()
	cache, commands := newSchedulerCacheL1Test(t)
	bucket := service.SchedulerBucket{GroupID: 31, Platform: service.PlatformOpenAI, Mode: service.SchedulerModeSingle}
	publishSchedulerL1TestSnapshot(t, cache, bucket, []service.Account{{
		ID: 3101, Name: "one", Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
	}})

	first, hit, err := cache.GetSnapshot(ctx, bucket)
	require.NoError(t, err)
	require.True(t, hit)
	require.Len(t, first, 1)
	commands.reset()

	second, hit, err := cache.GetSnapshot(ctx, bucket)
	require.NoError(t, err)
	require.True(t, hit)
	require.Len(t, second, 1)
	require.Zero(t, commands.total(), "fresh local version validation should avoid Redis entirely")
	require.Zero(t, commands.count("zrange"))
	require.Zero(t, commands.count("mget"))

	time.Sleep(schedulerSnapshotL1ValidationTTL + 20*time.Millisecond)
	_, hit, err = cache.GetSnapshot(ctx, bucket)
	require.NoError(t, err)
	require.True(t, hit)
	require.Equal(t, 2, commands.count("get"), "expired local validation must recheck ready and active")
}

func TestSchedulerCacheSnapshotReadOnlyUsesConstantAllocations(t *testing.T) {
	ctx := context.Background()
	cache, _ := newSchedulerCacheL1Test(t)
	bucket := service.SchedulerBucket{GroupID: 36, Platform: service.PlatformOpenAI, Mode: service.SchedulerModeSingle}
	accounts := make([]service.Account, 3000)
	for i := range accounts {
		accounts[i] = service.Account{
			ID: int64(36_000 + i), Name: fmt.Sprintf("account-%d", i), Platform: service.PlatformOpenAI,
			Credentials: map[string]any{"model_mapping": map[string]any{"source": "target"}},
		}
	}
	publishSchedulerL1TestSnapshot(t, cache, bucket, accounts)
	_, hit, err := cache.GetSnapshotReadOnly(ctx, bucket)
	require.NoError(t, err)
	require.True(t, hit)

	allocations := testing.AllocsPerRun(20, func() {
		view, viewHit, viewErr := cache.GetSnapshotReadOnly(ctx, bucket)
		if viewErr != nil || !viewHit || len(view) != len(accounts) {
			panic("read-only snapshot miss")
		}
	})
	require.LessOrEqual(t, allocations, float64(2), "read-only snapshot should copy one account-value slice without deep-cloning 3000 maps")

	first, hit, err := cache.GetSnapshotReadOnly(ctx, bucket)
	require.NoError(t, err)
	require.True(t, hit)
	first[0].Name = "mutated"
	second, hit, err := cache.GetSnapshotReadOnly(ctx, bucket)
	require.NoError(t, err)
	require.True(t, hit)
	require.Equal(t, "account-0", second[0].Name, "returned account structs must not alias the cached slice")
}

func TestOpenAIAdaptiveRedisHotPathUsesAtMostThreeCommands(t *testing.T) {
	ctx := context.Background()
	cache, commands := newSchedulerCacheL1Test(t)
	bucket := service.SchedulerBucket{GroupID: 37, Platform: service.PlatformOpenAI, Mode: service.SchedulerModeSingle}
	publishSchedulerL1TestSnapshot(t, cache, bucket, []service.Account{{ID: 37_001, Platform: service.PlatformOpenAI}})
	_, hit, err := cache.GetSnapshotReadOnly(ctx, bucket)
	require.NoError(t, err)
	require.True(t, hit)
	concurrency := NewConcurrencyCache(cache.rdb, 15, 900).(*concurrencyCache)
	acquired, err := concurrency.AcquireAccountSlot(ctx, 37_001, 10, "warm")
	require.NoError(t, err)
	require.True(t, acquired)
	require.NoError(t, concurrency.ReleaseAccountSlot(ctx, 37_001, "warm"))
	commands.reset()

	_, hit, err = cache.GetSnapshotReadOnly(ctx, bucket)
	require.NoError(t, err)
	require.True(t, hit)
	acquired, err = concurrency.AcquireAccountSlot(ctx, 37_001, 10, "request")
	require.NoError(t, err)
	require.True(t, acquired)
	require.NoError(t, concurrency.ReleaseAccountSlot(ctx, 37_001, "request"))

	require.LessOrEqual(t, commands.total(), 3, "normal adaptive request must stay within the 0-3 Redis command budget")
}

func TestSchedulerCacheSnapshotL1ReloadsChangedVersion(t *testing.T) {
	ctx := context.Background()
	cache, _ := newSchedulerCacheL1Test(t)
	bucket := service.SchedulerBucket{GroupID: 32, Platform: service.PlatformOpenAI, Mode: service.SchedulerModeSingle}
	publishSchedulerL1TestSnapshot(t, cache, bucket, []service.Account{{ID: 3201, Name: "v1", Platform: service.PlatformOpenAI}})

	first, hit, err := cache.GetSnapshot(ctx, bucket)
	require.NoError(t, err)
	require.True(t, hit)
	require.Equal(t, "v1", first[0].Name)

	publishSchedulerL1TestSnapshot(t, cache, bucket, []service.Account{{ID: 3202, Name: "v2", Platform: service.PlatformOpenAI}})
	second, hit, err := cache.GetSnapshot(ctx, bucket)
	require.NoError(t, err)
	require.True(t, hit)
	require.Equal(t, int64(3202), second[0].ID)
	require.Equal(t, "v2", second[0].Name)
}

func TestSchedulerCacheSnapshotL1ReturnsIsolatedAccounts(t *testing.T) {
	ctx := context.Background()
	cache, _ := newSchedulerCacheL1Test(t)
	bucket := service.SchedulerBucket{GroupID: 33, Platform: service.PlatformOpenAI, Mode: service.SchedulerModeSingle}
	loadFactor := 10_000
	publishSchedulerL1TestSnapshot(t, cache, bucket, []service.Account{{
		ID:          3301,
		Name:        "original",
		Platform:    service.PlatformOpenAI,
		LoadFactor:  &loadFactor,
		Credentials: map[string]any{"model_mapping": map[string]any{"source": "target"}},
		Extra:       map[string]any{"codex_7d_used_percent": float64(100)},
		GroupIDs:    []int64{33},
	}})

	first, hit, err := cache.GetSnapshot(ctx, bucket)
	require.NoError(t, err)
	require.True(t, hit)
	first[0].Name = "mutated"
	first[0].Credentials["model_mapping"].(map[string]any)["source"] = "mutated"
	first[0].Extra["codex_7d_used_percent"] = float64(0)
	first[0].GroupIDs[0] = 999
	*first[0].LoadFactor = 1

	second, hit, err := cache.GetSnapshot(ctx, bucket)
	require.NoError(t, err)
	require.True(t, hit)
	require.Equal(t, "original", second[0].Name)
	require.Equal(t, "target", second[0].Credentials["model_mapping"].(map[string]any)["source"])
	require.Equal(t, float64(100), second[0].Extra["codex_7d_used_percent"])
	require.Equal(t, int64(33), second[0].GroupIDs[0])
	require.Equal(t, 10_000, *second[0].LoadFactor)
}

func TestSchedulerCacheSnapshotL1ReflectsSingleAccountUpdate(t *testing.T) {
	ctx := context.Background()
	cache, _ := newSchedulerCacheL1Test(t)
	bucket := service.SchedulerBucket{GroupID: 34, Platform: service.PlatformOpenAI, Mode: service.SchedulerModeSingle}
	publishSchedulerL1TestSnapshot(t, cache, bucket, []service.Account{{ID: 3401, Name: "before", Platform: service.PlatformOpenAI}})
	_, _, err := cache.GetSnapshot(ctx, bucket)
	require.NoError(t, err)

	require.NoError(t, cache.SetAccount(ctx, &service.Account{ID: 3401, Name: "after", Platform: service.PlatformOpenAI}))
	updated, hit, err := cache.GetSnapshot(ctx, bucket)
	require.NoError(t, err)
	require.True(t, hit)
	require.Equal(t, "after", updated[0].Name)
}

func TestSchedulerCacheSnapshotL1DoesNotSurviveRetire(t *testing.T) {
	ctx := context.Background()
	cache, _ := newSchedulerCacheL1Test(t)
	bucket := service.SchedulerBucket{GroupID: 35, Platform: service.PlatformOpenAI, Mode: service.SchedulerModeSingle}
	publishSchedulerL1TestSnapshot(t, cache, bucket, []service.Account{{ID: 3501, Platform: service.PlatformOpenAI}})
	_, hit, err := cache.GetSnapshot(ctx, bucket)
	require.NoError(t, err)
	require.True(t, hit)

	require.NoError(t, cache.RetireBucket(ctx, bucket))
	retired, hit, err := cache.GetSnapshot(ctx, bucket)
	require.NoError(t, err)
	require.False(t, hit)
	require.Nil(t, retired)
}
