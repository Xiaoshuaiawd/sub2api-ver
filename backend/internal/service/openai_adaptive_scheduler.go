package service

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"math"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	openAIAdaptiveRuntimeShardCount        = 64
	openAIAdaptiveAccountStateTTL          = 30 * time.Minute
	openAIAdaptiveRouteStateTTL            = 15 * time.Minute
	openAIAdaptiveStatePruneInterval       = time.Minute
	openAIAdaptiveAbandonedProbeCooldown   = 250 * time.Millisecond
	openAIAdaptiveRedisFullCooldown        = 250 * time.Millisecond
	openAIAdaptiveMaxAccountStatesPerShard = 256
	openAIAdaptiveMaxRouteStatesPerShard   = 256
)

var (
	errOpenAIAdaptiveWaitTimeout = errors.New("openai adaptive scheduler wait timeout")
	errOpenAIAdaptiveWaiterLimit = errors.New("openai adaptive scheduler waiter limit reached")
)

type openAIAdaptiveSchedulerConfig struct {
	enabled             bool
	shadowMode          bool
	initialWindow       int
	minWindow           int
	maxWindow           int
	increaseInterval    time.Duration
	no429IncreaseWindow time.Duration
	sampleSize          int
	sampleRounds        int
	waitTimeout         time.Duration
	stickyEscape        float64
	maxWaiters          int
	stormWindow         time.Duration
	stormMinAttempts    int
	storm429Ratio       float64
	stormRecoveryRatio  float64
}

type openAIAdaptiveRuntime struct {
	config      openAIAdaptiveSchedulerConfig
	shards      [openAIAdaptiveRuntimeShardCount]openAIAdaptiveRuntimeShard
	routes      [openAIAdaptiveRuntimeShardCount]openAIAdaptiveRouteShard
	waiters     atomic.Int64
	lastPruneAt atomic.Int64
	permitSeq   atomic.Uint64
}

func (s *OpenAIGatewayService) openAIAdaptiveConfig() openAIAdaptiveSchedulerConfig {
	result := openAIAdaptiveSchedulerConfig{
		initialWindow:       2,
		minWindow:           1,
		maxWindow:           32,
		increaseInterval:    2 * time.Second,
		no429IncreaseWindow: 30 * time.Second,
		sampleSize:          4,
		sampleRounds:        2,
		waitTimeout:         time.Second,
		stickyEscape:        0.8,
		maxWaiters:          1000,
		stormWindow:         5 * time.Second,
		stormMinAttempts:    20,
		storm429Ratio:       0.20,
		stormRecoveryRatio:  0.05,
	}
	if s == nil || s.cfg == nil {
		return result
	}
	cfg := s.cfg.Gateway.OpenAIScheduler
	result.enabled = cfg.AdaptiveEnabled
	result.shadowMode = cfg.ShadowMode
	if cfg.InitialWindow > 0 {
		result.initialWindow = cfg.InitialWindow
	}
	if cfg.MinWindow > 0 {
		result.minWindow = cfg.MinWindow
	}
	if cfg.MaxWindow > 0 {
		result.maxWindow = cfg.MaxWindow
	}
	if cfg.IncreaseIntervalMS > 0 {
		result.increaseInterval = time.Duration(cfg.IncreaseIntervalMS) * time.Millisecond
	}
	if cfg.No429IncreaseWindowSeconds > 0 {
		result.no429IncreaseWindow = time.Duration(cfg.No429IncreaseWindowSeconds) * time.Second
	}
	if cfg.SampleSize > 0 {
		result.sampleSize = cfg.SampleSize
	}
	if cfg.SampleRounds > 0 {
		result.sampleRounds = cfg.SampleRounds
	}
	if cfg.SchedulingWaitTimeoutMS > 0 {
		result.waitTimeout = time.Duration(cfg.SchedulingWaitTimeoutMS) * time.Millisecond
	}
	if cfg.StickyEscapeUtilization > 0 && cfg.StickyEscapeUtilization <= 1 {
		result.stickyEscape = cfg.StickyEscapeUtilization
	}
	if cfg.MaxWaiters > 0 {
		result.maxWaiters = cfg.MaxWaiters
	}
	if cfg.StormWindowSeconds > 0 {
		result.stormWindow = time.Duration(cfg.StormWindowSeconds) * time.Second
	}
	if cfg.StormMinAttempts > 0 {
		result.stormMinAttempts = cfg.StormMinAttempts
	}
	if cfg.Storm429Ratio > 0 && cfg.Storm429Ratio <= 1 {
		result.storm429Ratio = cfg.Storm429Ratio
	}
	if cfg.StormRecoveryRatio >= 0 && cfg.StormRecoveryRatio < result.storm429Ratio {
		result.stormRecoveryRatio = cfg.StormRecoveryRatio
	}
	return result
}

