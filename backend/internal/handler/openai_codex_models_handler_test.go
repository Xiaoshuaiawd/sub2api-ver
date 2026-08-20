package handler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type codexModelsFailoverAccountRepo struct {
	service.AccountRepository
	accounts []service.Account
}

type codexModelsAdaptiveAccountRepo struct {
	service.AccountRepository
	getByIDCalls     atomic.Int64
	schedulableCalls atomic.Int64
}

func (r *codexModelsAdaptiveAccountRepo) GetByID(context.Context, int64) (*service.Account, error) {
	r.getByIDCalls.Add(1)
	return nil, service.ErrNoAvailableAccounts
}

func (r *codexModelsAdaptiveAccountRepo) ListSchedulableByPlatform(context.Context, string) ([]service.Account, error) {
	r.schedulableCalls.Add(1)
	return nil, service.ErrNoAvailableAccounts
}

func (r *codexModelsAdaptiveAccountRepo) ListSchedulableByGroupIDAndPlatform(context.Context, int64, string) ([]service.Account, error) {
	r.schedulableCalls.Add(1)
	return nil, service.ErrNoAvailableAccounts
}

func (r *codexModelsAdaptiveAccountRepo) ListSchedulableUngroupedByPlatform(context.Context, string) ([]service.Account, error) {
	r.schedulableCalls.Add(1)
	return nil, service.ErrNoAvailableAccounts
}

type codexModelsAdaptiveSchedulerCache struct {
	service.SchedulerCache
	accounts      []*service.Account
	snapshotCalls atomic.Int64
	accountCalls  atomic.Int64
}

func (c *codexModelsAdaptiveSchedulerCache) GetSnapshot(context.Context, service.SchedulerBucket) ([]*service.Account, bool, error) {
	c.snapshotCalls.Add(1)
	return c.accounts, true, nil
}

func (c *codexModelsAdaptiveSchedulerCache) GetAccount(_ context.Context, accountID int64) (*service.Account, error) {
	c.accountCalls.Add(1)
	for _, account := range c.accounts {
		if account != nil && account.ID == accountID {
			clone := *account
			return &clone, nil
		}
	}
	return nil, service.ErrNoAvailableAccounts
}

type codexModelsAdaptiveConcurrencyCache struct {
	service.ConcurrencyCache
	loadCalls    atomic.Int64
	acquireCalls atomic.Int64
	releaseCalls atomic.Int64
}

func (c *codexModelsAdaptiveConcurrencyCache) GetAccountsLoadBatch(context.Context, []service.AccountWithConcurrency) (map[int64]*service.AccountLoadInfo, error) {
	c.loadCalls.Add(1)
	return nil, nil
}

func (c *codexModelsAdaptiveConcurrencyCache) AcquireAccountSlot(context.Context, int64, int, string) (bool, error) {
	c.acquireCalls.Add(1)
	return true, nil
}

func (c *codexModelsAdaptiveConcurrencyCache) ReleaseAccountSlot(context.Context, int64, string) error {
	c.releaseCalls.Add(1)
	return nil
}

func (r codexModelsFailoverAccountRepo) GetByID(_ context.Context, id int64) (*service.Account, error) {
	for i := range r.accounts {
		if r.accounts[i].ID == id {
			account := r.accounts[i]
			return &account, nil
		}
	}
	return nil, service.ErrNoAvailableAccounts
}

func (r codexModelsFailoverAccountRepo) ListSchedulableByPlatform(_ context.Context, platform string) ([]service.Account, error) {
	accounts := make([]service.Account, 0, len(r.accounts))
	for _, account := range r.accounts {
		if account.Platform == platform {
			accounts = append(accounts, account)
		}
	}
	return accounts, nil
}

type codexModelsFailoverHTTPUpstream struct {
	service.HTTPUpstream
	mu          sync.Mutex
	accountIDs  []int64
	firstErr    error
	firstStatus int
	firstBody   string
	statuses    map[int64]int
	onDo        func(accountID int64)
}

