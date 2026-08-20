package service

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/stretchr/testify/require"
)

type adaptiveBlockingSettingRepo struct {
	entered       chan struct{}
	release       chan struct{}
	once          sync.Once
	multipleCalls atomic.Int64
}

func (r *adaptiveBlockingSettingRepo) Get(context.Context, string) (*Setting, error) {
	return nil, ErrSettingNotFound
}

func (r *adaptiveBlockingSettingRepo) GetValue(context.Context, string) (string, error) {
	r.once.Do(func() { close(r.entered) })
	<-r.release
	return "", ErrSettingNotFound
}

func (r *adaptiveBlockingSettingRepo) Set(context.Context, string, string) error { return nil }
func (r *adaptiveBlockingSettingRepo) GetMultiple(context.Context, []string) (map[string]string, error) {
	r.multipleCalls.Add(1)
	return nil, nil
}
func (r *adaptiveBlockingSettingRepo) SetMultiple(context.Context, map[string]string) error {
	return nil
}
func (r *adaptiveBlockingSettingRepo) GetAll(context.Context) (map[string]string, error) {
	return nil, nil
}
func (r *adaptiveBlockingSettingRepo) Delete(context.Context, string) error { return nil }

func testOpenAIAdaptiveConfig() openAIAdaptiveSchedulerConfig {
	return openAIAdaptiveSchedulerConfig{
		initialWindow:       2,
		minWindow:           1,
		maxWindow:           32,
		increaseInterval:    2 * time.Second,
		no429IncreaseWindow: 30 * time.Second,
		maxWaiters:          1000,
	}
}

func adaptiveOpenAITestContext(groupID int64) context.Context {
	return context.WithValue(context.Background(), ctxkey.Group, &Group{
		ID:       groupID,
		Name:     "adaptive-test-group",
		Platform: PlatformOpenAI,
		Status:   StatusActive,
		Hydrated: true,
	})
}

func adaptiveOpenAITestSnapshot(accounts []Account) *SchedulerSnapshotService {
	snapshotAccounts := make([]*Account, 0, len(accounts))
	accountsByID := make(map[int64]*Account, len(accounts))
	for i := range accounts {
		account := &accounts[i]
		snapshotAccounts = append(snapshotAccounts, account)
		accountsByID[account.ID] = account
	}
	return &SchedulerSnapshotService{cache: &openAISnapshotCacheStub{
		snapshotAccounts: snapshotAccounts,
		accountsByID:     accountsByID,
	}}
}

func TestOpenAIAdaptiveSampleIndexesAreUniqueAndBounded(t *testing.T) {
	rng := newOpenAISelectionRNG(42)
	seen := make(map[int]struct{})

	first := openAIAdaptiveSampleIndexes(3000, 4, seen, &rng)
	second := openAIAdaptiveSampleIndexes(3000, 4, seen, &rng)

	require.Len(t, first, 4)
	require.Len(t, second, 4)
	require.Len(t, seen, 8)
	for _, index := range append(first, second...) {
		require.GreaterOrEqual(t, index, 0)
		require.Less(t, index, 3000)
	}
}

func TestOpenAIAdaptiveSelectUsesPreciseUtilization(t *testing.T) {
	now := time.Unix(7000, 0)
	cfg := testOpenAIAdaptiveConfig()
	cfg.initialWindow = 10_000
	cfg.maxWindow = 10_000
	cfg.sampleSize = 4
	cfg.sampleRounds = 2
	runtime := newOpenAIAdaptiveRuntime(cfg)
	accounts := []*Account{
		{ID: 201, Concurrency: 10_000},
		{ID: 202, Concurrency: 10_000},
	}
	firstLoad, ok := runtime.tryReserve(201, 10_000, "route", now)
	require.True(t, ok)
	secondLoadOne, ok := runtime.tryReserve(202, 10_000, "route", now)
	require.True(t, ok)
	secondLoadTwo, ok := runtime.tryReserve(202, 10_000, "route", now)
	require.True(t, ok)

	selected, permit := runtime.selectCandidate(accounts, nil, "route", now, 99)
	require.NotNil(t, selected)
	require.NotNil(t, permit)
	require.Equal(t, int64(201), selected.ID, "1/10000 must rank below 2/10000 instead of both rounding to zero")

	permit.Release()
	firstLoad.Release()
	secondLoadOne.Release()
	secondLoadTwo.Release()
}

func TestOpenAIAdaptiveSelectRandomTieCoversLargePool(t *testing.T) {
	now := time.Unix(8000, 0)
	cfg := testOpenAIAdaptiveConfig()
	cfg.sampleSize = 4
	cfg.sampleRounds = 2
	runtime := newOpenAIAdaptiveRuntime(cfg)
	accounts := make([]*Account, 0, 3000)
	for i := 0; i < 3000; i++ {
		accounts = append(accounts, &Account{ID: int64(10_000 + i), Concurrency: 10_000})
	}

	selectedIDs := make(map[int64]struct{}, 3000)
	for seed := uint64(1); seed <= 6000; seed++ {
		selected, permit := runtime.selectCandidate(accounts, nil, "route", now, seed)
		require.NotNil(t, selected)
		selectedIDs[selected.ID] = struct{}{}
		permit.Release()
	}

	require.Greater(t, len(selectedIDs), 2400, "tie-breaking must not concentrate on low account IDs")
}

func TestOpenAIAdaptiveSelectExcludesFailedAccounts(t *testing.T) {
	now := time.Unix(9000, 0)
	cfg := testOpenAIAdaptiveConfig()
	cfg.sampleSize = 4
	cfg.sampleRounds = 2
	runtime := newOpenAIAdaptiveRuntime(cfg)
	accounts := []*Account{{ID: 301, Concurrency: 10}, {ID: 302, Concurrency: 10}}

	selected, permit := runtime.selectCandidate(accounts, map[int64]struct{}{301: {}}, "route", now, 123)

	require.NotNil(t, selected)
	require.Equal(t, int64(302), selected.ID)
	permit.Release()
}

func TestOpenAIAdaptiveWaitWakesWhenPermitIsReleased(t *testing.T) {
	now := time.Now()
	cfg := testOpenAIAdaptiveConfig()
	cfg.initialWindow = 1
	cfg.maxWindow = 1
	runtime := newOpenAIAdaptiveRuntime(cfg)
	accounts := []*Account{{ID: 303, Concurrency: 1}}
	held, ok := runtime.tryReserve(303, 1, "route", now)
	require.True(t, ok)

	type waitResult struct {
		account *Account
		permit  *openAIAdaptivePermit
		err     error
	}
	resultCh := make(chan waitResult, 1)
	go func() {
		account, permit, err := runtime.waitAndSelect(context.Background(), time.Now().Add(time.Second), "route", accounts, nil, 91)
		resultCh <- waitResult{account: account, permit: permit, err: err}
	}()

	require.Eventually(t, func() bool { return runtime.waiters.Load() == 1 }, time.Second, time.Millisecond)
	releasedAt := time.Now()
	held.Release()
	result := <-resultCh
	require.NoError(t, result.err)
	require.NotNil(t, result.account)
	require.Equal(t, int64(303), result.account.ID)
	require.Less(t, time.Since(releasedAt), 100*time.Millisecond)
	result.permit.Release()
}

func TestOpenAIAdaptiveWaitSelectsReleasedAccountOutsideRandomSample(t *testing.T) {
	now := time.Now()
	cfg := testOpenAIAdaptiveConfig()
	cfg.initialWindow = 1
	cfg.maxWindow = 1
	runtime := newOpenAIAdaptiveRuntime(cfg)
	accounts := make([]*Account, 3000)
	held := make([]*openAIAdaptivePermit, len(accounts))
	for i := range accounts {
		accounts[i] = &Account{ID: int64(20_000 + i), Concurrency: 1}
		permit, ok := runtime.tryReserve(accounts[i].ID, 1, "route", now)
		require.True(t, ok)
		held[i] = permit
	}
	defer func() {
		for _, permit := range held {
			permit.Release()
		}
	}()

	type waitResult struct {
		account *Account
		permit  *openAIAdaptivePermit
		err     error
	}
	resultCh := make(chan waitResult, 1)
	go func() {
		account, permit, err := runtime.waitAndSelect(context.Background(), time.Now().Add(250*time.Millisecond), "route", accounts, nil, 91)
		resultCh <- waitResult{account: account, permit: permit, err: err}
	}()

	require.Eventually(t, func() bool { return runtime.waiters.Load() == 1 }, time.Second, time.Millisecond)
	released := accounts[len(accounts)-1]
	held[len(held)-1].Release()
	result := <-resultCh
	require.NoError(t, result.err)
	require.NotNil(t, result.account)
	require.Equal(t, released.ID, result.account.ID)
	result.permit.Release()
}

func TestOpenAIAdaptiveWaitWakesAcrossRoutesSharingAccountCapacity(t *testing.T) {
	now := time.Now()
	cfg := testOpenAIAdaptiveConfig()
	cfg.initialWindow = 1
	cfg.maxWindow = 1
	runtime := newOpenAIAdaptiveRuntime(cfg)
	accounts := []*Account{{ID: 307, Concurrency: 1}}
	held, ok := runtime.tryReserve(307, 1, "openai|gpt-5.1", now)
	require.True(t, ok)

	type waitResult struct {
		account *Account
		permit  *openAIAdaptivePermit
		err     error
	}
	resultCh := make(chan waitResult, 1)
	go func() {
		account, permit, err := runtime.waitAndSelect(
			context.Background(), time.Now().Add(time.Second), "openai|gpt-5.2", accounts, nil, 94,
		)
		resultCh <- waitResult{account: account, permit: permit, err: err}
	}()

	require.Eventually(t, func() bool { return runtime.waiters.Load() == 1 }, time.Second, time.Millisecond)
	held.Release()
	result := <-resultCh
	require.NoError(t, result.err)
	require.NotNil(t, result.account)
	require.Equal(t, int64(307), result.account.ID)
	result.permit.Release()
}

func TestOpenAIAdaptiveWaitWakesWhen429CooldownExpires(t *testing.T) {
	now := time.Now()
	runtime := newOpenAIAdaptiveRuntime(testOpenAIAdaptiveConfig())
	accounts := []*Account{{ID: 308, Concurrency: 1}}
	cooldownUntil := runtime.report429(308, 1, now, time.Time{})

	startedAt := time.Now()
	account, permit, err := runtime.waitAndSelect(
		context.Background(), cooldownUntil.Add(500*time.Millisecond), "openai|gpt-5.1", accounts, nil, 95,
	)

	require.NoError(t, err)
	require.NotNil(t, account)
	require.NotNil(t, permit)
	require.False(t, time.Now().Before(cooldownUntil))
	require.Less(t, time.Since(startedAt), 2500*time.Millisecond)
	permit.Release()
}

func TestOpenAIAdaptiveRouteKeyIncludesSchedulingPool(t *testing.T) {
	groupA := int64(1)
	groupB := int64(2)
	base := OpenAIAccountScheduleRequest{GroupID: &groupA, Platform: PlatformOpenAI, RequestedModel: "gpt-5.1"}
	otherGroup := base
	otherGroup.GroupID = &groupB
	otherCapability := base
	otherCapability.RequiredCapability = OpenAIEndpointCapabilityResponses

	require.NotEqual(t, openAIAdaptiveRouteKey(base), openAIAdaptiveRouteKey(otherGroup))
	require.NotEqual(t, openAIAdaptiveRouteKey(base), openAIAdaptiveRouteKey(otherCapability))
	require.Equal(t, openAIAdaptiveResultRouteKey(base.Platform, base.RequestedModel), openAIAdaptiveStormRouteKey(openAIAdaptiveRouteKey(base)))
}