func openAIAdaptiveRouteKey(req OpenAIAccountScheduleRequest) string {
	return fmt.Sprintf("%s|pool:%d:%s:%s:%s:%t",
		openAIAdaptiveResultRouteKey(req.Platform, req.RequestedModel),
		derefGroupID(req.GroupID),
		req.RequiredTransport,
		req.RequiredCapability,
		req.RequiredImageCapability,
		req.RequireCompact,
	)
}

func openAIAdaptiveRouteKeyForAccount(req OpenAIAccountScheduleRequest, account *Account) string {
	accountView := openAIAdaptiveImmutableAccountView(account)
	req.RequestedModel = canonicalOpenAIAccountSchedulingModel(accountView, req.RequestedModel)
	return openAIAdaptiveRouteKey(req)
}

func openAIAdaptiveCandidateRouteKey(routeKey string, account *Account) string {
	stormKey, poolKey, found := strings.Cut(routeKey, "|pool:")
	if !found {
		return routeKey
	}
	platform, model, found := strings.Cut(stormKey, "|")
	if !found {
		return routeKey
	}
	accountView := openAIAdaptiveImmutableAccountView(account)
	mappedModel := canonicalOpenAIAccountSchedulingModel(accountView, model)
	return openAIAdaptiveResultRouteKey(platform, mappedModel) + "|pool:" + poolKey
}

// Borrowed scheduler snapshots are immutable. Account helpers keep request-hot
// parsing caches on the value, so adaptive checks use a value copy before
// calling any helper that may populate those caches.
func openAIAdaptiveImmutableAccountView(account *Account) *Account {
	if account == nil {
		return nil
	}
	view := *account
	return &view
}

func openAIAdaptiveOwnedAccount(account *Account) *Account {
	owned := openAIAdaptiveImmutableAccountView(account)
	if owned == nil {
		return nil
	}
	owned.Credentials = cloneOpenAIAdaptiveMap(account.Credentials)
	owned.Extra = cloneOpenAIAdaptiveMap(account.Extra)
	owned.GroupIDs = append([]int64(nil), account.GroupIDs...)
	owned.AccountGroups = append([]AccountGroup(nil), account.AccountGroups...)
	owned.Groups = append([]*Group(nil), account.Groups...)
	return owned
}

func cloneOpenAIAdaptiveMap(source map[string]any) map[string]any {
	if source == nil {
		return nil
	}
	cloned := make(map[string]any, len(source))
	for key, value := range source {
		cloned[key] = cloneOpenAIAdaptiveValue(value)
	}
	return cloned
}

func cloneOpenAIAdaptiveValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneOpenAIAdaptiveMap(typed)
	case map[string]string:
		cloned := make(map[string]string, len(typed))
		for key, item := range typed {
			cloned[key] = item
		}
		return cloned
	case []any:
		cloned := make([]any, len(typed))
		for index, item := range typed {
			cloned[index] = cloneOpenAIAdaptiveValue(item)
		}
		return cloned
	case []string:
		return append([]string(nil), typed...)
	case []int64:
		return append([]int64(nil), typed...)
	default:
		return value
	}
}

func openAIAdaptiveResultRouteKey(platform, model string) string {
	return NormalizeOpenAICompatiblePlatform(platform) + "|" + strings.TrimSpace(model)
}

func openAIAdaptiveStormRouteKey(routeKey string) string {
	stormKey, _, _ := strings.Cut(routeKey, "|pool:")
	return stormKey
}

type openAIAdaptiveRuntimeShard struct {
	mu       sync.Mutex
	accounts map[int64]*openAIAdaptiveAccountState
}

type openAIAdaptiveRouteShard struct {
	mu      sync.Mutex
	routes  map[string]*openAIAdaptiveRouteState
	notify  chan int64
	waiters atomic.Int64
}

type openAIAdaptiveRouteState struct {
	buckets       []openAIAdaptiveRouteBucket
	storm         bool
	recoverySince time.Time
	lastTouchedAt time.Time
}

type openAIAdaptiveRouteBucket struct {
	second   int64
	attempts int
	limited  int
}

type openAIAdaptiveAccountState struct {
	window              int
	inflight            int
	increaseCredit      float64
	consecutive429      int
	cooldownUntil       time.Time
	redisFullUntil      time.Time
	halfOpenInflight    bool
	last429At           time.Time
	lastIncreaseAt      time.Time
	lastSelectedAt      time.Time
	latencyEWMA         float64
	errorRateEWMA       float64
	lastTouchedAt       time.Time
	rateLimitGeneration uint64
	halfOpenPermitID    uint64
}

type openAIAdaptiveAccountSnapshot struct {
	window         int
	inflight       int
	utilization    float64
	halfOpen       bool
	cooldownUntil  time.Time
	lastSelectedAt time.Time
	latencyEWMA    float64
	errorRateEWMA  float64
	reservable     bool
}