func (u *codexModelsFailoverHTTPUpstream) Do(_ *http.Request, _ string, accountID int64, _ int) (*http.Response, error) {
	u.mu.Lock()
	u.accountIDs = append(u.accountIDs, accountID)
	u.mu.Unlock()
	if u.onDo != nil {
		u.onDo(accountID)
	}

	status, hasStatus := u.statuses[accountID]
	if accountID == 1 || hasStatus {
		if u.firstErr != nil {
			return nil, u.firstErr
		}
		if u.firstBody != "" && !hasStatus {
			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(u.firstBody)),
			}, nil
		}
		if !hasStatus {
			status = u.firstStatus
		}
		if status == 0 {
			status = http.StatusServiceUnavailable
		}
		return &http.Response{
			StatusCode: status,
			Status:     http.StatusText(status),
			Header:     make(http.Header),
			Body: io.NopCloser(strings.NewReader(
				`{"error":{"message":"No available OpenAI accounts","type":"upstream_error"}}`,
			)),
		}, nil
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(`{"models":[{"slug":"gpt-5.6-sol"}]}`)),
	}, nil
}

func (u *codexModelsFailoverHTTPUpstream) calls() []int64 {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]int64(nil), u.accountIDs...)
}

func TestCodexModelsCanceledRequestDoesNotWriteResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil).WithContext(ctx)

	h := &OpenAIGatewayHandler{}
	h.CodexModels(c)

	if c.Writer.Written() {
		t.Fatalf("canceled request wrote an HTTP response: status=%d body=%q", recorder.Code, recorder.Body.String())
	}
}

func TestCodexModelsFailsOverFromRetryableUpstreamStatus(t *testing.T) {
	retryableStatuses := []int{
		http.StatusTooManyRequests,
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout,
	}
	for _, status := range retryableStatuses {
		t.Run(http.StatusText(status), func(t *testing.T) {
			handler, upstream, groupID := newCodexModelsFailoverTestHandler(status)
			recorder := performCodexModelsRequest(t, handler, groupID)

			if got, want := upstream.calls(), []int64{1, 2}; !equalInt64Slices(got, want) {
				t.Fatalf("upstream account calls: got %v, want %v", got, want)
			}
			if recorder.Code != http.StatusOK {
				t.Fatalf("status: got %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
			}
			if got, want := recorder.Body.String(), `{"models":[{"slug":"gpt-5.6-sol"}]}`; got != want {
				t.Fatalf("body: got %q, want %q", got, want)
			}
		})
	}
}

func TestCodexModelsFailsOverFromUpstreamTransportError(t *testing.T) {
	handler, upstream, groupID := newCodexModelsFailoverTestHandler(http.StatusServiceUnavailable)
	upstream.firstErr = &net.OpError{
		Op:  "read",
		Net: "tcp",
		Err: errors.New("connection reset"),
	}
	recorder := performCodexModelsRequest(t, handler, groupID)

	if got, want := upstream.calls(), []int64{1, 2}; !equalInt64Slices(got, want) {
		t.Fatalf("upstream account calls: got %v, want %v", got, want)
	}
	if recorder.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
}

func TestCodexModelsFailsOverFromInvalidManifestEnvelope(t *testing.T) {
	handler, upstream, groupID := newCodexModelsFailoverTestHandler(http.StatusOK)
	upstream.firstBody = `{"object":"list","data":[]}`
	recorder := performCodexModelsRequest(t, handler, groupID)

	if got, want := upstream.calls(), []int64{1, 2}; !equalInt64Slices(got, want) {
		t.Fatalf("upstream account calls: got %v, want %v", got, want)
	}
	if recorder.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if got, want := recorder.Body.String(), `{"models":[{"slug":"gpt-5.6-sol"}]}`; got != want {
		t.Fatalf("body: got %q, want %q", got, want)
	}
}

func TestCodexModelsDoesNotFailOverFromPermanentUpstreamStatus(t *testing.T) {
	statuses := []int{
		http.StatusBadRequest,
		http.StatusUnauthorized,
		http.StatusForbidden,
		http.StatusNotFound,
		600,
	}
	for _, status := range statuses {
		t.Run(fmt.Sprintf("status_%d", status), func(t *testing.T) {
			handler, upstream, groupID := newCodexModelsFailoverTestHandler(status)
			recorder := performCodexModelsRequest(t, handler, groupID)

			if got, want := upstream.calls(), []int64{1}; !equalInt64Slices(got, want) {
				t.Fatalf("upstream account calls: got %v, want %v", got, want)
			}
			if recorder.Code != http.StatusBadGateway {
				t.Fatalf("status: got %d, want %d; body=%s", recorder.Code, http.StatusBadGateway, recorder.Body.String())
			}
		})
	}
}

func TestCodexModelsDoesNotFailOverFromUpstreamConfigurationError(t *testing.T) {
	handler, upstream, groupID := newCodexModelsFailoverTestHandler(http.StatusServiceUnavailable)
	upstream.firstErr = errors.New("invalid proxy URL")
	recorder := performCodexModelsRequest(t, handler, groupID)

	if got, want := upstream.calls(), []int64{1}; !equalInt64Slices(got, want) {
		t.Fatalf("upstream account calls: got %v, want %v", got, want)
	}
	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status: got %d, want %d; body=%s", recorder.Code, http.StatusBadGateway, recorder.Body.String())
	}
}