func TestOpenAIAdaptiveReleasePreservesOneWakePerFreedPermit(t *testing.T) {
	now := time.Now()
	cfg := testOpenAIAdaptiveConfig()
	cfg.initialWindow = 2
	cfg.maxWindow = 2
	runtime := newOpenAIAdaptiveRuntime(cfg)
	first, ok := runtime.tryReserve(306, 2, "route", now)
	require.True(t, ok)
	second, ok := runtime.tryReserve(306, 2, "route", now)
	require.True(t, ok)
	runtime.waiters.Store(2)
	runtime.routeShard("route").waiters.Store(2)

	first.Release()
	second.Release()

	require.Len(t, runtime.capacityNotifications("route"), 2, "simultaneous releases must not collapse into one waiter wakeup")
	runtime.waiters.Store(0)
	runtime.routeShard("route").waiters.Store(0)
}

func TestOpenAIAdaptiveWaitStopsAtAbsoluteDeadline(t *testing.T) {
	now := time.Now()
	cfg := testOpenAIAdaptiveConfig()
	cfg.initialWindow = 1
	cfg.maxWindow = 1
	runtime := newOpenAIAdaptiveRuntime(cfg)
	accounts := []*Account{{ID: 304, Concurrency: 1}}
	held, ok := runtime.tryReserve(304, 1, "route", now)
	require.True(t, ok)
	defer held.Release()

	deadline := time.Now().Add(80 * time.Millisecond)
	account, permit, err := runtime.waitAndSelect(context.Background(), deadline, "route", accounts, nil, 92)

	require.ErrorIs(t, err, errOpenAIAdaptiveWaitTimeout)
	require.Nil(t, account)
	require.Nil(t, permit)
	require.False(t, time.Now().Before(deadline))
	require.Zero(t, runtime.waiters.Load())
}

func TestOpenAIAdaptiveWaitRespectsContextCancellation(t *testing.T) {
	now := time.Now()
	cfg := testOpenAIAdaptiveConfig()
	cfg.initialWindow = 1
	cfg.maxWindow = 1
	runtime := newOpenAIAdaptiveRuntime(cfg)
	accounts := []*Account{{ID: 305, Concurrency: 1}}
	held, ok := runtime.tryReserve(305, 1, "route", now)
	require.True(t, ok)
	defer held.Release()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	account, permit, err := runtime.waitAndSelect(ctx, time.Now().Add(time.Second), "route", accounts, nil, 93)

	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, account)
	require.Nil(t, permit)
	require.Zero(t, runtime.waiters.Load())
}

func TestOpenAIAdaptiveWaitRejectsThe1001stWaiter(t *testing.T) {
	runtime := newOpenAIAdaptiveRuntime(testOpenAIAdaptiveConfig())
	for i := 0; i < 1000; i++ {
		require.True(t, runtime.enterWaiter("route"))
	}

	require.False(t, runtime.enterWaiter("route"))
	require.Equal(t, int64(1000), runtime.waiters.Load())
	for i := 0; i < 1000; i++ {
		runtime.leaveWaiter("route")
	}
	require.Zero(t, runtime.waiters.Load())
}

type adaptiveCountingConcurrencyCache struct {
	schedulerTestConcurrencyCache
	loadCalls    *atomic.Int64
	acquireCalls *atomic.Int64
	releaseCalls *atomic.Int64
}

type adaptiveRejectFirstConcurrencyCache struct {
	schedulerTestConcurrencyCache
	mu          sync.Mutex
	acquiredIDs []int64
}

type adaptiveHotPathAccountRepo struct {
	schedulerTestOpenAIAccountRepo
	getByIDCalls              atomic.Int64
	schedulableCalls          atomic.Int64
	setErrorCalls             atomic.Int64
	setTempUnschedulableCalls atomic.Int64
}

func (r *adaptiveHotPathAccountRepo) GetByID(ctx context.Context, id int64) (*Account, error) {
	r.getByIDCalls.Add(1)
	return r.schedulerTestOpenAIAccountRepo.GetByID(ctx, id)
}

func (r *adaptiveHotPathAccountRepo) ListSchedulableByPlatform(ctx context.Context, platform string) ([]Account, error) {
	r.schedulableCalls.Add(1)
	return r.schedulerTestOpenAIAccountRepo.ListSchedulableByPlatform(ctx, platform)
}

func (r *adaptiveHotPathAccountRepo) SetError(context.Context, int64, string) error {
	r.setErrorCalls.Add(1)
	return nil
}

func (r *adaptiveHotPathAccountRepo) SetTempUnschedulable(context.Context, int64, time.Time, string) error {
	r.setTempUnschedulableCalls.Add(1)
	return nil
}

type adaptiveHotPathGroupRepo struct {
	GroupRepository
	group *Group
	calls atomic.Int64
}

func (r *adaptiveHotPathGroupRepo) GetByID(context.Context, int64) (*Group, error) {
	r.calls.Add(1)
	return r.group, nil
}

func (r *adaptiveHotPathGroupRepo) GetByIDLite(context.Context, int64) (*Group, error) {
	r.calls.Add(1)
	return r.group, nil
}

func (c *adaptiveRejectFirstConcurrencyCache) AcquireAccountSlot(_ context.Context, accountID int64, _ int, _ string) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.acquiredIDs = append(c.acquiredIDs, accountID)
	return len(c.acquiredIDs) > 1, nil
}

func (c *adaptiveRejectFirstConcurrencyCache) ReleaseAccountSlot(context.Context, int64, string) error {
	return nil
}

func (c *adaptiveRejectFirstConcurrencyCache) attempts() []int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]int64(nil), c.acquiredIDs...)
}

func (c adaptiveCountingConcurrencyCache) GetAccountsLoadBatch(ctx context.Context, accounts []AccountWithConcurrency) (map[int64]*AccountLoadInfo, error) {
	c.loadCalls.Add(1)
	return c.schedulerTestConcurrencyCache.GetAccountsLoadBatch(ctx, accounts)
}

func (c adaptiveCountingConcurrencyCache) AcquireAccountSlot(ctx context.Context, accountID int64, maxConcurrency int, requestID string) (bool, error) {
	c.acquireCalls.Add(1)
	return c.schedulerTestConcurrencyCache.AcquireAccountSlot(ctx, accountID, maxConcurrency, requestID)
}

func (c adaptiveCountingConcurrencyCache) ReleaseAccountSlot(ctx context.Context, accountID int64, requestID string) error {
	c.releaseCalls.Add(1)
	return c.schedulerTestConcurrencyCache.ReleaseAccountSlot(ctx, accountID, requestID)
}

func TestOpenAIGatewayService_AdaptiveSchedulerAvoidsBroadRedisLoadReads(t *testing.T) {
	groupID := int64(41)
	ctx := adaptiveOpenAITestContext(groupID)
	accounts := make([]Account, 0, 3000)
	for i := 0; i < 3000; i++ {
		accounts = append(accounts, Account{
			ID:          int64(40_000 + i),
			Name:        fmt.Sprintf("account-%d", i),
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 10_000,
		})
	}
	cfg := &config.Config{}
	cfg.RunMode = config.RunModeSimple
	cfg.Gateway.OpenAIScheduler.AdaptiveEnabled = true
	cfg.Gateway.OpenAIScheduler.ShadowMode = false
	cfg.Gateway.OpenAIScheduler.InitialWindow = 2
	cfg.Gateway.OpenAIScheduler.MinWindow = 1
	cfg.Gateway.OpenAIScheduler.MaxWindow = 32
	cfg.Gateway.OpenAIScheduler.SampleSize = 4
	cfg.Gateway.OpenAIScheduler.SampleRounds = 2
	cfg.Gateway.OpenAIScheduler.SchedulingWaitTimeoutMS = 1000
	cfg.Gateway.OpenAIScheduler.MaxWaiters = 1000
	var loadCalls, acquireCalls, releaseCalls atomic.Int64
	var snapshotCalls, accountMetadataCalls atomic.Int64
	cache := adaptiveCountingConcurrencyCache{
		schedulerTestConcurrencyCache: schedulerTestConcurrencyCache{},
		loadCalls:                     &loadCalls,
		acquireCalls:                  &acquireCalls,
		releaseCalls:                  &releaseCalls,
	}
	snapshotAccounts := make([]*Account, 0, len(accounts))
	accountsByID := make(map[int64]*Account, len(accounts))
	for i := range accounts {
		account := &accounts[i]
		snapshotAccounts = append(snapshotAccounts, account)
		accountsByID[account.ID] = account
	}
	repo := &adaptiveHotPathAccountRepo{
		schedulerTestOpenAIAccountRepo: schedulerTestOpenAIAccountRepo{accounts: accounts},
	}
	snapshotCache := &openAISnapshotCacheStub{
		snapshotAccounts: snapshotAccounts,
		accountsByID:     accountsByID,
		snapshotCalls:    &snapshotCalls,
		accountCalls:     &accountMetadataCalls,
	}
	svc := &OpenAIGatewayService{
		accountRepo:        repo,
		cache:              &schedulerTestGatewayCache{},
		cfg:                cfg,
		concurrencyService: NewConcurrencyService(cache),
		schedulerSnapshot:  &SchedulerSnapshotService{cache: snapshotCache, accountRepo: repo},
	}

	selection, decision, err := svc.SelectAccountWithScheduler(ctx, &groupID, "", "", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, 3000, decision.CandidateCount)
	require.Zero(t, loadCalls.Load(), "adaptive hot path must not read every account load from Redis")
	require.Equal(t, int64(1), acquireCalls.Load(), "only the locally selected account should acquire a Redis slot")
	require.Equal(t, int64(1), snapshotCalls.Load(), "adaptive selection should read one versioned pool snapshot")
	require.Zero(t, accountMetadataCalls.Load(), "adaptive hot path must use the account already present in the pool snapshot")
	require.Zero(t, repo.getByIDCalls.Load(), "adaptive hot path must not recheck the selected account in PostgreSQL")

	selection.ReleaseFunc()
	require.Equal(t, int64(1), releaseCalls.Load())
}

func TestOpenAIGatewayService_AdaptiveSchedulerDoesNotReadAdvancedSettingsFromPostgres(t *testing.T) {
	groupID := int64(42)
	account := Account{
		ID: 49_001, Name: "cache-only-settings", Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Status: StatusActive, Schedulable: true, Concurrency: 10,
	}
	cfg := &config.Config{RunMode: config.RunModeSimple}
	cfg.Gateway.OpenAIScheduler.AdaptiveEnabled = true
	cfg.Gateway.OpenAIScheduler.ShadowMode = false
	settingRepo := &adaptiveBlockingSettingRepo{entered: make(chan struct{}), release: make(chan struct{})}
	previous := openAIAdvancedSchedulerSettingCache.Load()
	openAIAdvancedSchedulerSettingCache.Store(&cachedOpenAIAdvancedSchedulerSetting{expiresAt: time.Now().Add(-time.Hour).UnixNano()})
	t.Cleanup(func() {
		if previous != nil {
			openAIAdvancedSchedulerSettingCache.Store(previous)
		}
	})
	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: []Account{account}},
		cache:              &schedulerTestGatewayCache{},
		cfg:                cfg,
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
		rateLimitService: &RateLimitService{
			settingService: NewSettingService(settingRepo, cfg),
		},
		schedulerSnapshot: adaptiveOpenAITestSnapshot([]Account{account}),
	}

	selection, _, err := svc.SelectAccountWithScheduler(
		adaptiveOpenAITestContext(groupID), &groupID, "", "", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false,
	)

	require.NoError(t, err)
	require.NotNil(t, selection)
	require.Zero(t, settingRepo.multipleCalls.Load(), "adaptive request scheduling must use only already-cached/default advanced settings")
	selection.ReleaseFunc()
}

