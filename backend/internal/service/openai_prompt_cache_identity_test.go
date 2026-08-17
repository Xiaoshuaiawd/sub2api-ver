package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type openAIPromptCacheIdentityStoreStub struct {
	GatewayCache
	resolveCalls     int
	resolveAPIKeyID  int64
	resolveModel     string
	resolveSource    OpenAIPromptCacheIdentitySource
	resolveCandidate string
	resolveTTL       time.Duration
	resolveRecord    *OpenAIPromptCacheIdentityRecord
	resolveErr       error
	resolveDelay     time.Duration
	aliasRecord      *OpenAIPromptCacheIdentityRecord
	aliasErr         error
	aliasDelay       time.Duration
	aliasResponseID  string
	bindCalls        int
	bindResponseID   string
	bindValue        string
	bindTTL          time.Duration
	bindErr          error
}

func (s *openAIPromptCacheIdentityStoreStub) GetSessionAccountID(context.Context, int64, string) (int64, error) {
	return 0, ErrStickySessionNotFound
}

func (s *openAIPromptCacheIdentityStoreStub) SetSessionAccountID(context.Context, int64, string, int64, time.Duration) error {
	return nil
}

func (s *openAIPromptCacheIdentityStoreStub) RefreshSessionTTL(context.Context, int64, string, time.Duration) error {
	return nil
}

func (s *openAIPromptCacheIdentityStoreStub) DeleteSessionAccountID(context.Context, int64, string) error {
	return nil
}

func (s *openAIPromptCacheIdentityStoreStub) SetGrokVideoPendingBilling(context.Context, string, []byte, time.Duration) error {
	return nil
}

func (s *openAIPromptCacheIdentityStoreStub) GetGrokVideoPendingBilling(context.Context, string) ([]byte, error) {
	return nil, nil
}

func (s *openAIPromptCacheIdentityStoreStub) ClaimGrokVideoBilled(context.Context, string, time.Duration) (bool, error) {
	return false, nil
}

func (s *openAIPromptCacheIdentityStoreStub) ReleaseGrokVideoBilled(context.Context, string) error {
	return nil
}

func (s *openAIPromptCacheIdentityStoreStub) ResolveOpenAIPromptCacheIdentity(
	_ context.Context,
	apiKeyID int64,
	modelIdentity string,
	sourceIdentity OpenAIPromptCacheIdentitySource,
	candidate string,
	ttl time.Duration,
) (*OpenAIPromptCacheIdentityRecord, error) {
	if s.resolveDelay > 0 {
		time.Sleep(s.resolveDelay)
	}
	s.resolveCalls++
	s.resolveAPIKeyID = apiKeyID
	s.resolveModel = modelIdentity
	s.resolveSource = sourceIdentity
	s.resolveCandidate = candidate
	s.resolveTTL = ttl
	if s.resolveErr != nil {
		return nil, s.resolveErr
	}
	if s.resolveRecord != nil {
		return s.resolveRecord, nil
	}
	return &OpenAIPromptCacheIdentityRecord{Value: candidate, RemainingTTL: 30 * time.Minute}, nil
}

func (s *openAIPromptCacheIdentityStoreStub) GetOpenAIPromptCacheResponseAlias(
	_ context.Context,
	_ int64,
	_ string,
	responseID string,
) (*OpenAIPromptCacheIdentityRecord, error) {
	if s.aliasDelay > 0 {
		time.Sleep(s.aliasDelay)
	}
	s.aliasResponseID = responseID
	return s.aliasRecord, s.aliasErr
}

func (s *openAIPromptCacheIdentityStoreStub) SetOpenAIPromptCacheResponseAlias(
	_ context.Context,
	_ int64,
	_ string,
	responseID string,
	value string,
	ttl time.Duration,
) (bool, error) {
	s.bindCalls++
	s.bindResponseID = responseID
	s.bindValue = value
	s.bindTTL = ttl
	return s.bindErr == nil, s.bindErr
}

func newOpenAIPromptCacheIdentityTestContext(t *testing.T, body string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	return c
}

func mustOpenAIPromptCacheUUIDv7(t *testing.T) string {
	t.Helper()
	value, err := uuid.NewV7()
	require.NoError(t, err)
	return value.String()
}