func TestCodexModelsReturnsLastUpstreamErrorWhenAccountsAreExhausted(t *testing.T) {
	handler, upstream, groupID := newCodexModelsFailoverTestHandler(http.StatusServiceUnavailable)
	upstream.statuses = map[int64]int{
		1: http.StatusServiceUnavailable,
		2: http.StatusGatewayTimeout,
	}
	recorder := performCodexModelsRequest(t, handler, groupID)

	if got, want := upstream.calls(), []int64{1, 2}; !equalInt64Slices(got, want) {
		t.Fatalf("upstream account calls: got %v, want %v", got, want)
	}
	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status: got %d, want %d; body=%s", recorder.Code, http.StatusBadGateway, recorder.Body.String())
	}
	if body := recorder.Body.String(); !strings.Contains(body, "upstream error 504") {
		t.Fatalf("body does not preserve the last upstream error: %s", body)
	}
}

func TestCodexModelsHonorsAccountSwitchLimit(t *testing.T) {
	handler, upstream, groupID := newCodexModelsFailoverTestHandlerWithAccountCount(http.StatusServiceUnavailable, 4, 2)
	upstream.statuses = map[int64]int{
		1: http.StatusServiceUnavailable,
		2: http.StatusBadGateway,
		3: http.StatusGatewayTimeout,
		4: http.StatusInternalServerError,
	}
	recorder := performCodexModelsRequest(t, handler, groupID)

	if got, want := upstream.calls(), []int64{1, 2, 3}; !equalInt64Slices(got, want) {
		t.Fatalf("upstream account calls: got %v, want %v", got, want)
	}
	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status: got %d, want %d; body=%s", recorder.Code, http.StatusBadGateway, recorder.Body.String())
	}
	if body := recorder.Body.String(); !strings.Contains(body, "upstream error 504") {
		t.Fatalf("body does not preserve the limit-ending upstream error: %s", body)
	}
}

func TestCodexModelsAdaptiveBudgetNeverCallsThirdDistinctAccount(t *testing.T) {
	handler, upstream, groupID := newCodexModelsFailoverTestHandlerWithAccountCount(http.StatusServiceUnavailable, 4, 3)
	handler.openAIFailoverBudget = 800 * time.Millisecond
	handler.openAIMaxDistinctAccounts = 2
	upstream.statuses = map[int64]int{
		1: http.StatusServiceUnavailable,
		2: http.StatusBadGateway,
		3: http.StatusGatewayTimeout,
		4: http.StatusInternalServerError,
	}

	recorder := performCodexModelsRequest(t, handler, groupID)

	if got, want := upstream.calls(), []int64{1, 2}; !equalInt64Slices(got, want) {
		t.Fatalf("upstream account calls: got %v, want %v", got, want)
	}
	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status: got %d, want %d; body=%s", recorder.Code, http.StatusBadGateway, recorder.Body.String())
	}
}