func TestOpenAIGatewayService_AdaptiveSchedulerDoesNotFallBackToPostgresOnSnapshotMiss(t *testing.T) {
	groupID := int64(49)
	account := Account{ID: 50_601, Name: "db-only", Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 10}
	cfg := &config.Config{RunMode: config.RunModeSimple}
	cfg.Gateway.OpenAIScheduler.AdaptiveEnabled = true
	cfg.Gateway.OpenAIScheduler.ShadowMode = false
	repo := &adaptiveHotPathAccountRepo{
		schedulerTestOpenAIAccountRepo: schedulerTestOpenAIAccountRepo{accounts: []Account{account}},
	}
	svc := &OpenAIGatewayService{
		accountRepo:       repo,
		cache:             &schedulerTestGatewayCache{},
		cfg:               cfg,
		schedulerSnapshot: &SchedulerSnapshotService{cache: &openAISnapshotCacheStub{}, accountRepo: repo},
	}

	selection, _, err := svc.SelectAccountWithScheduler(
		context.Background(), &groupID, "", "", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false,
	)

	require.Error(t, err)
	require.Nil(t, selection)
	require.Zero(t, repo.schedulableCalls.Load(), "adaptive snapshot miss must not query the PostgreSQL account pool")
	require.Zero(t, repo.getByIDCalls.Load())
}

func TestOpenAIGatewayService_AdaptiveSchedulerRequiresSnapshotService(t *testing.T) {
	groupID := int64(53)
	account := Account{ID: 51_001, Name: "db-only", Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 10}
	cfg := &config.Config{RunMode: config.RunModeSimple}
	cfg.Gateway.OpenAIScheduler.AdaptiveEnabled = true
	cfg.Gateway.OpenAIScheduler.ShadowMode = false
	repo := &adaptiveHotPathAccountRepo{
		schedulerTestOpenAIAccountRepo: schedulerTestOpenAIAccountRepo{accounts: []Account{account}},
	}
	svc := &OpenAIGatewayService{
		accountRepo: repo,
		cache:       &schedulerTestGatewayCache{},
		cfg:         cfg,
	}

	selection, _, err := svc.SelectAccountWithScheduler(
		adaptiveOpenAITestContext(groupID), &groupID, "", "", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false,
	)

	require.ErrorIs(t, err, ErrSchedulerCacheNotReady)
	require.Nil(t, selection)
	require.Zero(t, repo.schedulableCalls.Load(), "adaptive scheduling must not use PostgreSQL when the snapshot service is unavailable")
	require.Zero(t, repo.getByIDCalls.Load())
}

func TestOpenAIGatewayService_AdaptiveStickyRequiresSnapshotService(t *testing.T) {
	groupID := int64(56)
	account := Account{ID: 51_401, Name: "db-sticky", Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 10}
	cfg := &config.Config{RunMode: config.RunModeSimple}
	cfg.Gateway.OpenAIScheduler.AdaptiveEnabled = true
	cfg.Gateway.OpenAIScheduler.ShadowMode = false
	repo := &adaptiveHotPathAccountRepo{
		schedulerTestOpenAIAccountRepo: schedulerTestOpenAIAccountRepo{accounts: []Account{account}},
	}
	svc := &OpenAIGatewayService{
		accountRepo: repo,
		cache: &schedulerTestGatewayCache{sessionBindings: map[string]int64{
			"openai:adaptive-no-snapshot": account.ID,
		}},
		cfg: cfg,
	}

	selection, _, err := svc.SelectAccountWithScheduler(
		adaptiveOpenAITestContext(groupID), &groupID, "", "adaptive-no-snapshot", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false,
	)

	require.ErrorIs(t, err, ErrSchedulerCacheNotReady)
	require.Nil(t, selection)
	require.Zero(t, repo.getByIDCalls.Load(), "adaptive sticky lookup must not use PostgreSQL when the snapshot service is unavailable")
	require.Zero(t, repo.schedulableCalls.Load())
}

func TestOpenAIGatewayService_AdaptiveShadowParentCacheMissDoesNotFallBackToPostgres(t *testing.T) {
	parentID := int64(51_101)
	parent := Account{ID: parentID, Name: "parent", Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 10}
	shadow := Account{
		ID: 51_102, Name: "shadow", Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Status: StatusActive, Schedulable: true, Concurrency: 10,
		ParentAccountID: &parentID, QuotaDimension: QuotaDimensionSpark,
	}
	cfg := &config.Config{RunMode: config.RunModeSimple}
	cfg.Gateway.OpenAIScheduler.AdaptiveEnabled = true
	cfg.Gateway.OpenAIScheduler.ShadowMode = false
	var accountMetadataCalls atomic.Int64
	repo := &adaptiveHotPathAccountRepo{
		schedulerTestOpenAIAccountRepo: schedulerTestOpenAIAccountRepo{accounts: []Account{parent, shadow}},
	}
	svc := &OpenAIGatewayService{
		accountRepo: repo,
		cache:       &schedulerTestGatewayCache{},
		cfg:         cfg,
		schedulerSnapshot: &SchedulerSnapshotService{cache: &openAISnapshotCacheStub{
			accountCalls: &accountMetadataCalls,
		}, accountRepo: repo},
	}
	scheduler := newDefaultOpenAIAccountScheduler(svc, nil).(*defaultOpenAIAccountScheduler)

	compatible, reason := scheduler.isAccountRequestCompatibleReason(context.Background(), &shadow, OpenAIAccountScheduleRequest{
		Platform: PlatformOpenAI,
	})

	require.False(t, compatible)
	require.Equal(t, "shadow_parent_unhealthy", reason)
	require.Equal(t, int64(1), accountMetadataCalls.Load(), "adaptive shadow parent lookup may read only cached account metadata")
	require.Zero(t, repo.getByIDCalls.Load(), "adaptive shadow parent cache miss must fail closed without PostgreSQL")
}

func TestOpenAIGatewayService_AdaptiveShadowParentsReusePoolSnapshot(t *testing.T) {
	groupID := int64(57)
	accounts := make([]Account, 0, 200)
	for i := 0; i < 100; i++ {
		parentID := int64(52_000 + i)
		accounts = append(accounts,
			Account{ID: parentID, Name: fmt.Sprintf("parent-%d", i), Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 10},
			Account{
				ID: 53_000 + int64(i), Name: fmt.Sprintf("shadow-%d", i), Platform: PlatformOpenAI, Type: AccountTypeOAuth,
				Status: StatusActive, Schedulable: true, Concurrency: 10,
				ParentAccountID: &parentID, QuotaDimension: QuotaDimensionSpark,
			},
		)
	}
	cfg := &config.Config{RunMode: config.RunModeSimple}
	cfg.Gateway.OpenAIScheduler.AdaptiveEnabled = true
	cfg.Gateway.OpenAIScheduler.ShadowMode = false
	var accountMetadataCalls atomic.Int64
	snapshotAccounts := make([]*Account, 0, len(accounts))
	accountsByID := make(map[int64]*Account, len(accounts))
	for i := range accounts {
		account := &accounts[i]
		snapshotAccounts = append(snapshotAccounts, account)
		accountsByID[account.ID] = account
	}
	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: accounts},
		cache:              &schedulerTestGatewayCache{},
		cfg:                cfg,
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
		schedulerSnapshot: &SchedulerSnapshotService{cache: &openAISnapshotCacheStub{
			snapshotAccounts: snapshotAccounts,
			accountsByID:     accountsByID,
			accountCalls:     &accountMetadataCalls,
		}},
	}

	selection, _, err := svc.SelectAccountWithScheduler(
		adaptiveOpenAITestContext(groupID), &groupID, "", "", "", nil, OpenAIUpstreamTransportAny, false,
	)

	require.NoError(t, err)
	require.NotNil(t, selection)
	require.Zero(t, accountMetadataCalls.Load(), "shadow parent health checks must reuse the versioned pool snapshot instead of issuing Redis MGETs")
	selection.ReleaseFunc()
}

func TestOpenAIGatewayService_AdaptiveShadowParentsMemoizeCacheLookup(t *testing.T) {
	groupID := int64(58)
	parentID := int64(54_000)
	parent := Account{ID: parentID, Name: "parent", Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 10}
	accounts := make([]Account, 100)
	for i := range accounts {
		accounts[i] = Account{
			ID: 55_000 + int64(i), Name: fmt.Sprintf("shadow-%d", i), Platform: PlatformOpenAI, Type: AccountTypeOAuth,
			Status: StatusActive, Schedulable: true, Concurrency: 10,
			ParentAccountID: &parentID, QuotaDimension: QuotaDimensionSpark,
		}
	}
	cfg := &config.Config{RunMode: config.RunModeSimple}
	cfg.Gateway.OpenAIScheduler.AdaptiveEnabled = true
	cfg.Gateway.OpenAIScheduler.ShadowMode = false
	var accountMetadataCalls atomic.Int64
	snapshotAccounts := make([]*Account, 0, len(accounts))
	for i := range accounts {
		snapshotAccounts = append(snapshotAccounts, &accounts[i])
	}
	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: accounts},
		cache:              &schedulerTestGatewayCache{},
		cfg:                cfg,
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
		schedulerSnapshot: &SchedulerSnapshotService{cache: &openAISnapshotCacheStub{
			snapshotAccounts: snapshotAccounts,
			accountsByID:     map[int64]*Account{parentID: &parent},
			accountCalls:     &accountMetadataCalls,
		}},
	}

	selection, _, err := svc.SelectAccountWithScheduler(
		adaptiveOpenAITestContext(groupID), &groupID, "", "", "", nil, OpenAIUpstreamTransportAny, false,
	)

	require.NoError(t, err)
	require.NotNil(t, selection)
	require.Equal(t, int64(1), accountMetadataCalls.Load(), "shared shadow parent metadata must be read only once per scheduling request")
	selection.ReleaseFunc()
}