type openAIAdaptivePermit struct {
	runtime             *openAIAdaptiveRuntime
	accountID           int64
	hardLimit           int
	routeKey            string
	rateLimitGeneration uint64
	permitID            uint64
	halfOpen            bool
	releaseOnce         sync.Once
	resultOnce          sync.Once
	outcomeReported     atomic.Bool
}

func newOpenAIAdaptiveRuntime(config openAIAdaptiveSchedulerConfig) *openAIAdaptiveRuntime {
	if config.minWindow <= 0 {
		config.minWindow = 1
	}
	if config.initialWindow < config.minWindow {
		config.initialWindow = config.minWindow
	}
	if config.maxWindow < config.initialWindow {
		config.maxWindow = config.initialWindow
	}
	if config.increaseInterval <= 0 {
		config.increaseInterval = 2 * time.Second
	}
	if config.no429IncreaseWindow <= 0 {
		config.no429IncreaseWindow = 30 * time.Second
	}
	if config.sampleSize <= 0 {
		config.sampleSize = 4
	}
	if config.sampleRounds <= 0 {
		config.sampleRounds = 2
	}
	if config.waitTimeout <= 0 {
		config.waitTimeout = time.Second
	}
	if config.stickyEscape <= 0 || config.stickyEscape > 1 {
		config.stickyEscape = 0.8
	}
	if config.maxWaiters <= 0 {
		config.maxWaiters = 1000
	}
	if config.stormWindow <= 0 {
		config.stormWindow = 5 * time.Second
	}
	if config.stormMinAttempts <= 0 {
		config.stormMinAttempts = 20
	}
	if config.storm429Ratio <= 0 || config.storm429Ratio > 1 {
		config.storm429Ratio = 0.20
	}
	if config.stormRecoveryRatio < 0 || config.stormRecoveryRatio >= config.storm429Ratio {
		config.stormRecoveryRatio = 0.05
	}
	runtime := &openAIAdaptiveRuntime{config: config}
	for i := range runtime.shards {
		runtime.shards[i].accounts = make(map[int64]*openAIAdaptiveAccountState)
		runtime.routes[i].routes = make(map[string]*openAIAdaptiveRouteState)
		runtime.routes[i].notify = make(chan int64, config.maxWaiters)
	}
	return runtime
}

func (r *openAIAdaptiveRuntime) shard(accountID int64) *openAIAdaptiveRuntimeShard {
	return &r.shards[uint64(accountID)&(openAIAdaptiveRuntimeShardCount-1)]
}

func (r *openAIAdaptiveRuntime) routeShard(routeKey string) *openAIAdaptiveRouteShard {
	hasher := fnv.New64a()
	_, _ = hasher.Write([]byte(routeKey))
	return &r.routes[hasher.Sum64()&(openAIAdaptiveRuntimeShardCount-1)]
}

func (r *openAIAdaptiveRuntime) accountStateLocked(shard *openAIAdaptiveRuntimeShard, accountID int64, hardLimit int, now time.Time) *openAIAdaptiveAccountState {
	state := shard.accounts[accountID]
	if state != nil {
		state.lastTouchedAt = now
		if state.window > r.effectiveMaxWindow(hardLimit) {
			state.window = r.effectiveMaxWindow(hardLimit)
		}
		return state
	}
	if len(shard.accounts) >= openAIAdaptiveMaxAccountStatesPerShard {
		r.evictOldestIdleAccountStateLocked(shard, now)
		if len(shard.accounts) >= openAIAdaptiveMaxAccountStatesPerShard {
			return nil
		}
	}
	state = &openAIAdaptiveAccountState{
		window:         r.initialWindow(hardLimit),
		lastIncreaseAt: now,
		lastTouchedAt:  now,
	}
	shard.accounts[accountID] = state
	return state
}

func (r *openAIAdaptiveRuntime) evictOldestIdleAccountStateLocked(shard *openAIAdaptiveRuntimeShard, now time.Time) {
	var oldestID int64
	var oldestAt time.Time
	for accountID, state := range shard.accounts {
		if state == nil || state.inflight > 0 || state.halfOpenInflight || state.cooldownUntil.After(now) {
			continue
		}
		if oldestID == 0 || state.lastTouchedAt.Before(oldestAt) {
			oldestID = accountID
			oldestAt = state.lastTouchedAt
		}
	}
	if oldestID != 0 {
		delete(shard.accounts, oldestID)
	}
}

func (r *openAIAdaptiveRuntime) maybePrune(now time.Time) {
	if r == nil {
		return
	}
	cutoff := now.Add(-openAIAdaptiveStatePruneInterval).UnixNano()
	previous := r.lastPruneAt.Load()
	if previous > cutoff || !r.lastPruneAt.CompareAndSwap(previous, now.UnixNano()) {
		return
	}
	r.prune(now)
}