func TestCodexModelsAdaptiveUsesCacheOnlySchedulerAndReleasesSlot(t *testing.T) {
	gin.SetMode(gin.TestMode)
	groupID := int64(42)
	group := &service.Group{
		ID:       groupID,
		Name:     "adaptive-codex-models",
		Platform: service.PlatformOpenAI,
		Status:   service.StatusActive,
		Hydrated: true,
	}
	account := &service.Account{
		ID:          7001,
		Name:        "adaptive-upstream",
		Platform:    service.PlatformOpenAI,
		Type:        service.AccountTypeAPIKey,
		Status:      service.StatusActive,
		Schedulable: true,
		Concurrency: 8,
		Credentials: map[string]any{
			"api_key":  "sk-adaptive",
			"base_url": "https://adaptive-upstream.example/v1",
		},
	}
	repo := &codexModelsAdaptiveAccountRepo{}
	snapshotCache := &codexModelsAdaptiveSchedulerCache{accounts: []*service.Account{account}}
	concurrencyCache := &codexModelsAdaptiveConcurrencyCache{}
	upstream := &codexModelsFailoverHTTPUpstream{}
	var releasesObservedByUpstream atomic.Int64
	releasesObservedByUpstream.Store(-1)
	upstream.onDo = func(int64) {
		releasesObservedByUpstream.Store(concurrencyCache.releaseCalls.Load())
	}
	cfg := &config.Config{RunMode: config.RunModeSimple}
	cfg.Gateway.OpenAIScheduler.AdaptiveEnabled = true
	cfg.Gateway.OpenAIScheduler.ShadowMode = false
	cfg.Gateway.OpenAIScheduler.InitialWindow = 2
	cfg.Gateway.OpenAIScheduler.MinWindow = 1
	cfg.Gateway.OpenAIScheduler.MaxWindow = 32
	cfg.Gateway.OpenAIScheduler.SampleSize = 4
	cfg.Gateway.OpenAIScheduler.SampleRounds = 2
	cfg.Gateway.OpenAIScheduler.SchedulingWaitTimeoutMS = 1000
	cfg.Gateway.OpenAIScheduler.MaxWaiters = 1000

	snapshot := service.NewSchedulerSnapshotService(snapshotCache, nil, repo, nil, cfg)
	concurrency := service.NewConcurrencyService(concurrencyCache)
	gatewayService := service.NewOpenAIGatewayService(
		repo,
		nil, nil, nil, nil, nil, nil, cfg, snapshot, concurrency, nil, nil, nil,
		upstream,
		nil, nil, nil, nil, nil, nil, nil, nil,
	)
	handler := &OpenAIGatewayHandler{
		gatewayService:            gatewayService,
		maxAccountSwitches:        1,
		openAIFailoverBudget:      800 * time.Millisecond,
		openAIMaxDistinctAccounts: 2,
	}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	request := httptest.NewRequest(http.MethodGet, "/v1/models?client_version=0.144.0", nil)
	request = request.WithContext(context.WithValue(request.Context(), ctxkey.Group, group))
	c.Request = request
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{GroupID: &groupID, Group: group})

	handler.CodexModels(c)

	if recorder.Code != http.StatusOK {
		t.Fatalf(
			"status: got %d, want %d; body=%s snapshot=%d metadata=%d db_list=%d db_get=%d load=%d acquire=%d release=%d upstream=%v",
			recorder.Code,
			http.StatusOK,
			recorder.Body.String(),
			snapshotCache.snapshotCalls.Load(),
			snapshotCache.accountCalls.Load(),
			repo.schedulableCalls.Load(),
			repo.getByIDCalls.Load(),
			concurrencyCache.loadCalls.Load(),
			concurrencyCache.acquireCalls.Load(),
			concurrencyCache.releaseCalls.Load(),
			upstream.calls(),
		)
	}
	if got := repo.schedulableCalls.Load(); got != 0 {
		t.Fatalf("adaptive models request queried schedulable accounts from PostgreSQL %d times", got)
	}
	if got := repo.getByIDCalls.Load(); got != 0 {
		t.Fatalf("adaptive models request queried account metadata from PostgreSQL %d times", got)
	}
	if got := concurrencyCache.loadCalls.Load(); got != 0 {
		t.Fatalf("adaptive models request read broad Redis account loads %d times", got)
	}
	if got := concurrencyCache.acquireCalls.Load(); got != 1 {
		t.Fatalf("adaptive models request acquired Redis slots %d times, want 1", got)
	}
	if got := concurrencyCache.releaseCalls.Load(); got != 1 {
		t.Fatalf("adaptive models request released Redis slots %d times, want 1", got)
	}
	if got := releasesObservedByUpstream.Load(); got != 0 {
		t.Fatalf("adaptive models request had released %d slots before the manifest attempt completed", got)
	}
	if got := snapshotCache.snapshotCalls.Load(); got != 1 {
		t.Fatalf("adaptive models request read scheduler snapshots %d times, want 1", got)
	}
	if got := snapshotCache.accountCalls.Load(); got != 0 {
		t.Fatalf("adaptive models request reloaded selected account metadata %d times", got)
	}
	if got := gatewayService.SnapshotOpenAIAccountSchedulerMetrics().RuntimeStatsAccountCount; got != 1 {
		t.Fatalf("adaptive models request reported results for %d accounts, want 1", got)
	}
}