func TestOpenAIGatewayService_AdaptiveGetAccessTokenUsesCachedShadowParent(t *testing.T) {
	parentID := int64(56_000)
	parent := Account{
		ID: parentID, Name: "parent", Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Status: StatusActive, Schedulable: true,
		Credentials: map[string]any{"access_token": "cached-parent-token"},
	}
	shadow := Account{
		ID: 56_001, Name: "shadow", Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Status: StatusActive, Schedulable: true,
		ParentAccountID: &parentID, QuotaDimension: QuotaDimensionSpark,
	}
	cfg := &config.Config{RunMode: config.RunModeSimple}
	cfg.Gateway.OpenAIScheduler.AdaptiveEnabled = true
	cfg.Gateway.OpenAIScheduler.ShadowMode = false
	var accountMetadataCalls atomic.Int64
	repo := &adaptiveHotPathAccountRepo{
		schedulerTestOpenAIAccountRepo: schedulerTestOpenAIAccountRepo{accounts: []Account{parent}},
	}
	svc := &OpenAIGatewayService{
		accountRepo: repo,
		cfg:         cfg,
		schedulerSnapshot: &SchedulerSnapshotService{cache: &openAISnapshotCacheStub{
			accountsByID: map[int64]*Account{parentID: &parent},
			accountCalls: &accountMetadataCalls,
		}, accountRepo: repo},
	}

	token, tokenType, err := svc.GetAccessToken(context.Background(), &shadow)

	require.NoError(t, err)
	require.Equal(t, "cached-parent-token", token)
	require.Equal(t, "oauth", tokenType)
	require.Equal(t, int64(1), accountMetadataCalls.Load())
	require.Zero(t, repo.getByIDCalls.Load(), "adaptive credential resolution must not query PostgreSQL")
}

func TestOpenAIGatewayService_AdaptiveGetAccessTokenShadowParentCacheMissDoesNotFallBackToPostgres(t *testing.T) {
	parentID := int64(56_100)
	parent := Account{
		ID: parentID, Name: "parent", Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Status: StatusActive, Schedulable: true,
		Credentials: map[string]any{"access_token": "database-parent-token"},
	}
	shadow := Account{
		ID: 56_101, Name: "shadow", Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Status: StatusActive, Schedulable: true,
		ParentAccountID: &parentID, QuotaDimension: QuotaDimensionSpark,
	}
	cfg := &config.Config{RunMode: config.RunModeSimple}
	cfg.Gateway.OpenAIScheduler.AdaptiveEnabled = true
	cfg.Gateway.OpenAIScheduler.ShadowMode = false
	var accountMetadataCalls atomic.Int64
	repo := &adaptiveHotPathAccountRepo{
		schedulerTestOpenAIAccountRepo: schedulerTestOpenAIAccountRepo{accounts: []Account{parent}},
	}
	svc := &OpenAIGatewayService{
		accountRepo: repo,
		cfg:         cfg,
		schedulerSnapshot: &SchedulerSnapshotService{cache: &openAISnapshotCacheStub{
			accountCalls: &accountMetadataCalls,
		}, accountRepo: repo},
	}

	token, tokenType, err := svc.GetAccessToken(context.Background(), &shadow)

	require.ErrorIs(t, err, ErrSchedulerCacheNotReady)
	require.Empty(t, token)
	require.Empty(t, tokenType)
	require.Equal(t, int64(1), accountMetadataCalls.Load())
	require.Zero(t, repo.getByIDCalls.Load(), "adaptive credential cache miss must fail closed without PostgreSQL")
}

func TestOpenAIGatewayService_AdaptiveCodexModelsUsesCachedShadowParent(t *testing.T) {
	parentID := int64(56_200)
	parent := Account{
		ID: parentID, Name: "parent", Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Status: StatusActive, Schedulable: true,
	}
	shadow := Account{
		ID: 56_201, Name: "shadow", Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Status: StatusActive, Schedulable: true,
		ParentAccountID: &parentID, QuotaDimension: QuotaDimensionSpark,
	}
	cfg := &config.Config{RunMode: config.RunModeSimple}
	cfg.Gateway.OpenAIScheduler.AdaptiveEnabled = true
	cfg.Gateway.OpenAIScheduler.ShadowMode = false
	var accountMetadataCalls atomic.Int64
	repo := &adaptiveHotPathAccountRepo{
		schedulerTestOpenAIAccountRepo: schedulerTestOpenAIAccountRepo{accounts: []Account{parent}},
	}
	svc := &OpenAIGatewayService{
		accountRepo: repo,
		cfg:         cfg,
		schedulerSnapshot: &SchedulerSnapshotService{cache: &openAISnapshotCacheStub{
			accountsByID: map[int64]*Account{parentID: &parent},
			accountCalls: &accountMetadataCalls,
		}, accountRepo: repo},
	}

	manifest, err := svc.FetchCodexModelsManifest(context.Background(), &shadow, "0.144.0", "")

	require.Error(t, err, "the cached parent intentionally has no token")
	require.Nil(t, manifest)
	require.Equal(t, int64(1), accountMetadataCalls.Load())
	require.Zero(t, repo.getByIDCalls.Load(), "adaptive Codex credential resolution must not query PostgreSQL")
}

func TestOpenAIGatewayService_AdaptivePreviousResponseShadowParentCacheMissDoesNotFallBackToPostgres(t *testing.T) {
	groupID := int64(54)
	parentID := int64(51_201)
	parent := Account{ID: parentID, Name: "parent", Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 10}
	shadow := Account{
		ID: 51_202, Name: "shadow", Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Status: StatusActive, Schedulable: true, Concurrency: 10,
		ParentAccountID: &parentID, QuotaDimension: QuotaDimensionSpark,
		Extra: map[string]any{"openai_oauth_responses_websockets_v2_enabled": true},
	}
	cfg := newOpenAIWSV2TestConfig()
	cfg.RunMode = config.RunModeSimple
	cfg.Gateway.OpenAIScheduler.AdaptiveEnabled = true
	cfg.Gateway.OpenAIScheduler.ShadowMode = false
	gatewayCache := &schedulerTestGatewayCache{}
	store := NewOpenAIWSStateStore(gatewayCache)
	var accountMetadataCalls atomic.Int64
	repo := &adaptiveHotPathAccountRepo{
		schedulerTestOpenAIAccountRepo: schedulerTestOpenAIAccountRepo{accounts: []Account{parent, shadow}},
	}
	svc := &OpenAIGatewayService{
		accountRepo:        repo,
		cache:              gatewayCache,
		cfg:                cfg,
		openaiWSStateStore: store,
		schedulerSnapshot: &SchedulerSnapshotService{cache: &openAISnapshotCacheStub{
			accountsByID: map[int64]*Account{shadow.ID: &shadow},
			accountCalls: &accountMetadataCalls,
		}, accountRepo: repo},
	}
	ctx := adaptiveOpenAITestContext(groupID)
	require.NoError(t, store.BindResponseAccount(ctx, groupID, "resp_shadow_cache_miss", shadow.ID, time.Hour))

	accountID, account, _, _ := svc.resolveAccountByPreviousResponseIDForCapability(
		ctx, &groupID, "resp_shadow_cache_miss", "gpt-5.3-codex-spark", nil, "", false,
	)

	require.Zero(t, accountID)
	require.Nil(t, account)
	require.Equal(t, int64(2), accountMetadataCalls.Load(), "adaptive previous-response lookup may read the shadow and parent caches only")
	require.Zero(t, repo.getByIDCalls.Load(), "adaptive previous-response parent cache miss must fail closed without PostgreSQL")
}

func TestOpenAIGatewayService_AdaptiveSchedulingThresholdDoesNotPersistPerCandidate(t *testing.T) {
	groupID := int64(55)
	now := time.Now().UTC()
	accounts := []Account{
		{
			ID: 51_301, Name: "threshold-blocked", Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
			Status: StatusActive, Schedulable: true, Concurrency: 10,
			Credentials: map[string]any{"account_scheduling_threshold": 80},
			Extra: map[string]any{
				"codex_usage_updated_at": now.Add(-time.Minute).Format(time.RFC3339),
				"codex_5h_used_percent":  95.0,
				"codex_5h_reset_at":      now.Add(time.Hour).Format(time.RFC3339),
			},
		},
		{ID: 51_302, Name: "healthy", Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 10},
	}
	cfg := &config.Config{RunMode: config.RunModeSimple}
	cfg.Gateway.OpenAIScheduler.AdaptiveEnabled = true
	cfg.Gateway.OpenAIScheduler.ShadowMode = false
	repo := &adaptiveHotPathAccountRepo{
		schedulerTestOpenAIAccountRepo: schedulerTestOpenAIAccountRepo{accounts: accounts},
	}
	svc := &OpenAIGatewayService{
		accountRepo:        repo,
		cache:              &schedulerTestGatewayCache{},
		cfg:                cfg,
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
		rateLimitService: &RateLimitService{
			accountRepo:    repo,
			settingService: &SettingService{},
		},
		schedulerSnapshot: &SchedulerSnapshotService{cache: &openAISnapshotCacheStub{
			snapshotAccounts: []*Account{&accounts[0], &accounts[1]},
		}, accountRepo: repo},
	}

	selection, _, err := svc.SelectAccountWithScheduler(
		adaptiveOpenAITestContext(groupID), &groupID, "", "", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false,
	)

	require.NoError(t, err)
	require.NotNil(t, selection)
	require.Equal(t, accounts[1].ID, selection.Account.ID)
	require.Zero(t, repo.setTempUnschedulableCalls.Load(), "adaptive candidate filtering must not persist threshold pauses per request")
	selection.ReleaseFunc()
}

func TestOpenAIGatewayService_AdaptiveSchedulerReusesRequestGroup(t *testing.T) {
	groupID := int64(50)
	account := Account{ID: 50_701, Name: "context-group", Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 10}
	group := &Group{ID: groupID, Name: "request-group", Platform: PlatformOpenAI, Status: StatusActive, Hydrated: true}
	cfg := &config.Config{RunMode: config.RunModeSimple}
	cfg.Gateway.OpenAIScheduler.AdaptiveEnabled = true
	cfg.Gateway.OpenAIScheduler.ShadowMode = false
	groupRepo := &adaptiveHotPathGroupRepo{group: group}
	snapshotCache := &openAISnapshotCacheStub{snapshotAccounts: []*Account{&account}}
	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: []Account{account}},
		cache:              &schedulerTestGatewayCache{},
		cfg:                cfg,
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
		schedulerSnapshot:  &SchedulerSnapshotService{cache: snapshotCache, groupRepo: groupRepo},
	}
	ctx := context.WithValue(context.Background(), ctxkey.Group, group)

	selection, _, err := svc.SelectAccountWithScheduler(
		ctx, &groupID, "", "", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false,
	)

	require.NoError(t, err)
	require.NotNil(t, selection)
	require.Zero(t, groupRepo.calls.Load(), "the authenticated request group already contains privacy scheduling policy")
	selection.ReleaseFunc()
}

func TestOpenAIGatewayService_AdaptivePrivacyFilterDoesNotWritePostgres(t *testing.T) {
	groupID := int64(52)
	accounts := []Account{
		{ID: 50_901, Name: "privacy-missing", Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 10},
		{
			ID: 50_902, Name: "privacy-ready", Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
			Status: StatusActive, Schedulable: true, Concurrency: 10,
			Extra: map[string]any{"privacy_mode": PrivacyModeTrainingOff},
		},
	}
	group := &Group{
		ID: groupID, Name: "privacy-group", Platform: PlatformOpenAI, Status: StatusActive,
		Hydrated: true, RequirePrivacySet: true,
	}
	cfg := &config.Config{RunMode: config.RunModeSimple}
	cfg.Gateway.OpenAIScheduler.AdaptiveEnabled = true
	cfg.Gateway.OpenAIScheduler.ShadowMode = false
	repo := &adaptiveHotPathAccountRepo{
		schedulerTestOpenAIAccountRepo: schedulerTestOpenAIAccountRepo{accounts: accounts},
	}
	snapshotCache := &openAISnapshotCacheStub{snapshotAccounts: []*Account{&accounts[0], &accounts[1]}}
	svc := &OpenAIGatewayService{
		accountRepo:        repo,
		cache:              &schedulerTestGatewayCache{},
		cfg:                cfg,
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
		schedulerSnapshot:  &SchedulerSnapshotService{cache: snapshotCache, accountRepo: repo},
	}
	ctx := context.WithValue(context.Background(), ctxkey.Group, group)

	selection, _, err := svc.SelectAccountWithScheduler(
		ctx, &groupID, "", "", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false,
	)

	require.NoError(t, err)
	require.Equal(t, accounts[1].ID, selection.Account.ID)
	require.Zero(t, repo.setErrorCalls.Load(), "adaptive candidate filtering must not persist one error per rejected account")
	selection.ReleaseFunc()
}