func (r *openAIAdaptiveRuntime) prune(now time.Time) {
	if r == nil {
		return
	}
	accountCutoff := now.Add(-openAIAdaptiveAccountStateTTL)
	for i := range r.shards {
		shard := &r.shards[i]
		shard.mu.Lock()
		for accountID, state := range shard.accounts {
			if state == nil || (state.inflight == 0 && !state.halfOpenInflight && !state.cooldownUntil.After(now) && state.lastTouchedAt.Before(accountCutoff)) {
				delete(shard.accounts, accountID)
			}
		}
		shard.mu.Unlock()
	}
	routeCutoff := now.Add(-openAIAdaptiveRouteStateTTL)
	for i := range r.routes {
		shard := &r.routes[i]
		shard.mu.Lock()
		for routeKey, state := range shard.routes {
			if state == nil || state.lastTouchedAt.Before(routeCutoff) {
				delete(shard.routes, routeKey)
			}
		}
		shard.mu.Unlock()
	}
}

func (r *openAIAdaptiveRuntime) initialWindow(hardLimit int) int {
	window := r.config.initialWindow
	if maxWindow := r.effectiveMaxWindow(hardLimit); window > maxWindow {
		window = maxWindow
	}
	if window < 1 {
		return 1
	}
	return window
}

func (r *openAIAdaptiveRuntime) effectiveMaxWindow(hardLimit int) int {
	maxWindow := r.config.maxWindow
	if maxWindow <= 0 {
		maxWindow = 32
	}
	if hardLimit > 0 && hardLimit < maxWindow {
		maxWindow = hardLimit
	}
	if maxWindow < 1 {
		return 1
	}
	return maxWindow
}

func (r *openAIAdaptiveRuntime) tryReserve(accountID int64, hardLimit int, routeKey string, now time.Time) (*openAIAdaptivePermit, bool) {
	if r == nil || accountID <= 0 {
		return nil, false
	}
	r.maybePrune(now)
	shard := r.shard(accountID)
	shard.mu.Lock()
	defer shard.mu.Unlock()
	state := r.accountStateLocked(shard, accountID, hardLimit, now)
	if state == nil {
		return nil, false
	}
	if now.Before(state.redisFullUntil) {
		return nil, false
	}

	halfOpen := state.consecutive429 > 0 && !now.Before(state.cooldownUntil)
	if state.consecutive429 > 0 && now.Before(state.cooldownUntil) {
		return nil, false
	}
	if halfOpen {
		if state.halfOpenInflight {
			return nil, false
		}
		state.halfOpenInflight = true
	} else if state.inflight >= r.routeWindow(state.window, routeKey) {
		return nil, false
	}
	state.inflight++
	state.lastSelectedAt = now
	permitID := r.permitSeq.Add(1)
	if halfOpen {
		state.halfOpenPermitID = permitID
	}
	return &openAIAdaptivePermit{
		runtime:             r,
		accountID:           accountID,
		hardLimit:           hardLimit,
		routeKey:            routeKey,
		rateLimitGeneration: state.rateLimitGeneration,
		permitID:            permitID,
		halfOpen:            halfOpen,
	}, true
}

func (r *openAIAdaptiveRuntime) routeWindow(window int, routeKey string) int {
	if !r.isRouteStorm(openAIAdaptiveStormRouteKey(routeKey)) {
		return window
	}
	contracted := int(math.Floor(float64(window) * 0.75))
	return maxInt(r.config.minWindow, contracted)
}

func (r *openAIAdaptiveRuntime) reportRouteAttempt(routeKey string, limited bool, now time.Time) {
	if r == nil || strings.TrimSpace(routeKey) == "" {
		return
	}
	r.maybePrune(now)
	shard := r.routeShard(routeKey)
	shard.mu.Lock()
	defer shard.mu.Unlock()
	state := shard.routes[routeKey]
	if state == nil {
		if len(shard.routes) >= openAIAdaptiveMaxRouteStatesPerShard {
			var oldestKey string
			var oldestAt time.Time
			for candidateKey, candidate := range shard.routes {
				if candidate == nil || oldestKey == "" || candidate.lastTouchedAt.Before(oldestAt) {
					oldestKey = candidateKey
					if candidate != nil {
						oldestAt = candidate.lastTouchedAt
					}
				}
			}
			delete(shard.routes, oldestKey)
		}
		bucketCount := maxInt(1, int(math.Ceil(r.config.stormWindow.Seconds())))
		state = &openAIAdaptiveRouteState{buckets: make([]openAIAdaptiveRouteBucket, bucketCount)}
		shard.routes[routeKey] = state
	}
	state.lastTouchedAt = now
	second := now.Unix()
	index := int(second % int64(len(state.buckets)))
	if index < 0 {
		index += len(state.buckets)
	}
	bucket := &state.buckets[index]
	if bucket.second != second {
		*bucket = openAIAdaptiveRouteBucket{second: second}
	}
	bucket.attempts++
	if limited {
		bucket.limited++
	}

	attempts, limitedCount := 0, 0
	for i := range state.buckets {
		candidate := state.buckets[i]
		age := now.Sub(time.Unix(candidate.second, 0))
		if candidate.second == 0 || age < 0 || age >= r.config.stormWindow {
			continue
		}
		attempts += candidate.attempts
		limitedCount += candidate.limited
	}
	ratio := 0.0
	if attempts > 0 {
		ratio = float64(limitedCount) / float64(attempts)
	}
	if !state.storm {
		if attempts >= r.config.stormMinAttempts && ratio >= r.config.storm429Ratio {
			state.storm = true
			state.recoverySince = time.Time{}
		}
		return
	}
	if ratio < r.config.stormRecoveryRatio {
		if state.recoverySince.IsZero() {
			state.recoverySince = now
		} else if now.Sub(state.recoverySince) >= 10*time.Second {
			state.storm = false
			state.recoverySince = time.Time{}
		}
		return
	}
	state.recoverySince = time.Time{}
}