func TestOpenAIAutoPromptCacheExplicitKeyBypassesStore(t *testing.T) {
	store := &openAIPromptCacheIdentityStoreStub{}
	svc := &OpenAIGatewayService{cache: store}
	c := newOpenAIPromptCacheIdentityTestContext(t, "")

	resolved := svc.ResolveAndStageOpenAIAutoPromptCacheIdentity(
		context.Background(), c, 101, "gpt-5.6", "", []byte(`{"model":"gpt-5.6","prompt_cache_key":"client-key","input":"hello"}`),
	)

	require.Empty(t, resolved)
	require.Zero(t, store.resolveCalls)
	require.Nil(t, stagedOpenAIAutoPromptCacheIdentity(c))
	decision, ok := GetOpenAIPromptCacheIdentityDecision(c)
	require.True(t, ok)
	require.Equal(t, OpenAIPromptCacheIdentityReasonExplicit, decision.Reason)
}

func TestOpenAIAutoPromptCacheDecisionRecordsRedisMiss(t *testing.T) {
	store := &openAIPromptCacheIdentityStoreStub{}
	svc := &OpenAIGatewayService{cache: store}
	c := newOpenAIPromptCacheIdentityTestContext(t, "")

	resolved := svc.ResolveAndStageOpenAIAutoPromptCacheIdentity(
		context.Background(), c, 109, "gpt-5.6", "session-hash", []byte(`{"model":"gpt-5.6","input":"hello"}`),
	)

	require.NotEmpty(t, resolved)
	decision, ok := GetOpenAIPromptCacheIdentityDecision(c)
	require.True(t, ok)
	require.Equal(t, OpenAIPromptCacheIdentityReasonRedisMiss, decision.Reason)
	require.Equal(t, "session", decision.Source)
	require.False(t, decision.Hit)
	require.Greater(t, decision.RemainingTTL, time.Duration(0))
}

func TestOpenAIAutoPromptCacheDecisionRecordsNoSource(t *testing.T) {
	store := &openAIPromptCacheIdentityStoreStub{}
	svc := &OpenAIGatewayService{cache: store}
	c := newOpenAIPromptCacheIdentityTestContext(t, "")

	resolved := svc.ResolveAndStageOpenAIAutoPromptCacheIdentity(
		context.Background(), c, 110, "gpt-5.6", "", []byte(`{"model":"gpt-5.6"}`),
	)

	require.Empty(t, resolved)
	decision, ok := GetOpenAIPromptCacheIdentityDecision(c)
	require.True(t, ok)
	require.Equal(t, OpenAIPromptCacheIdentityReasonNoSource, decision.Reason)
	require.Empty(t, decision.Source)
}

func TestOpenAIAutoPromptCacheDecisionRecordsUnavailableStore(t *testing.T) {
	svc := &OpenAIGatewayService{}
	c := newOpenAIPromptCacheIdentityTestContext(t, "")

	resolved := svc.ResolveAndStageOpenAIAutoPromptCacheIdentity(
		context.Background(), c, 112, "gpt-5.6", "session-hash", []byte(`{"model":"gpt-5.6","input":"hello"}`),
	)

	require.Empty(t, resolved)
	decision, ok := GetOpenAIPromptCacheIdentityDecision(c)
	require.True(t, ok)
	require.Equal(t, OpenAIPromptCacheIdentityReasonStoreUnavailable, decision.Reason)
}

func TestOpenAIAutoPromptCacheDecisionRecordsRedisError(t *testing.T) {
	store := &openAIPromptCacheIdentityStoreStub{resolveErr: errors.New("redis unavailable")}
	svc := &OpenAIGatewayService{cache: store}
	c := newOpenAIPromptCacheIdentityTestContext(t, "")

	resolved := svc.ResolveAndStageOpenAIAutoPromptCacheIdentity(
		context.Background(), c, 111, "gpt-5.6", "session-hash", []byte(`{"model":"gpt-5.6","input":"hello"}`),
	)

	require.Empty(t, resolved)
	decision, ok := GetOpenAIPromptCacheIdentityDecision(c)
	require.True(t, ok)
	require.Equal(t, OpenAIPromptCacheIdentityReasonRedisError, decision.Reason)
	require.Equal(t, "session", decision.Source)
}