func TestOpenAIGatewayService_AdaptiveStickyEscapesAtUtilizationThreshold(t *testing.T) {
	groupID := int64(47)
	accounts := []Account{
		{ID: 50_401, Name: "sticky", Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 10},
		{ID: 50_402, Name: "backup", Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 10},
	}
	cfg := &config.Config{}
	cfg.RunMode = config.RunModeSimple
	cfg.Gateway.OpenAIScheduler.AdaptiveEnabled = true
	cfg.Gateway.OpenAIScheduler.ShadowMode = false
	cfg.Gateway.OpenAIScheduler.InitialWindow = 1
	cfg.Gateway.OpenAIScheduler.MinWindow = 1
	cfg.Gateway.OpenAIScheduler.MaxWindow = 1
	cfg.Gateway.OpenAIScheduler.StickyEscapeUtilization = 0.8
	cfg.Gateway.OpenAIScheduler.SampleSize = 2
	cfg.Gateway.OpenAIScheduler.SampleRounds = 1
	cfg.Gateway.OpenAIScheduler.SchedulingWaitTimeoutMS = 50
	cfg.Gateway.OpenAIScheduler.MaxWaiters = 1000
	acquiredIDs := make([]int64, 0, 2)
	svc := &OpenAIGatewayService{
		accountRepo:       schedulerTestOpenAIAccountRepo{accounts: accounts},
		schedulerSnapshot: adaptiveOpenAITestSnapshot(accounts),
		cache: &schedulerTestGatewayCache{sessionBindings: map[string]int64{
			"openai:adaptive-sticky": accounts[0].ID,
		}},
		cfg: cfg,
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{
			acquiredIDs: &acquiredIDs,
		}),
	}

	first, firstDecision, err := svc.SelectAccountWithScheduler(
		adaptiveOpenAITestContext(groupID), &groupID, "", "adaptive-sticky", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false,
	)
	require.NoError(t, err)
	require.Equal(t, openAIAccountScheduleLayerSessionSticky, firstDecision.Layer)
	require.Equal(t, accounts[0].ID, first.Account.ID)

	second, secondDecision, err := svc.SelectAccountWithScheduler(
		adaptiveOpenAITestContext(groupID), &groupID, "", "adaptive-sticky", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false,
	)
	require.NoError(t, err)
	require.Equal(t, openAIAccountScheduleLayerLoadBalance, secondDecision.Layer)
	require.Equal(t, accounts[1].ID, second.Account.ID)
	require.Equal(t, []int64{accounts[0].ID, accounts[1].ID}, acquiredIDs)

	first.ReleaseFunc()
	second.ReleaseFunc()
}

func TestOpenAIGatewayService_AdaptiveStickyRedisRejectionTriesDifferentAccount(t *testing.T) {
	groupID := int64(48)
	accounts := []Account{
		{ID: 50_501, Name: "sticky-redis-full", Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 10},
		{ID: 50_502, Name: "backup", Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 10},
	}
	cfg := &config.Config{}
	cfg.RunMode = config.RunModeSimple
	cfg.Gateway.OpenAIScheduler.AdaptiveEnabled = true
	cfg.Gateway.OpenAIScheduler.ShadowMode = false
	cfg.Gateway.OpenAIScheduler.InitialWindow = 2
	cfg.Gateway.OpenAIScheduler.MinWindow = 1
	cfg.Gateway.OpenAIScheduler.MaxWindow = 2
	cfg.Gateway.OpenAIScheduler.StickyEscapeUtilization = 0.8
	cfg.Gateway.OpenAIScheduler.SampleSize = 2
	cfg.Gateway.OpenAIScheduler.SampleRounds = 1
	cfg.Gateway.OpenAIScheduler.SchedulingWaitTimeoutMS = 50
	cfg.Gateway.OpenAIScheduler.MaxWaiters = 1000
	concurrencyCache := &adaptiveRejectFirstConcurrencyCache{}
	svc := &OpenAIGatewayService{
		accountRepo:       schedulerTestOpenAIAccountRepo{accounts: accounts},
		schedulerSnapshot: adaptiveOpenAITestSnapshot(accounts),
		cache: &schedulerTestGatewayCache{sessionBindings: map[string]int64{
			"openai:adaptive-sticky-redis": accounts[0].ID,
		}},
		cfg:                cfg,
		concurrencyService: NewConcurrencyService(concurrencyCache),
	}

	selection, decision, err := svc.SelectAccountWithScheduler(
		adaptiveOpenAITestContext(groupID), &groupID, "", "adaptive-sticky-redis", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false,
	)

	require.NoError(t, err)
	require.True(t, selection.Acquired)
	require.Equal(t, openAIAccountScheduleLayerLoadBalance, decision.Layer)
	require.Equal(t, accounts[1].ID, selection.Account.ID)
	require.Equal(t, []int64{accounts[0].ID, accounts[1].ID}, concurrencyCache.attempts())
	selection.ReleaseFunc()
}

func TestOpenAIGatewayService_AdaptivePreviousResponseEscapesAtUtilizationThreshold(t *testing.T) {
	groupID := int64(51)
	accounts := []Account{
		{
			ID: 50_801, Name: "previous-response", Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
			Status: StatusActive, Schedulable: true, Concurrency: 10,
			Extra: map[string]any{"openai_apikey_responses_websockets_v2_enabled": true},
		},
		{
			ID: 50_802, Name: "backup", Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
			Status: StatusActive, Schedulable: true, Concurrency: 10,
			Extra: map[string]any{"openai_apikey_responses_websockets_v2_enabled": true},
		},
	}
	cfg := newOpenAIWSV2TestConfig()
	cfg.RunMode = config.RunModeSimple
	cfg.Gateway.OpenAIScheduler.AdaptiveEnabled = true
	cfg.Gateway.OpenAIScheduler.ShadowMode = false
	cfg.Gateway.OpenAIScheduler.InitialWindow = 1
	cfg.Gateway.OpenAIScheduler.MinWindow = 1
	cfg.Gateway.OpenAIScheduler.MaxWindow = 1
	cfg.Gateway.OpenAIScheduler.StickyEscapeUtilization = 0.8
	cfg.Gateway.OpenAIScheduler.SampleSize = 2
	cfg.Gateway.OpenAIScheduler.SampleRounds = 1
	cfg.Gateway.OpenAIScheduler.SchedulingWaitTimeoutMS = 50
	cfg.Gateway.OpenAIScheduler.MaxWaiters = 1000
	gatewayCache := &schedulerTestGatewayCache{}
	store := NewOpenAIWSStateStore(gatewayCache)
	var accountMetadataCalls atomic.Int64
	snapshotCache := &openAISnapshotCacheStub{
		snapshotAccounts: []*Account{&accounts[0], &accounts[1]},
		accountsByID: map[int64]*Account{
			accounts[0].ID: &accounts[0],
			accounts[1].ID: &accounts[1],
		},
		accountCalls: &accountMetadataCalls,
	}
	repo := &adaptiveHotPathAccountRepo{
		schedulerTestOpenAIAccountRepo: schedulerTestOpenAIAccountRepo{accounts: accounts},
	}
	acquiredIDs := make([]int64, 0, 2)
	svc := &OpenAIGatewayService{
		accountRepo:        repo,
		cache:              gatewayCache,
		cfg:                cfg,
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{acquiredIDs: &acquiredIDs}),
		openaiWSStateStore: store,
		schedulerSnapshot:  &SchedulerSnapshotService{cache: snapshotCache, accountRepo: repo},
	}
	ctx := adaptiveOpenAITestContext(groupID)
	require.NoError(t, store.BindResponseAccount(ctx, groupID, "resp_adaptive_busy", accounts[0].ID, time.Hour))

	first, firstDecision, err := svc.SelectAccountWithScheduler(
		ctx, &groupID, "resp_adaptive_busy", "", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false,
	)
	require.NoError(t, err)
	require.Equal(t, openAIAccountScheduleLayerPreviousResponse, firstDecision.Layer)
	require.Equal(t, accounts[0].ID, first.Account.ID)

	second, secondDecision, err := svc.SelectAccountWithScheduler(
		ctx, &groupID, "resp_adaptive_busy", "", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false,
	)
	require.NoError(t, err)
	require.Equal(t, openAIAccountScheduleLayerLoadBalance, secondDecision.Layer)
	require.Equal(t, accounts[1].ID, second.Account.ID)
	require.Equal(t, []int64{accounts[0].ID, accounts[1].ID}, acquiredIDs)
	require.Zero(t, repo.getByIDCalls.Load(), "adaptive previous-response routing must not recheck PostgreSQL")
	require.Equal(t, int64(2), accountMetadataCalls.Load(), "each sticky attempt may read only its cached account metadata")

	first.ReleaseFunc()
	second.ReleaseFunc()
}

func TestOpenAIGatewayService_AdaptiveSchedulerDoesNotReacquireRedisRejectedAccount(t *testing.T) {
	groupID := int64(45)
	cfg := &config.Config{}
	cfg.RunMode = config.RunModeSimple
	cfg.Gateway.OpenAIScheduler.AdaptiveEnabled = true
	cfg.Gateway.OpenAIScheduler.ShadowMode = false
	cfg.Gateway.OpenAIScheduler.InitialWindow = 2
	cfg.Gateway.OpenAIScheduler.MinWindow = 1
	cfg.Gateway.OpenAIScheduler.MaxWindow = 32
	cfg.Gateway.OpenAIScheduler.SampleSize = 4
	cfg.Gateway.OpenAIScheduler.SampleRounds = 2
	cfg.Gateway.OpenAIScheduler.SchedulingWaitTimeoutMS = 20
	cfg.Gateway.OpenAIScheduler.MaxWaiters = 1000
	var loadCalls, acquireCalls, releaseCalls atomic.Int64
	cache := adaptiveCountingConcurrencyCache{
		schedulerTestConcurrencyCache: schedulerTestConcurrencyCache{
			acquireResults: map[int64]bool{50_201: false},
		},
		loadCalls:    &loadCalls,
		acquireCalls: &acquireCalls,
		releaseCalls: &releaseCalls,
	}
	accounts := []Account{{
		ID:          50_201,
		Name:        "redis-full",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 10,
	}}
	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: accounts},
		schedulerSnapshot:  adaptiveOpenAITestSnapshot(accounts),
		cache:              &schedulerTestGatewayCache{},
		cfg:                cfg,
		concurrencyService: NewConcurrencyService(cache),
	}

	selection, _, err := svc.SelectAccountWithScheduler(
		adaptiveOpenAITestContext(groupID),
		&groupID,
		"",
		"",
		"gpt-5.1",
		nil,
		OpenAIUpstreamTransportAny,
		false,
	)

	require.ErrorIs(t, err, ErrNoAvailableAccounts)
	require.Nil(t, selection)
	require.Zero(t, loadCalls.Load())
	require.Equal(t, int64(1), acquireCalls.Load(), "one scheduling call must not reacquire the same Redis-rejected account")
	require.Zero(t, releaseCalls.Load())
}