func (r *openAIAdaptiveRuntime) isRouteStorm(routeKey string) bool {
	if r == nil || strings.TrimSpace(routeKey) == "" {
		return false
	}
	shard := r.routeShard(routeKey)
	shard.mu.Lock()
	defer shard.mu.Unlock()
	state := shard.routes[routeKey]
	return state != nil && state.storm
}

func (p *openAIAdaptivePermit) Release() {
	if p == nil || p.runtime == nil {
		return
	}
	p.releaseOnce.Do(func() {
		p.runtime.release(p, time.Now())
	})
}

func (p *openAIAdaptivePermit) reportResult(success bool, now time.Time, latency time.Duration) {
	if p == nil || p.runtime == nil {
		return
	}
	p.resultOnce.Do(func() {
		p.outcomeReported.Store(true)
		p.runtime.reportPermitResult(p, success, now, latency)
	})
}

func (r *openAIAdaptiveRuntime) release(permit *openAIAdaptivePermit, now time.Time) {
	if r == nil || permit == nil {
		return
	}
	shard := r.shard(permit.accountID)
	shard.mu.Lock()
	if state := shard.accounts[permit.accountID]; state != nil {
		if state.inflight > 0 {
			state.inflight--
		}
		if permit.halfOpen && !permit.outcomeReported.Load() &&
			state.rateLimitGeneration == permit.rateLimitGeneration &&
			state.halfOpenPermitID == permit.permitID {
			state.halfOpenInflight = false
			fallbackUntil := now.Add(openAIAdaptiveAbandonedProbeCooldown)
			if fallbackUntil.After(state.cooldownUntil) {
				state.cooldownUntil = fallbackUntil
			}
		}
	}
	shard.mu.Unlock()
	r.signalCapacity(permit.routeKey, permit.accountID)
}

func (r *openAIAdaptiveRuntime) capacityNotifications(routeKey string) chan int64 {
	return r.routeShard(routeKey).notify
}

func (r *openAIAdaptiveRuntime) signalCapacity(routeKey string, accountID int64) {
	if r == nil || r.waiters.Load() <= 0 {
		return
	}
	for i := range r.routes {
		shard := &r.routes[i]
		if shard.waiters.Load() <= 0 {
			continue
		}
		select {
		case shard.notify <- accountID:
		default:
		}
	}
}

func (r *openAIAdaptiveRuntime) enterWaiter(routeKey string) bool {
	if r == nil {
		return false
	}
	limit := int64(r.config.maxWaiters)
	for {
		current := r.waiters.Load()
		if current >= limit {
			return false
		}
		if r.waiters.CompareAndSwap(current, current+1) {
			r.routeShard(routeKey).waiters.Add(1)
			return true
		}
	}
}

func (r *openAIAdaptiveRuntime) leaveWaiter(routeKey string) {
	if r != nil {
		r.routeShard(routeKey).waiters.Add(-1)
		r.waiters.Add(-1)
	}
}

func (r *openAIAdaptiveRuntime) waitForCapacity(ctx context.Context, wakeAt time.Time, routeKey string) (int64, bool) {
	if r == nil || !time.Now().Before(wakeAt) {
		return 0, false
	}
	timer := time.NewTimer(time.Until(wakeAt))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return 0, false
	case <-timer.C:
		return 0, false
	case accountID := <-r.capacityNotifications(routeKey):
		return accountID, true
	}
}

func (r *openAIAdaptiveRuntime) nextCandidateCooldown(accounts []*Account, excluded map[int64]struct{}, now, deadline time.Time) time.Time {
	next := deadline
	for _, account := range accounts {
		if account == nil || account.ID <= 0 {
			continue
		}
		if _, skip := excluded[account.ID]; skip {
			continue
		}
		shard := r.shard(account.ID)
		shard.mu.Lock()
		state := shard.accounts[account.ID]
		if state != nil {
			if state.cooldownUntil.After(now) && state.cooldownUntil.Before(next) {
				next = state.cooldownUntil
			}
			if state.redisFullUntil.After(now) && state.redisFullUntil.Before(next) {
				next = state.redisFullUntil
			}
		}
		shard.mu.Unlock()
	}
	return next
}