func TestOpenAIAutoPromptCacheStableSourcePriority(t *testing.T) {
	tests := []struct {
		name        string
		headerName  string
		headerValue string
		body        string
		wantSource  string
	}{
		{name: "session header", headerName: "session_id", headerValue: "session-1", body: `{"conversation":"conversation-body","input":"hello"}`, wantSource: "header:session_id:session-1"},
		{name: "claude code header", headerName: "X-Claude-Code-Session-Id", headerValue: "claude-1", body: `{"input":"hello"}`, wantSource: "header:x-claude-code-session-id:claude-1"},
		{name: "conversation string", body: `{"conversation":"conv-1","input":"hello"}`, wantSource: "body:conversation:conv-1"},
		{name: "conversation object", body: `{"conversation":{"id":"conv-2"},"input":"hello"}`, wantSource: "body:conversation:conv-2"},
		{name: "metadata session", body: `{"metadata":{"session_id":"meta-1"},"input":"hello"}`, wantSource: "body:metadata.session_id:meta-1"},
		{name: "client metadata thread", body: `{"client_metadata":{"thread_id":"thread-1"},"input":"hello"}`, wantSource: "body:client_metadata.thread_id:thread-1"},
		{name: "embedded metadata user", body: `{"metadata":{"user_id":"{\"session_id\":\"embedded-1\"}"},"input":"hello"}`, wantSource: "body:metadata.user_id.session_id:embedded-1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newOpenAIPromptCacheIdentityTestContext(t, tt.body)
			if tt.headerName != "" {
				c.Request.Header.Set(tt.headerName, tt.headerValue)
			}
			sourceKind, source := resolveOpenAIAutoPromptCacheStableSource(c, []byte(tt.body))
			require.NotEmpty(t, sourceKind)
			require.Equal(t, tt.wantSource, source)
		})
	}
}

func TestOpenAIAutoPromptCacheContentSourceStableAcrossLaterTurns(t *testing.T) {
	base := []byte(`{"model":"gpt-5.6","instructions":"be concise","input":[{"role":"user","content":"hello"}]}`)
	extended := []byte(`{"model":"gpt-5.6","instructions":"be concise","input":[{"role":"user","content":"hello"},{"role":"assistant","content":"hi"},{"role":"user","content":"continue"}]}`)

	baseSource := resolveOpenAIAutoPromptCacheContentSource(base)
	extendedSource := resolveOpenAIAutoPromptCacheContentSource(extended)
	require.NotEmpty(t, baseSource)
	require.Equal(t, baseSource, extendedSource)
}

func TestOpenAIAutoPromptCachePreviousResponseAliasWins(t *testing.T) {
	value := mustOpenAIPromptCacheUUIDv7(t)
	store := &openAIPromptCacheIdentityStoreStub{aliasRecord: &OpenAIPromptCacheIdentityRecord{
		Value: value, RemainingTTL: 2 * time.Minute, Hit: true,
	}}
	svc := &OpenAIGatewayService{cache: store}
	c := newOpenAIPromptCacheIdentityTestContext(t, "")
	body := []byte(`{"model":"gpt-5.6","previous_response_id":"resp_previous","input":"next question"}`)

	resolved := svc.ResolveAndStageOpenAIAutoPromptCacheIdentity(context.Background(), c, 102, "gpt-5.6", "session-hash", body)

	require.Equal(t, value, resolved)
	require.Equal(t, "resp_previous", store.aliasResponseID)
	require.Zero(t, store.resolveCalls)
	staged := stagedOpenAIAutoPromptCacheIdentity(c)
	require.NotNil(t, staged)
	require.Equal(t, value, staged.Value)
	require.Equal(t, "previous_response_alias", staged.Source)
}