func TestOpenAIGatewayService_AdaptiveSchedulerTriesOneDifferentAccountAfterRedisDivergence(t *testing.T) {
	groupID := int64(46)
	cfg := &config.Config{}
	cfg.RunMode = config.RunModeSimple
	cfg.Gateway.OpenAIScheduler.AdaptiveEnabled = true
	cfg.Gateway.OpenAIScheduler.ShadowMode = false
	cfg.Gateway.OpenAIScheduler.InitialWindow = 2
	cfg.Gateway.OpenAIScheduler.MinWindow = 1
	cfg.Gateway.OpenAIScheduler.MaxWindow = 32
	cfg.Gateway.OpenAIScheduler.SampleSize = 4
	cfg.Gateway.OpenAIScheduler.SampleRounds = 2
	cfg.Gateway.OpenAIScheduler.SchedulingWaitTimeoutMS = 20
	cfg.Gateway.OpenAIScheduler.MaxWaiters = 1000
	cache := &adaptiveRejectFirstConcurrencyCache{}
	accounts := []Account{
		{ID: 50_301, Name: "first", Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 10},
		{ID: 50_302, Name: "second", Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 10},
	}
	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: accounts},
		schedulerSnapshot:  adaptiveOpenAITestSnapshot(accounts),
		cache:              &schedulerTestGatewayCache{},
		cfg:                cfg,
		concurrencyService: NewConcurrencyService(cache),
	}

	selection, _, err := svc.SelectAccountWithScheduler(
		adaptiveOpenAITestContext(groupID),
		&groupID,
		"",
		"",
		"gpt-5.1",
		nil,
		OpenAIUpstreamTransportAny,
		false,
	)

	require.NoError(t, err)
	require.NotNil(t, selection)
	attempts := cache.attempts()
	require.Len(t, attempts, 2)
	require.NotEqual(t, attempts[0], attempts[1], "Redis divergence retry must exclude the rejected account")
	selection.ReleaseFunc()
}

func TestOpenAIGatewayService_AdaptiveSchedulerWaitsForLocalPermitRelease(t *testing.T) {
	groupID := int64(42)
	ctx := adaptiveOpenAITestContext(groupID)
	cfg := &config.Config{}
	cfg.RunMode = config.RunModeSimple
	cfg.Gateway.OpenAIScheduler.AdaptiveEnabled = true
	cfg.Gateway.OpenAIScheduler.ShadowMode = false
	cfg.Gateway.OpenAIScheduler.InitialWindow = 1
	cfg.Gateway.OpenAIScheduler.MinWindow = 1
	cfg.Gateway.OpenAIScheduler.MaxWindow = 1
	cfg.Gateway.OpenAIScheduler.SampleSize = 4
	cfg.Gateway.OpenAIScheduler.SampleRounds = 2
	cfg.Gateway.OpenAIScheduler.SchedulingWaitTimeoutMS = 1000
	cfg.Gateway.OpenAIScheduler.MaxWaiters = 1000
	var loadCalls, acquireCalls, releaseCalls atomic.Int64
	cache := adaptiveCountingConcurrencyCache{
		schedulerTestConcurrencyCache: schedulerTestConcurrencyCache{},
		loadCalls:                     &loadCalls,
		acquireCalls:                  &acquireCalls,
		releaseCalls:                  &releaseCalls,
	}
	accounts := []Account{{
		ID:          50_001,
		Name:        "single-account",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 10,
	}}
	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: accounts},
		schedulerSnapshot:  adaptiveOpenAITestSnapshot(accounts),
		cache:              &schedulerTestGatewayCache{},
		cfg:                cfg,
		concurrencyService: NewConcurrencyService(cache),
	}

	first, _, err := svc.SelectAccountWithScheduler(ctx, &groupID, "", "", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false)
	require.NoError(t, err)
	require.NotNil(t, first)

	type selectionResult struct {
		selection *AccountSelectionResult
		err       error
	}
	secondCh := make(chan selectionResult, 1)
	go func() {
		selection, _, selectErr := svc.SelectAccountWithScheduler(ctx, &groupID, "", "", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false)
		secondCh <- selectionResult{selection: selection, err: selectErr}
	}()

	scheduler := svc.openaiScheduler.(*defaultOpenAIAccountScheduler)
	require.Eventually(t, func() bool { return scheduler.adaptive.waiters.Load() == 1 }, time.Second, time.Millisecond)
	releasedAt := time.Now()
	first.ReleaseFunc()
	second := <-secondCh
	require.NoError(t, second.err)
	require.NotNil(t, second.selection)
	require.Less(t, time.Since(releasedAt), 100*time.Millisecond)
	second.selection.ReleaseFunc()
	require.Zero(t, loadCalls.Load())
	require.Equal(t, int64(2), acquireCalls.Load())
	require.Equal(t, int64(2), releaseCalls.Load())
}

func TestOpenAIGatewayService_AdaptiveSchedulerSelectsCreditBackedSevenDayAccount(t *testing.T) {
	now := time.Now().UTC()
	groupID := int64(43)
	account := Account{
		ID:          50_101,
		Name:        "credit-backed-oauth",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 10,
		Extra: map[string]any{
			"codex_7d_used_percent": 100.0,
			"codex_7d_reset_at":     now.Add(48 * time.Hour).Format(time.RFC3339),
			openaiQuotaResetCreditsKey: map[string]any{
				"available_count": float64(3),
				"credits": []any{map[string]any{
					"expires_at": now.Add(24 * time.Hour).Format(time.RFC3339),
				}},
			},
		},
	}
	pauseUntil := now.Add(48 * time.Hour)
	account.TempUnschedulableUntil = &pauseUntil
	account.TempUnschedulableReason = BuildDetailedAccountSchedulingThresholdReason(AccountSchedulingThresholdReasonInput{
		Platform:         PlatformOpenAI,
		Window:           "7d",
		ThresholdPercent: 99,
		UsedPercent:      100,
		Until:            pauseUntil,
		Now:              now,
	})
	cfg := &config.Config{}
	cfg.RunMode = config.RunModeSimple
	cfg.Gateway.OpenAIScheduler.AdaptiveEnabled = true
	cfg.Gateway.OpenAIScheduler.ShadowMode = false
	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: []Account{account}},
		schedulerSnapshot:  adaptiveOpenAITestSnapshot([]Account{account}),
		cache:              &schedulerTestGatewayCache{},
		cfg:                cfg,
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
	}

	selection, _, err := svc.SelectAccountWithScheduler(adaptiveOpenAITestContext(groupID), &groupID, "", "", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false)

	require.NoError(t, err)
	require.NotNil(t, selection)
	require.Equal(t, account.ID, selection.Account.ID)
	selection.ReleaseFunc()
}

func TestOpenAIGatewayService_AdaptiveSchedulerKeepsFiveHourPause(t *testing.T) {
	now := time.Now().UTC()
	groupID := int64(44)
	account := Account{
		ID:          50_102,
		Name:        "five-hour-limited",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 10,
		Extra: map[string]any{
			"codex_5h_used_percent": 100.0,
			"codex_5h_reset_at":     now.Add(2 * time.Hour).Format(time.RFC3339),
			openaiQuotaResetCreditsKey: map[string]any{
				"available_count": float64(3),
			},
		},
	}
	pauseUntil := now.Add(2 * time.Hour)
	account.TempUnschedulableUntil = &pauseUntil
	account.TempUnschedulableReason = BuildDetailedAccountSchedulingThresholdReason(AccountSchedulingThresholdReasonInput{
		Platform:         PlatformOpenAI,
		Window:           "5h",
		ThresholdPercent: 99,
		UsedPercent:      100,
		Until:            pauseUntil,
		Now:              now,
	})
	cfg := &config.Config{}
	cfg.RunMode = config.RunModeSimple
	cfg.Gateway.OpenAIScheduler.AdaptiveEnabled = true
	cfg.Gateway.OpenAIScheduler.ShadowMode = false
	svc := &OpenAIGatewayService{
		accountRepo:       schedulerTestOpenAIAccountRepo{accounts: []Account{account}},
		schedulerSnapshot: adaptiveOpenAITestSnapshot([]Account{account}),
		cache:             &schedulerTestGatewayCache{},
		cfg:               cfg,
	}

	selection, _, err := svc.SelectAccountWithScheduler(adaptiveOpenAITestContext(groupID), &groupID, "", "", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false)

	require.Error(t, err)
	require.ErrorIs(t, err, ErrNoAvailableAccounts)
	require.Nil(t, selection)
}

type adaptiveSnapshotAccountRepo struct {
	AccountRepository
	modelCandidates  []Account
	modelCalls       atomic.Int64
	schedulableCalls atomic.Int64
}

func (r *adaptiveSnapshotAccountRepo) ListModelAvailabilityCandidates(context.Context, *int64, []string, bool) ([]Account, error) {
	r.modelCalls.Add(1)
	return append([]Account(nil), r.modelCandidates...), nil
}

func (r *adaptiveSnapshotAccountRepo) ListSchedulableByPlatform(context.Context, string) ([]Account, error) {
	r.schedulableCalls.Add(1)
	return nil, nil
}

func TestSchedulerSnapshotAdaptiveOpenAIIncludesTransientAccountsForLocalFiltering(t *testing.T) {
	cfg := &config.Config{RunMode: config.RunModeSimple}
	cfg.Gateway.OpenAIScheduler.AdaptiveEnabled = true
	repo := &adaptiveSnapshotAccountRepo{modelCandidates: []Account{{ID: 50_103, Platform: PlatformOpenAI}}}
	snapshot := NewSchedulerSnapshotService(nil, nil, repo, nil, cfg)

	accounts, err := snapshot.loadAccountsFromDB(context.Background(), SchedulerBucket{
		Platform: PlatformOpenAI,
		Mode:     SchedulerModeSingle,
	}, false)

	require.NoError(t, err)
	require.Len(t, accounts, 1)
	require.Equal(t, int64(1), repo.modelCalls.Load())
	require.Zero(t, repo.schedulableCalls.Load())
}

func TestOpenAIAdaptiveRuntimeInitialWindowAndIdempotentRelease(t *testing.T) {
	now := time.Unix(1000, 0)
	runtime := newOpenAIAdaptiveRuntime(testOpenAIAdaptiveConfig())

	first, ok := runtime.tryReserve(101, 10_000, "route", now)
	require.True(t, ok)
	second, ok := runtime.tryReserve(101, 10_000, "route", now)
	require.True(t, ok)
	_, ok = runtime.tryReserve(101, 10_000, "route", now)
	require.False(t, ok)

	snapshot := runtime.snapshot(101, 10_000, now)
	require.Equal(t, 2, snapshot.window)
	require.Equal(t, 2, snapshot.inflight)
	require.Equal(t, 1.0, snapshot.utilization)

	first.Release()
	first.Release()
	require.Equal(t, 1, runtime.snapshot(101, 10_000, now).inflight)
	second.Release()
	require.Zero(t, runtime.snapshot(101, 10_000, now).inflight)
}