func findOpenAIAdaptiveAccount(accounts []*Account, accountID int64) *Account {
	for _, account := range accounts {
		if account != nil && account.ID == accountID {
			return account
		}
	}
	return nil
}

func (r *openAIAdaptiveRuntime) waitAndSelect(
	ctx context.Context,
	deadline time.Time,
	routeKey string,
	accounts []*Account,
	excluded map[int64]struct{},
	seed uint64,
) (*Account, *openAIAdaptivePermit, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if account, permit := r.selectCandidate(accounts, excluded, routeKey, time.Now(), seed); account != nil {
		return account, permit, nil
	}
	if !r.enterWaiter(routeKey) {
		return nil, nil, errOpenAIAdaptiveWaiterLimit
	}
	defer r.leaveWaiter(routeKey)
	// Close the gap between the first failed sample and waiter admission. A
	// release in that interval observes zero waiters and deliberately emits no
	// token, so capacity must be sampled once more before sleeping.
	seed += 0x9e3779b97f4a7c15
	if account, permit := r.selectCandidate(accounts, excluded, routeKey, time.Now(), seed); account != nil {
		return account, permit, nil
	}

	for {
		now := time.Now()
		wakeAt := r.nextCandidateCooldown(accounts, excluded, now, deadline)
		releasedAccountID, notified := r.waitForCapacity(ctx, wakeAt, routeKey)
		if !notified {
			if err := ctx.Err(); err != nil {
				return nil, nil, err
			}
			if !time.Now().Before(deadline) {
				return nil, nil, errOpenAIAdaptiveWaitTimeout
			}
			seed += 0x9e3779b97f4a7c15
			if account, permit := r.selectCandidate(accounts, excluded, routeKey, time.Now(), seed); account != nil {
				return account, permit, nil
			}
			continue
		}
		if _, skip := excluded[releasedAccountID]; !skip {
			if account := findOpenAIAdaptiveAccount(accounts, releasedAccountID); account != nil {
				candidateRouteKey := openAIAdaptiveCandidateRouteKey(routeKey, account)
				if permit, ok := r.tryReserve(account.ID, account.Concurrency, candidateRouteKey, time.Now()); ok {
					return account, permit, nil
				}
			}
		}
		seed += 0x9e3779b97f4a7c15
		if account, permit := r.selectCandidate(accounts, excluded, routeKey, time.Now(), seed); account != nil {
			return account, permit, nil
		}
	}
}

func (r *openAIAdaptiveRuntime) snapshot(accountID int64, hardLimit int, now time.Time) openAIAdaptiveAccountSnapshot {
	if r == nil || accountID <= 0 {
		return openAIAdaptiveAccountSnapshot{}
	}
	shard := r.shard(accountID)
	shard.mu.Lock()
	defer shard.mu.Unlock()
	state := r.accountStateLocked(shard, accountID, hardLimit, now)
	if state == nil {
		return openAIAdaptiveAccountSnapshot{}
	}
	utilization := float64(state.inflight) / float64(maxInt(state.window, 1))
	return openAIAdaptiveAccountSnapshot{
		window:         state.window,
		inflight:       state.inflight,
		utilization:    utilization,
		halfOpen:       state.consecutive429 > 0 && !now.Before(state.cooldownUntil),
		cooldownUntil:  state.cooldownUntil,
		lastSelectedAt: state.lastSelectedAt,
		latencyEWMA:    state.latencyEWMA,
		errorRateEWMA:  state.errorRateEWMA,
		reservable:     !now.Before(state.redisFullUntil) && ((!now.Before(state.cooldownUntil) && state.consecutive429 > 0 && !state.halfOpenInflight) || (state.consecutive429 == 0 && state.inflight < state.window)),
	}
}

func (r *openAIAdaptiveRuntime) reportRedisFull(accountID int64, hardLimit int, now time.Time) time.Time {
	if r == nil || accountID <= 0 {
		return time.Time{}
	}
	r.maybePrune(now)
	shard := r.shard(accountID)
	shard.mu.Lock()
	state := r.accountStateLocked(shard, accountID, hardLimit, now)
	if state == nil {
		shard.mu.Unlock()
		return time.Time{}
	}
	until := now.Add(openAIAdaptiveRedisFullCooldown)
	if until.After(state.redisFullUntil) {
		state.redisFullUntil = until
	}
	until = state.redisFullUntil
	shard.mu.Unlock()
	return until
}

type openAIAdaptiveCandidate struct {
	account  *Account
	snapshot openAIAdaptiveAccountSnapshot
	tie      uint64
}