func newCodexModelsFailoverTestHandler(firstStatus int) (*OpenAIGatewayHandler, *codexModelsFailoverHTTPUpstream, int64) {
	return newCodexModelsFailoverTestHandlerWithAccountCount(firstStatus, 2, 3)
}

func newCodexModelsFailoverTestHandlerWithAccountCount(firstStatus, accountCount, maxSwitches int) (*OpenAIGatewayHandler, *codexModelsFailoverHTTPUpstream, int64) {
	gin.SetMode(gin.TestMode)
	groupID := int64(42)
	accounts := make([]service.Account, 0, accountCount)
	for i := 1; i <= accountCount; i++ {
		accounts = append(accounts, service.Account{
			ID:          int64(i),
			Name:        fmt.Sprintf("upstream-%d", i),
			Platform:    service.PlatformOpenAI,
			Type:        service.AccountTypeAPIKey,
			Status:      service.StatusActive,
			Schedulable: true,
			Priority:    i - 1,
			Concurrency: 1,
			Credentials: map[string]any{
				"api_key":  fmt.Sprintf("sk-%d", i),
				"base_url": fmt.Sprintf("https://upstream-%d.example/v1", i),
			},
		})
	}
	upstream := &codexModelsFailoverHTTPUpstream{firstStatus: firstStatus}
	cfg := &config.Config{RunMode: config.RunModeSimple}
	gatewayService := service.NewOpenAIGatewayService(
		codexModelsFailoverAccountRepo{accounts: accounts},
		nil, nil, nil, nil, nil, nil, cfg, nil, nil, nil, nil, nil,
		upstream,
		nil, nil, nil, nil, nil, nil, nil, nil,
	)
	return &OpenAIGatewayHandler{gatewayService: gatewayService, maxAccountSwitches: maxSwitches}, upstream, groupID
}

func performCodexModelsRequest(t *testing.T, handler *OpenAIGatewayHandler, groupID int64) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/models?client_version=0.144.0", nil)
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
		GroupID: &groupID,
		Group:   &service.Group{ID: groupID, Platform: service.PlatformOpenAI},
	})

	handler.CodexModels(c)
	return recorder
}

func equalInt64Slices(got, want []int64) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