func TestOpenAIAutoPromptCachePreviousResponseMissStartsChainFromResponseID(t *testing.T) {
	store := &openAIPromptCacheIdentityStoreStub{}
	svc := &OpenAIGatewayService{cache: store}
	c := newOpenAIPromptCacheIdentityTestContext(t, "")
	body := []byte(`{"model":"gpt-5.6","previous_response_id":"resp_previous","input":"next question"}`)

	resolved := svc.ResolveAndStageOpenAIAutoPromptCacheIdentity(context.Background(), c, 102, "gpt-5.6", "session-hash", body)

	require.NotEmpty(t, resolved)
	require.Equal(t, OpenAIPromptCacheIdentitySourceSession, store.resolveSource.Kind)
	require.Len(t, store.resolveSource.Hash, 64)
	require.Equal(t, "previous_response", stagedOpenAIAutoPromptCacheIdentity(c).Source)
}

func TestOpenAIAutoPromptCacheCreatesUUIDv7FromContent(t *testing.T) {
	store := &openAIPromptCacheIdentityStoreStub{}
	svc := &OpenAIGatewayService{cache: store}
	c := newOpenAIPromptCacheIdentityTestContext(t, "")
	body := []byte(`{"model":"gpt-5.6","instructions":"be concise","input":[{"role":"user","content":"hello"}]}`)

	resolved := svc.ResolveAndStageOpenAIAutoPromptCacheIdentity(context.Background(), c, 103, "gpt-5.6", "session-hash", body)

	require.NotEmpty(t, resolved)
	parsed, err := uuid.Parse(store.resolveCandidate)
	require.NoError(t, err)
	require.Equal(t, uuid.Version(7), parsed.Version())
	require.Equal(t, int64(103), store.resolveAPIKeyID)
	require.Equal(t, "gpt-5.6-sol", store.resolveModel)
	require.Equal(t, OpenAIPromptCacheIdentitySourcePrefix, store.resolveSource.Kind)
	require.Equal(t, 4, store.resolveSource.ShardCount)
	require.Len(t, store.resolveSource.Hash, 64)
	require.Equal(t, 30*time.Minute, store.resolveTTL)
	decision, ok := GetOpenAIPromptCacheIdentityDecision(c)
	require.True(t, ok)
	require.Len(t, decision.PrefixSHA256, 64)
	require.Len(t, decision.IdentitySHA256, 64)
	require.Equal(t, store.resolveSource.ShardCount, decision.ShardCount)
	require.Equal(t, store.resolveSource.ShardIndex, decision.ShardIndex)
}

func TestOpenAIAutoPromptCacheFinalModelScopesIdentity(t *testing.T) {
	store := &openAIPromptCacheIdentityStoreStub{}
	svc := &OpenAIGatewayService{cache: store}
	body := []byte(`{"instructions":"be concise","input":[{"role":"user","content":"hello"}]}`)

	c1 := newOpenAIPromptCacheIdentityTestContext(t, "")
	first := svc.ResolveAndStageOpenAIAutoPromptCacheIdentity(context.Background(), c1, 103, "mapped-a", "session-hash", body)
	c2 := newOpenAIPromptCacheIdentityTestContext(t, "")
	second := svc.ResolveAndStageOpenAIAutoPromptCacheIdentity(context.Background(), c2, 103, "mapped-b", "session-hash", body)

	require.NotEmpty(t, first)
	require.NotEmpty(t, second)
	require.Equal(t, 2, store.resolveCalls)
}

func TestOpenAIPromptCacheShardForSessionHashIsStable(t *testing.T) {
	first := shardForSessionHash("stable-session", 4)
	second := shardForSessionHash("stable-session", 4)

	require.Equal(t, first, second)
	require.GreaterOrEqual(t, first, 0)
	require.Less(t, first, 4)
	require.Equal(t, 0, shardForSessionHash("", 4))
}

func TestOpenAIAutoPromptCacheReusesStagedIdentityWithinRequest(t *testing.T) {
	store := &openAIPromptCacheIdentityStoreStub{}
	svc := &OpenAIGatewayService{cache: store}
	c := newOpenAIPromptCacheIdentityTestContext(t, "")
	body := []byte(`{"model":"gpt-5.6","input":"hello"}`)

	first := svc.ResolveAndStageOpenAIAutoPromptCacheIdentity(context.Background(), c, 103, "gpt-5.6", "session-hash", body)
	second := svc.ResolveAndStageOpenAIAutoPromptCacheIdentity(context.Background(), c, 103, "gpt-5.6", "session-hash", body)

	require.NotEmpty(t, first)
	require.Equal(t, first, second)
	require.Equal(t, 1, store.resolveCalls)
}