func (r *openAIAdaptiveRuntime) selectCandidate(
	accounts []*Account,
	excluded map[int64]struct{},
	routeKey string,
	now time.Time,
	seed uint64,
) (*Account, *openAIAdaptivePermit) {
	if r == nil || len(accounts) == 0 {
		return nil, nil
	}
	rng := newOpenAISelectionRNG(seed)
	seen := make(map[int]struct{}, minInt(len(accounts), r.config.sampleSize*r.config.sampleRounds))
	for round := 0; round < r.config.sampleRounds; round++ {
		indexes := openAIAdaptiveSampleIndexes(len(accounts), r.config.sampleSize, seen, &rng)
		if len(indexes) == 0 {
			break
		}
		sampled := make([]openAIAdaptiveCandidate, 0, len(indexes))
		for _, index := range indexes {
			account := accounts[index]
			if account == nil || account.ID <= 0 {
				continue
			}
			if _, skip := excluded[account.ID]; skip {
				continue
			}
			sampled = append(sampled, openAIAdaptiveCandidate{
				account:  account,
				snapshot: r.snapshot(account.ID, account.Concurrency, now),
				tie:      rng.nextUint64(),
			})
		}
		sort.Slice(sampled, func(i, j int) bool {
			return isOpenAIAdaptiveCandidateBetter(sampled[i], sampled[j])
		})
		for _, candidate := range sampled {
			if !candidate.snapshot.reservable {
				continue
			}
			candidateRouteKey := openAIAdaptiveCandidateRouteKey(routeKey, candidate.account)
			permit, ok := r.tryReserve(candidate.account.ID, candidate.account.Concurrency, candidateRouteKey, now)
			if ok {
				return candidate.account, permit
			}
		}
	}
	return nil, nil
}

func isOpenAIAdaptiveCandidateBetter(left, right openAIAdaptiveCandidate) bool {
	if left.snapshot.reservable != right.snapshot.reservable {
		return left.snapshot.reservable
	}
	if left.snapshot.utilization != right.snapshot.utilization {
		return left.snapshot.utilization < right.snapshot.utilization
	}
	if left.snapshot.inflight != right.snapshot.inflight {
		return left.snapshot.inflight < right.snapshot.inflight
	}
	if left.snapshot.errorRateEWMA != right.snapshot.errorRateEWMA {
		return left.snapshot.errorRateEWMA < right.snapshot.errorRateEWMA
	}
	if left.snapshot.latencyEWMA != right.snapshot.latencyEWMA {
		return left.snapshot.latencyEWMA < right.snapshot.latencyEWMA
	}
	if !left.snapshot.lastSelectedAt.Equal(right.snapshot.lastSelectedAt) {
		return left.snapshot.lastSelectedAt.Before(right.snapshot.lastSelectedAt)
	}
	return left.tie < right.tie
}

func openAIAdaptiveSampleIndexes(total int, size int, seen map[int]struct{}, rng *openAISelectionRNG) []int {
	if total <= 0 || size <= 0 || rng == nil {
		return nil
	}
	remaining := total - len(seen)
	if remaining <= 0 {
		return nil
	}
	if size > remaining {
		size = remaining
	}
	indexes := make([]int, 0, size)
	maxRandomAttempts := size*8 + 16
	for attempts := 0; len(indexes) < size && attempts < maxRandomAttempts; attempts++ {
		index := int(rng.nextUint64() % uint64(total))
		if _, exists := seen[index]; exists {
			continue
		}
		seen[index] = struct{}{}
		indexes = append(indexes, index)
	}
	if len(indexes) < size {
		start := int(rng.nextUint64() % uint64(total))
		for offset := 0; offset < total && len(indexes) < size; offset++ {
			index := (start + offset) % total
			if _, exists := seen[index]; exists {
				continue
			}
			seen[index] = struct{}{}
			indexes = append(indexes, index)
		}
	}
	return indexes
}

func (r *openAIAdaptiveRuntime) reportSuccess(accountID int64, hardLimit int, now time.Time, latency time.Duration) {
	if r == nil || accountID <= 0 {
		return
	}
	r.maybePrune(now)
	shard := r.shard(accountID)
	shard.mu.Lock()
	state := r.accountStateLocked(shard, accountID, hardLimit, now)
	if state != nil {
		r.applySuccessLocked(state, hardLimit, now, latency)
	}
	shard.mu.Unlock()
}