func TestOpenAIAdaptiveRuntimeConcurrentReserveNeverExceedsWindow(t *testing.T) {
	now := time.Unix(2000, 0)
	runtime := newOpenAIAdaptiveRuntime(testOpenAIAdaptiveConfig())
	start := make(chan struct{})
	release := make(chan struct{})
	var acquired atomic.Int64
	var wg sync.WaitGroup

	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			permit, ok := runtime.tryReserve(102, 10_000, "route", now)
			if !ok {
				return
			}
			acquired.Add(1)
			<-release
			permit.Release()
		}()
	}
	close(start)
	require.Eventually(t, func() bool { return runtime.snapshot(102, 10_000, now).inflight == 2 }, time.Second, time.Millisecond)
	require.Equal(t, int64(2), acquired.Load())
	close(release)
	wg.Wait()
	require.Zero(t, runtime.snapshot(102, 10_000, now).inflight)
}

func TestOpenAIAdaptiveRuntimeAIMDIncreaseIsPacedAndCapped(t *testing.T) {
	now := time.Unix(3000, 0)
	cfg := testOpenAIAdaptiveConfig()
	runtime := newOpenAIAdaptiveRuntime(cfg)
	permit, ok := runtime.tryReserve(103, 3, "route", now)
	require.True(t, ok)
	permit.Release()

	runtime.reportSuccess(103, 3, now.Add(time.Second), 100*time.Millisecond)
	runtime.reportSuccess(103, 3, now.Add(time.Second), 100*time.Millisecond)
	require.Equal(t, 2, runtime.snapshot(103, 3, now.Add(time.Second)).window, "increase interval must prevent an immediate jump")

	runtime.reportSuccess(103, 3, now.Add(2*time.Second), 100*time.Millisecond)
	require.Equal(t, 3, runtime.snapshot(103, 3, now.Add(2*time.Second)).window)

	for i := 0; i < 100; i++ {
		runtime.reportSuccess(103, 3, now.Add(time.Duration(4+i)*time.Second), 100*time.Millisecond)
	}
	require.Equal(t, 3, runtime.snapshot(103, 3, now.Add(2*time.Minute)).window, "account hard limit must cap AIMD")
}

func TestOpenAIAdaptiveRuntime429BackoffSequence(t *testing.T) {
	now := time.Unix(4000, 0)
	cfg := testOpenAIAdaptiveConfig()
	cfg.initialWindow = 8
	runtime := newOpenAIAdaptiveRuntime(cfg)
	permit, ok := runtime.tryReserve(104, 10_000, "route", now)
	require.True(t, ok)
	permit.Release()

	firstUntil := runtime.report429(104, 10_000, now, time.Time{})
	require.Equal(t, now.Add(2*time.Second), firstUntil)
	require.Equal(t, 4, runtime.snapshot(104, 10_000, now).window)

	secondAt := firstUntil.Add(time.Millisecond)
	secondUntil := runtime.report429(104, 10_000, secondAt, time.Time{})
	require.Equal(t, secondAt.Add(5*time.Second), secondUntil)
	require.Equal(t, 1, runtime.snapshot(104, 10_000, secondAt).window)

	thirdAt := secondUntil.Add(time.Millisecond)
	require.Equal(t, thirdAt.Add(15*time.Second), runtime.report429(104, 10_000, thirdAt, time.Time{}))
	fourthAt := thirdAt.Add(16 * time.Second)
	require.Equal(t, fourthAt.Add(30*time.Second), runtime.report429(104, 10_000, fourthAt, time.Time{}))
}

func TestOpenAIAdaptiveRuntimeHalfOpenAllowsOneProbeAndSuccessRecovers(t *testing.T) {
	now := time.Unix(5000, 0)
	runtime := newOpenAIAdaptiveRuntime(testOpenAIAdaptiveConfig())
	permit, ok := runtime.tryReserve(105, 10_000, "route", now)
	require.True(t, ok)
	cooldownUntil := runtime.report429(105, 10_000, now, time.Time{})
	permit.Release()

	_, ok = runtime.tryReserve(105, 10_000, "route", cooldownUntil.Add(-time.Nanosecond))
	require.False(t, ok)
	probe, ok := runtime.tryReserve(105, 10_000, "route", cooldownUntil)
	require.True(t, ok)
	_, ok = runtime.tryReserve(105, 10_000, "route", cooldownUntil)
	require.False(t, ok, "half-open permits exactly one probe")

	runtime.reportSuccess(105, 10_000, cooldownUntil.Add(time.Millisecond), 80*time.Millisecond)
	require.Equal(t, 2, runtime.snapshot(105, 10_000, cooldownUntil.Add(time.Millisecond)).window)
	require.False(t, runtime.snapshot(105, 10_000, cooldownUntil.Add(time.Millisecond)).halfOpen)
	probe.Release()

	first, ok := runtime.tryReserve(105, 10_000, "route", cooldownUntil.Add(2*time.Millisecond))
	require.True(t, ok)
	second, ok := runtime.tryReserve(105, 10_000, "route", cooldownUntil.Add(2*time.Millisecond))
	require.True(t, ok)
	first.Release()
	second.Release()
}

func TestOpenAIAdaptiveRuntimeHalfOpenReleaseWaitsForOutcome(t *testing.T) {
	now := time.Unix(5200, 0)
	runtime := newOpenAIAdaptiveRuntime(testOpenAIAdaptiveConfig())
	cooldownUntil := runtime.report429(108, 10_000, now, time.Time{})
	probe, ok := runtime.tryReserve(108, 10_000, "route", cooldownUntil)
	require.True(t, ok)

	probe.Release()
	secondProbe, ok := runtime.tryReserve(108, 10_000, "route", cooldownUntil.Add(time.Millisecond))

	require.False(t, ok, "releasing transport capacity before outcome reporting must not admit a second half-open probe")
	require.Nil(t, secondProbe)
	runtime.reportSuccess(108, 10_000, cooldownUntil.Add(2*time.Millisecond), time.Millisecond)
}

func TestOpenAIAdaptiveRuntimeHalfOpenFailureReentersCooldown(t *testing.T) {
	now := time.Unix(5500, 0)
	runtime := newOpenAIAdaptiveRuntime(testOpenAIAdaptiveConfig())
	cooldownUntil := runtime.report429(107, 10_000, now, time.Time{})

	probe, ok := runtime.tryReserve(107, 10_000, "route", cooldownUntil)
	require.True(t, ok)
	runtime.reportFailure(107, 10_000, cooldownUntil.Add(time.Millisecond))
	probe.Release()

	snapshot := runtime.snapshot(107, 10_000, cooldownUntil.Add(time.Millisecond))
	require.True(t, snapshot.cooldownUntil.After(cooldownUntil), "failed half-open probe must reopen the circuit")
	_, ok = runtime.tryReserve(107, 10_000, "route", cooldownUntil.Add(2*time.Millisecond))
	require.False(t, ok)
}

func TestOpenAIAdaptiveRuntimeExplicitResetWinsOverFallback(t *testing.T) {
	now := time.Unix(6000, 0)
	resetAt := now.Add(3 * time.Minute)
	runtime := newOpenAIAdaptiveRuntime(testOpenAIAdaptiveConfig())

	until := runtime.report429(106, 10_000, now, resetAt)

	require.Equal(t, resetAt, until)
	_, ok := runtime.tryReserve(106, 10_000, "route", now.Add(time.Minute))
	require.False(t, ok)
}

func TestOpenAIAdaptiveRuntimePrunesIdleAccountAndRouteState(t *testing.T) {
	now := time.Unix(5900, 0)
	runtime := newOpenAIAdaptiveRuntime(testOpenAIAdaptiveConfig())
	permit, ok := runtime.tryReserve(109, 10, "openai|gpt-5.1", now)
	require.True(t, ok)
	permit.Release()
	runtime.reportRouteAttempt("openai|gpt-5.1", false, now)

	runtime.prune(now.Add(openAIAdaptiveAccountStateTTL + time.Second))

	accountShard := runtime.shard(109)
	accountShard.mu.Lock()
	_, accountExists := accountShard.accounts[109]
	accountShard.mu.Unlock()
	routeShard := runtime.routeShard("openai|gpt-5.1")
	routeShard.mu.Lock()
	_, routeExists := routeShard.routes["openai|gpt-5.1"]
	routeShard.mu.Unlock()
	require.False(t, accountExists)
	require.False(t, routeExists)
}

func TestOpenAIAdaptiveRuntimePruneKeepsInflightAndCoolingAccounts(t *testing.T) {
	now := time.Unix(5950, 0)
	runtime := newOpenAIAdaptiveRuntime(testOpenAIAdaptiveConfig())
	permit, ok := runtime.tryReserve(110, 10, "route", now)
	require.True(t, ok)
	runtime.report429(111, 10, now, now.Add(openAIAdaptiveAccountStateTTL+time.Minute))

	runtime.prune(now.Add(openAIAdaptiveAccountStateTTL + time.Second))

	for _, accountID := range []int64{110, 111} {
		shard := runtime.shard(accountID)
		shard.mu.Lock()
		_, exists := shard.accounts[accountID]
		shard.mu.Unlock()
		require.True(t, exists)
	}
	permit.Release()
}

func TestOpenAIAdaptiveRuntimeStateMapsStayBoundedPerShard(t *testing.T) {
	now := time.Unix(5975, 0)
	runtime := newOpenAIAdaptiveRuntime(testOpenAIAdaptiveConfig())
	for i := 0; i < openAIAdaptiveMaxAccountStatesPerShard+10; i++ {
		accountID := int64(1 + i*openAIAdaptiveRuntimeShardCount)
		permit, ok := runtime.tryReserve(accountID, 1, "route", now.Add(time.Duration(i)*time.Millisecond))
		require.True(t, ok)
		permit.Release()
	}
	accountShard := runtime.shard(1)
	accountShard.mu.Lock()
	accountCount := len(accountShard.accounts)
	accountShard.mu.Unlock()
	require.LessOrEqual(t, accountCount, openAIAdaptiveMaxAccountStatesPerShard)

	for i := 0; i < openAIAdaptiveMaxRouteStatesPerShard+10; i++ {
		routeKey := ""
		for attempt := 0; ; attempt++ {
			candidate := fmt.Sprintf("route-%d-%d", i, attempt)
			if runtime.routeShard(candidate) == &runtime.routes[0] {
				routeKey = candidate
				break
			}
		}
		runtime.reportRouteAttempt(routeKey, false, now.Add(time.Duration(i)*time.Millisecond))
	}
	routeShard := &runtime.routes[0]
	routeShard.mu.Lock()
	routeCount := len(routeShard.routes)
	routeShard.mu.Unlock()
	require.LessOrEqual(t, routeCount, openAIAdaptiveMaxRouteStatesPerShard)
}