func TestOpenAIAutoPromptCacheStagedIdentityIsScopedBySource(t *testing.T) {
	store := &openAIPromptCacheIdentityStoreStub{}
	svc := &OpenAIGatewayService{cache: store}
	c := newOpenAIPromptCacheIdentityTestContext(t, "")
	firstBody := []byte(`{"model":"gpt-5.6","instructions":"policy one","input":"hello"}`)
	secondBody := []byte(`{"model":"gpt-5.6","instructions":"policy two","input":"hello"}`)

	first := svc.ResolveAndStageOpenAIAutoPromptCacheIdentity(context.Background(), c, 103, "gpt-5.6", "session-hash", firstBody)
	second := svc.ResolveAndStageOpenAIAutoPromptCacheIdentity(context.Background(), c, 103, "gpt-5.6", "session-hash", secondBody)

	require.NotEmpty(t, first)
	require.NotEmpty(t, second)
	require.Equal(t, 2, store.resolveCalls)
}

func TestOpenAIAutoPromptCacheDoesNotReuseStaleIdentityAfterSourceChangeError(t *testing.T) {
	store := &openAIPromptCacheIdentityStoreStub{}
	svc := &OpenAIGatewayService{cache: store}
	c := newOpenAIPromptCacheIdentityTestContext(t, "")
	firstBody := []byte(`{"model":"gpt-5.6","instructions":"policy one","input":"hello"}`)
	secondBody := []byte(`{"model":"gpt-5.6","instructions":"policy two","input":"hello"}`)

	require.NotEmpty(t, svc.ResolveAndStageOpenAIAutoPromptCacheIdentity(
		context.Background(), c, 103, "gpt-5.6", "session-hash", firstBody,
	))
	store.resolveErr = errors.New("redis unavailable")
	require.Empty(t, svc.ResolveAndStageOpenAIAutoPromptCacheIdentity(
		context.Background(), c, 103, "gpt-5.6", "session-hash", secondBody,
	))
	require.Nil(t, stagedOpenAIAutoPromptCacheIdentity(c))
}

func TestOpenAIAutoPromptCacheReusedStageRefreshesDecisionRemainingTTL(t *testing.T) {
	store := &openAIPromptCacheIdentityStoreStub{}
	svc := &OpenAIGatewayService{cache: store}
	c := newOpenAIPromptCacheIdentityTestContext(t, "")
	body := []byte(`{"model":"gpt-5.6","input":"hello"}`)

	resolved := svc.ResolveAndStageOpenAIAutoPromptCacheIdentity(context.Background(), c, 103, "gpt-5.6", "session-hash", body)
	require.NotEmpty(t, resolved)
	staged := stagedOpenAIAutoPromptCacheIdentity(c)
	require.NotNil(t, staged)
	staged.ExpiresAt = time.Now().Add(30 * time.Second)

	reused := svc.ResolveAndStageOpenAIAutoPromptCacheIdentity(context.Background(), c, 103, "gpt-5.6", "session-hash", body)
	require.Equal(t, resolved, reused)
	decision, ok := GetOpenAIPromptCacheIdentityDecision(c)
	require.True(t, ok)
	require.Greater(t, decision.RemainingTTL, time.Duration(0))
	require.LessOrEqual(t, decision.RemainingTTL, 30*time.Second)
}

func TestOpenAIAutoPromptCacheIgnoresRotatingGenericIDs(t *testing.T) {
	store := &openAIPromptCacheIdentityStoreStub{}
	svc := &OpenAIGatewayService{cache: store}
	c := newOpenAIPromptCacheIdentityTestContext(t, "")
	body := []byte(`{"model":"gpt-5.6","id":"msg_123","request_id":"req_123"}`)

	resolved := svc.ResolveAndStageOpenAIAutoPromptCacheIdentity(context.Background(), c, 104, "gpt-5.6", "", body)

	require.Empty(t, resolved)
	require.Zero(t, store.resolveCalls)
}