func (r *openAIAdaptiveRuntime) applySuccessLocked(state *openAIAdaptiveAccountState, hardLimit int, now time.Time, latency time.Duration) {
	if state == nil {
		return
	}
	if state.consecutive429 > 0 {
		state.consecutive429 = 0
		state.cooldownUntil = time.Time{}
		state.halfOpenInflight = false
		state.halfOpenPermitID = 0
		state.increaseCredit = 0
		state.window = minInt(maxInt(2, r.config.minWindow), r.effectiveMaxWindow(hardLimit))
		state.lastIncreaseAt = now
	} else {
		state.increaseCredit += 1 / float64(maxInt(state.window, 1))
		quietEnough := state.last429At.IsZero() || now.Sub(state.last429At) >= r.config.no429IncreaseWindow
		if state.increaseCredit >= 1 && quietEnough && now.Sub(state.lastIncreaseAt) >= r.config.increaseInterval && state.window < r.effectiveMaxWindow(hardLimit) {
			state.window++
			state.increaseCredit--
			state.lastIncreaseAt = now
		}
	}
	state.errorRateEWMA *= 0.8
	if latency > 0 {
		sample := float64(latency.Milliseconds())
		if state.latencyEWMA <= 0 {
			state.latencyEWMA = sample
		} else {
			state.latencyEWMA = 0.2*sample + 0.8*state.latencyEWMA
		}
	}
}

func (r *openAIAdaptiveRuntime) reportFailure(accountID int64, hardLimit int, now time.Time) {
	if r == nil || accountID <= 0 {
		return
	}
	r.maybePrune(now)
	shard := r.shard(accountID)
	shard.mu.Lock()
	state := r.accountStateLocked(shard, accountID, hardLimit, now)
	if state != nil {
		r.applyFailureLocked(state, now)
	}
	shard.mu.Unlock()
}

func (r *openAIAdaptiveRuntime) applyFailureLocked(state *openAIAdaptiveAccountState, now time.Time) {
	if state == nil {
		return
	}
	if state.consecutive429 > 0 && !now.Before(state.cooldownUntil) {
		retryAt := now.Add(openAIAdaptive429Cooldown(state.consecutive429))
		if retryAt.After(state.cooldownUntil) {
			state.cooldownUntil = retryAt
		}
		state.halfOpenInflight = false
		state.halfOpenPermitID = 0
	}
	state.errorRateEWMA = 0.2 + 0.8*state.errorRateEWMA

}

func (r *openAIAdaptiveRuntime) reportPermitResult(permit *openAIAdaptivePermit, success bool, now time.Time, latency time.Duration) {
	if r == nil || permit == nil || permit.accountID <= 0 {
		return
	}
	r.maybePrune(now)
	shard := r.shard(permit.accountID)
	shard.mu.Lock()
	state := r.accountStateLocked(shard, permit.accountID, permit.hardLimit, now)
	if state == nil {
		shard.mu.Unlock()
		return
	}
	if state.rateLimitGeneration != permit.rateLimitGeneration ||
		(permit.halfOpen && state.halfOpenPermitID != permit.permitID) ||
		(!permit.halfOpen && state.consecutive429 > 0) {
		shard.mu.Unlock()
		return
	}
	if success {
		r.applySuccessLocked(state, permit.hardLimit, now, latency)
	} else if permit.halfOpen && state.consecutive429 > 0 {
		retryAt := now.Add(openAIAdaptive429Cooldown(state.consecutive429))
		if retryAt.After(state.cooldownUntil) {
			state.cooldownUntil = retryAt
		}
		state.halfOpenInflight = false
		state.halfOpenPermitID = 0
		state.errorRateEWMA = 0.2 + 0.8*state.errorRateEWMA
	} else {
		r.applyFailureLocked(state, now)
	}
	shard.mu.Unlock()
}

func (r *openAIAdaptiveRuntime) report429(accountID int64, hardLimit int, now time.Time, resetAt time.Time) time.Time {
	if r == nil || accountID <= 0 {
		return time.Time{}
	}
	r.maybePrune(now)
	shard := r.shard(accountID)
	shard.mu.Lock()
	state := r.accountStateLocked(shard, accountID, hardLimit, now)
	if state == nil {
		shard.mu.Unlock()
		return time.Time{}
	}
	state.rateLimitGeneration++
	state.consecutive429++
	state.last429At = now
	state.halfOpenInflight = false
	state.halfOpenPermitID = 0
	state.increaseCredit = 0
	if state.consecutive429 == 1 {
		state.window = maxInt(r.config.minWindow, int(math.Ceil(float64(state.window)/2)))
	} else {
		state.window = maxInt(1, r.config.minWindow)
	}
	fallback := openAIAdaptive429Cooldown(state.consecutive429)
	state.cooldownUntil = now.Add(fallback)
	if resetAt.After(state.cooldownUntil) {
		state.cooldownUntil = resetAt
	}
	state.errorRateEWMA = 0.2 + 0.8*state.errorRateEWMA
	until := state.cooldownUntil
	shard.mu.Unlock()
	return until
}

func openAIAdaptive429Cooldown(consecutive int) time.Duration {
	switch consecutive {
	case 1:
		return 2 * time.Second
	case 2:
		return 5 * time.Second
	case 3:
		return 15 * time.Second
	default:
		return 30 * time.Second
	}
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}