func TestOpenAIAdaptive429UpdatesWindowBeforeFallbackSettingRead(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.OpenAIScheduler.AdaptiveEnabled = true
	cfg.Gateway.OpenAIScheduler.ShadowMode = false
	cfg.Gateway.OpenAIScheduler.InitialWindow = 8
	cfg.Gateway.OpenAIScheduler.MinWindow = 1
	cfg.Gateway.OpenAIScheduler.MaxWindow = 32
	settingRepo := &adaptiveBlockingSettingRepo{entered: make(chan struct{}), release: make(chan struct{})}
	defer close(settingRepo.release)
	svc := &OpenAIGatewayService{
		cfg: cfg,
		rateLimitService: &RateLimitService{
			settingService: NewSettingService(settingRepo, cfg),
		},
	}
	scheduler := svc.getOpenAIAccountScheduler(context.Background()).(*defaultOpenAIAccountScheduler)
	account := &Account{ID: 57_000, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Concurrency: 32}
	done := make(chan struct{})

	go func() {
		svc.reportOpenAIAdaptive429(context.Background(), account, http.Header{}, nil, "gpt-5.1")
		close(done)
	}()

	require.Eventually(t, func() bool {
		return scheduler.adaptive.snapshot(account.ID, account.Concurrency, time.Now()).window == 4
	}, 100*time.Millisecond, time.Millisecond, "local AIMD must not wait for a fallback setting database read")
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("adaptive 429 reporting blocked on fallback setting read")
	}
}

func TestOpenAIAdaptiveShadowModeInitializesObserverWithoutChangingSelection(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.OpenAIScheduler.AdaptiveEnabled = false
	cfg.Gateway.OpenAIScheduler.ShadowMode = true
	cfg.Gateway.OpenAIScheduler.InitialWindow = 8
	cfg.Gateway.OpenAIScheduler.MinWindow = 1
	cfg.Gateway.OpenAIScheduler.MaxWindow = 32
	svc := &OpenAIGatewayService{cfg: cfg}

	require.Nil(t, svc.getOpenAIAccountScheduler(context.Background()), "shadow rollout must keep the legacy dispatch path")

	account := &Account{ID: 57_010, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Concurrency: 32}
	svc.reportOpenAIAdaptive429(context.Background(), account, http.Header{"Retry-After": []string{"30"}}, nil, "gpt-5.1")

	scheduler, ok := svc.openaiScheduler.(*defaultOpenAIAccountScheduler)
	require.True(t, ok, "shadow feedback must initialize the scheduler observer")
	require.NotNil(t, scheduler.adaptive)
	require.False(t, scheduler.adaptiveSelectionEnabled(OpenAIAccountScheduleRequest{Platform: PlatformOpenAI}), "shadow rollout must keep adaptive account selection disabled")
	snapshot := scheduler.adaptive.snapshot(account.ID, account.Concurrency, time.Now())
	require.Equal(t, 4, snapshot.window, "shadow rollout must observe immediate 429 feedback")
}

func TestOpenAIAdaptiveStormCountsEarlyReturnUpstreamErrors(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.OpenAIScheduler.AdaptiveEnabled = true
	cfg.Gateway.OpenAIScheduler.ShadowMode = false
	svc := &OpenAIGatewayService{cfg: cfg}
	scheduler := svc.getOpenAIAccountScheduler(context.Background()).(*defaultOpenAIAccountScheduler)
	account := &Account{ID: 57_001, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Concurrency: 32}
	model := "gpt-5.1"
	contextWindowBody := []byte(`{"error":{"message":"Your input exceeds the context window of this model."}}`)
	resetHeaders := http.Header{"Retry-After": []string{"1"}}

	for i := 0; i < 16; i++ {
		svc.handleOpenAIAccountUpstreamError(context.Background(), account, http.StatusBadRequest, nil, contextWindowBody, model)
	}
	for i := 0; i < 4; i++ {
		svc.handleOpenAIAccountUpstreamError(context.Background(), account, http.StatusTooManyRequests, resetHeaders, nil, model)
	}

	require.True(t, scheduler.adaptive.isRouteStorm(openAIAdaptiveResultRouteKey(account.Platform, model)), "4 of 20 completed upstream attempts must enter storm mode")
}

func TestOpenAIAdaptiveStormRequiresMinimumAttemptsAndRatio(t *testing.T) {
	now := time.Unix(10_000, 0)
	cfg := testOpenAIAdaptiveConfig()
	cfg.stormWindow = 5 * time.Second
	cfg.stormMinAttempts = 20
	cfg.storm429Ratio = 0.20
	cfg.stormRecoveryRatio = 0.05
	runtime := newOpenAIAdaptiveRuntime(cfg)

	runtime.reportRouteAttempt("openai|gpt-5.1", true, now)
	require.False(t, runtime.isRouteStorm("openai|gpt-5.1"), "one 429 cannot start a storm")
	for i := 1; i < 20; i++ {
		runtime.reportRouteAttempt("openai|gpt-5.1", i < 4, now.Add(time.Duration(i)*time.Millisecond))
	}
	require.True(t, runtime.isRouteStorm("openai|gpt-5.1"), "4/20 attempts must start a storm")

	belowThreshold := newOpenAIAdaptiveRuntime(cfg)
	for i := 0; i < 20; i++ {
		belowThreshold.reportRouteAttempt("openai|gpt-5.2", i < 2, now.Add(time.Duration(i)*time.Millisecond))
	}
	require.False(t, belowThreshold.isRouteStorm("openai|gpt-5.2"), "2/20 attempts is 10%, not the configured 20% threshold")
}

func TestOpenAIAdaptiveStormUsesMappedUpstreamRouteConsistently(t *testing.T) {
	now := time.Unix(10_500, 0)
	cfg := testOpenAIAdaptiveConfig()
	cfg.initialWindow = 8
	cfg.maxWindow = 8
	cfg.stormWindow = 5 * time.Second
	cfg.stormMinAttempts = 20
	cfg.storm429Ratio = 0.20
	runtime := newOpenAIAdaptiveRuntime(cfg)
	account := &Account{
		ID: 59_001, Platform: PlatformOpenAI, Concurrency: 8,
		Credentials: map[string]any{
			"model_mapping": map[string]any{"customer-model": "gpt-5.1"},
		},
	}
	req := OpenAIAccountScheduleRequest{Platform: PlatformOpenAI, RequestedModel: "customer-model"}
	routeKey := openAIAdaptiveRouteKeyForAccount(req, account)

	for i := 0; i < 20; i++ {
		runtime.reportRouteAttempt(openAIAdaptiveResultRouteKey(account.Platform, account.GetMappedModel(req.RequestedModel)), i < 4, now.Add(time.Duration(i)*time.Millisecond))
	}

	require.True(t, runtime.isRouteStorm(openAIAdaptiveStormRouteKey(routeKey)))
	permits := make([]*openAIAdaptivePermit, 0, 6)
	for i := 0; i < 6; i++ {
		permit, ok := runtime.tryReserve(account.ID, account.Concurrency, routeKey, now.Add(time.Second))
		require.True(t, ok)
		permits = append(permits, permit)
	}
	_, ok := runtime.tryReserve(account.ID, account.Concurrency, routeKey, now.Add(time.Second))
	require.False(t, ok, "mapped 429 storm must contract the same route used during selection")
	for _, permit := range permits {
		permit.Release()
	}
}

func TestOpenAIAdaptiveStormContractsWindowAndRecoversAfterTenCleanSeconds(t *testing.T) {
	now := time.Unix(11_000, 0)
	cfg := testOpenAIAdaptiveConfig()
	cfg.initialWindow = 8
	cfg.maxWindow = 8
	cfg.stormWindow = 5 * time.Second
	cfg.stormMinAttempts = 20
	cfg.storm429Ratio = 0.20
	cfg.stormRecoveryRatio = 0.05
	runtime := newOpenAIAdaptiveRuntime(cfg)
	route := "openai|gpt-5.1"
	for i := 0; i < 20; i++ {
		runtime.reportRouteAttempt(route, i < 4, now.Add(time.Duration(i)*time.Millisecond))
	}
	require.True(t, runtime.isRouteStorm(route))

	permits := make([]*openAIAdaptivePermit, 0, 6)
	for i := 0; i < 6; i++ {
		permit, ok := runtime.tryReserve(60_001, 100, route, now.Add(time.Second))
		require.True(t, ok)
		permits = append(permits, permit)
	}
	_, ok := runtime.tryReserve(60_001, 100, route, now.Add(time.Second))
	require.False(t, ok, "storm must contract an 8-request window to 6")
	for _, permit := range permits {
		permit.Release()
	}

	runtime.reportRouteAttempt(route, false, now.Add(6*time.Second))
	require.True(t, runtime.isRouteStorm(route), "recovery needs ten clean seconds")
	runtime.reportRouteAttempt(route, false, now.Add(15*time.Second+999*time.Millisecond))
	require.True(t, runtime.isRouteStorm(route))
	runtime.reportRouteAttempt(route, false, now.Add(16*time.Second))
	require.False(t, runtime.isRouteStorm(route))
}

func TestMarkOpenAIOAuth429UpdatesAdaptiveWindowImmediately(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.OpenAIScheduler.AdaptiveEnabled = true
	cfg.Gateway.OpenAIScheduler.ShadowMode = false
	cfg.Gateway.OpenAIScheduler.InitialWindow = 8
	cfg.Gateway.OpenAIScheduler.MinWindow = 1
	cfg.Gateway.OpenAIScheduler.MaxWindow = 32
	svc := &OpenAIGatewayService{cfg: cfg}
	scheduler := svc.getOpenAIAccountScheduler(context.Background()).(*defaultOpenAIAccountScheduler)
	account := &Account{ID: 60_002, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Concurrency: 100}
	now := time.Now()
	resetAt := now.Add(3 * time.Minute)
	headers := http.Header{}
	headers.Set("x-codex-primary-used-percent", "100")
	headers.Set("x-codex-primary-reset-after-seconds", "180")
	headers.Set("x-codex-primary-window-minutes", "300")

	svc.handleOpenAIAccountUpstreamError(context.Background(), account, http.StatusTooManyRequests, headers, nil, "gpt-5.1")

	snapshot := scheduler.adaptive.snapshot(account.ID, account.Concurrency, time.Now())
	require.Equal(t, 4, snapshot.window)
	require.WithinDuration(t, resetAt, snapshot.cooldownUntil, 2*time.Second)
}

func TestOpenAIAPIKey429UpdatesAdaptiveWindowImmediately(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.OpenAIScheduler.AdaptiveEnabled = true
	cfg.Gateway.OpenAIScheduler.ShadowMode = false
	cfg.Gateway.OpenAIScheduler.InitialWindow = 8
	cfg.Gateway.OpenAIScheduler.MinWindow = 1
	cfg.Gateway.OpenAIScheduler.MaxWindow = 32
	svc := &OpenAIGatewayService{cfg: cfg}
	scheduler := svc.getOpenAIAccountScheduler(context.Background()).(*defaultOpenAIAccountScheduler)
	account := &Account{ID: 60_003, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Concurrency: 100}

	svc.handleOpenAIAccountUpstreamError(
		context.Background(),
		account,
		http.StatusTooManyRequests,
		http.Header{"Retry-After": []string{"30"}},
		nil,
		"gpt-5.1",
	)

	snapshot := scheduler.adaptive.snapshot(account.ID, account.Concurrency, time.Now())
	require.Equal(t, 4, snapshot.window)
	require.Greater(t, time.Until(snapshot.cooldownUntil), 25*time.Second)
}