func TestOpenAIAutoPromptCacheRedisErrorFailsOpen(t *testing.T) {
	store := &openAIPromptCacheIdentityStoreStub{resolveErr: errors.New("redis unavailable")}
	svc := &OpenAIGatewayService{cache: store}
	c := newOpenAIPromptCacheIdentityTestContext(t, "")
	body := []byte(`{"model":"gpt-5.6","input":"hello"}`)

	resolved := svc.ResolveAndStageOpenAIAutoPromptCacheIdentity(context.Background(), c, 105, "gpt-5.6", "session-hash", body)

	require.Empty(t, resolved)
	require.Nil(t, stagedOpenAIAutoPromptCacheIdentity(c))
}

func TestOpenAIAutoPromptCacheBindsResponseAliasWithRemainingTTL(t *testing.T) {
	value := mustOpenAIPromptCacheUUIDv7(t)
	store := &openAIPromptCacheIdentityStoreStub{resolveRecord: &OpenAIPromptCacheIdentityRecord{
		Value: value, RemainingTTL: 2 * time.Minute,
	}}
	svc := &OpenAIGatewayService{cache: store}
	c := newOpenAIPromptCacheIdentityTestContext(t, "")
	body := []byte(`{"model":"gpt-5.6","input":"hello"}`)
	require.Equal(t, value, svc.ResolveAndStageOpenAIAutoPromptCacheIdentity(context.Background(), c, 106, "gpt-5.6", "session-hash", body))

	svc.BindStagedOpenAIAutoPromptCacheResponseAlias(context.Background(), c, "resp_next")

	require.Equal(t, 1, store.bindCalls)
	require.Equal(t, "resp_next", store.bindResponseID)
	require.Equal(t, value, store.bindValue)
	require.Greater(t, store.bindTTL, 119*time.Second)
	require.LessOrEqual(t, store.bindTTL, 2*time.Minute)

	svc.BindStagedOpenAIAutoPromptCacheResponseAlias(context.Background(), c, "msg_not_response")
	require.Equal(t, 1, store.bindCalls, "message IDs must not become response aliases")
}

func TestOpenAIAutoPromptCacheResolveDeadlineDoesNotAddStoreLatency(t *testing.T) {
	value := mustOpenAIPromptCacheUUIDv7(t)
	store := &openAIPromptCacheIdentityStoreStub{
		resolveDelay:  40 * time.Millisecond,
		resolveRecord: &OpenAIPromptCacheIdentityRecord{Value: value, RemainingTTL: 2 * time.Minute},
	}
	svc := &OpenAIGatewayService{cache: store}
	c := newOpenAIPromptCacheIdentityTestContext(t, "")
	startedAt := time.Now()

	require.Equal(t, value, svc.ResolveAndStageOpenAIAutoPromptCacheIdentity(
		context.Background(), c, 107, "gpt-5.6", "session-hash", []byte(`{"model":"gpt-5.6","input":"hello"}`),
	))

	staged := stagedOpenAIAutoPromptCacheIdentity(c)
	require.NotNil(t, staged)
	require.LessOrEqual(t, staged.ExpiresAt, startedAt.Add(2*time.Minute+10*time.Millisecond))
}

func TestOpenAIAutoPromptCacheAliasDeadlineDoesNotAddStoreLatency(t *testing.T) {
	value := mustOpenAIPromptCacheUUIDv7(t)
	store := &openAIPromptCacheIdentityStoreStub{
		aliasDelay: 40 * time.Millisecond,
		aliasRecord: &OpenAIPromptCacheIdentityRecord{
			Value: value, RemainingTTL: 2 * time.Minute, Hit: true,
		},
	}
	svc := &OpenAIGatewayService{cache: store}
	c := newOpenAIPromptCacheIdentityTestContext(t, "")
	startedAt := time.Now()

	require.Equal(t, value, svc.ResolveAndStageOpenAIAutoPromptCacheIdentity(
		context.Background(), c, 108, "gpt-5.6", "session-hash", []byte(`{"model":"gpt-5.6","previous_response_id":"resp_previous","input":"next"}`),
	))

	staged := stagedOpenAIAutoPromptCacheIdentity(c)
	require.NotNil(t, staged)
	require.LessOrEqual(t, staged.ExpiresAt, startedAt.Add(2*time.Minute+10*time.Millisecond))
}
