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
	resolveSource    string
	resolveCandidate string
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
	sourceIdentity string,
	candidate string,
	_ time.Duration,
) (*OpenAIPromptCacheIdentityRecord, error) {
	if s.resolveDelay > 0 {
		time.Sleep(s.resolveDelay)
	}
	s.resolveCalls++
	s.resolveAPIKeyID = apiKeyID
	s.resolveModel = modelIdentity
	s.resolveSource = sourceIdentity
	s.resolveCandidate = candidate
	if s.resolveErr != nil {
		return nil, s.resolveErr
	}
	if s.resolveRecord != nil {
		return s.resolveRecord, nil
	}
	return &OpenAIPromptCacheIdentityRecord{Value: candidate, RemainingTTL: 5 * time.Minute}, nil
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
		context.Background(), c, 101, "gpt-5.6", []byte(`{"model":"gpt-5.6","prompt_cache_key":"client-key","input":"hello"}`),
	)

	require.Empty(t, resolved)
	require.Zero(t, store.resolveCalls)
	require.Nil(t, stagedOpenAIAutoPromptCacheIdentity(c))
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

	resolved := svc.ResolveAndStageOpenAIAutoPromptCacheIdentity(context.Background(), c, 102, "gpt-5.6", body)

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

	resolved := svc.ResolveAndStageOpenAIAutoPromptCacheIdentity(context.Background(), c, 102, "gpt-5.6", body)

	require.NotEmpty(t, resolved)
	require.Equal(t, "previous_response:resp_previous", store.resolveSource)
	require.Equal(t, "previous_response", stagedOpenAIAutoPromptCacheIdentity(c).Source)
}

func TestOpenAIAutoPromptCacheCreatesUUIDv7FromContent(t *testing.T) {
	store := &openAIPromptCacheIdentityStoreStub{}
	svc := &OpenAIGatewayService{cache: store}
	c := newOpenAIPromptCacheIdentityTestContext(t, "")
	body := []byte(`{"model":"gpt-5.6","instructions":"be concise","input":[{"role":"user","content":"hello"}]}`)

	resolved := svc.ResolveAndStageOpenAIAutoPromptCacheIdentity(context.Background(), c, 103, "gpt-5.6", body)

	require.NotEmpty(t, resolved)
	parsed, err := uuid.Parse(store.resolveCandidate)
	require.NoError(t, err)
	require.Equal(t, uuid.Version(7), parsed.Version())
	require.Equal(t, int64(103), store.resolveAPIKeyID)
	require.Equal(t, "gpt-5.6", store.resolveModel)
	require.Contains(t, store.resolveSource, "content:")
}

func TestOpenAIAutoPromptCacheReusesStagedIdentityWithinRequest(t *testing.T) {
	store := &openAIPromptCacheIdentityStoreStub{}
	svc := &OpenAIGatewayService{cache: store}
	c := newOpenAIPromptCacheIdentityTestContext(t, "")
	body := []byte(`{"model":"gpt-5.6","input":"hello"}`)

	first := svc.ResolveAndStageOpenAIAutoPromptCacheIdentity(context.Background(), c, 103, "gpt-5.6", body)
	second := svc.ResolveAndStageOpenAIAutoPromptCacheIdentity(context.Background(), c, 103, "gpt-5.6", body)

	require.NotEmpty(t, first)
	require.Equal(t, first, second)
	require.Equal(t, 1, store.resolveCalls)
}

func TestOpenAIAutoPromptCacheIgnoresRotatingGenericIDs(t *testing.T) {
	store := &openAIPromptCacheIdentityStoreStub{}
	svc := &OpenAIGatewayService{cache: store}
	c := newOpenAIPromptCacheIdentityTestContext(t, "")
	body := []byte(`{"model":"gpt-5.6","id":"msg_123","request_id":"req_123"}`)

	resolved := svc.ResolveAndStageOpenAIAutoPromptCacheIdentity(context.Background(), c, 104, "gpt-5.6", body)

	require.Empty(t, resolved)
	require.Zero(t, store.resolveCalls)
}

func TestOpenAIAutoPromptCacheRedisErrorFailsOpen(t *testing.T) {
	store := &openAIPromptCacheIdentityStoreStub{resolveErr: errors.New("redis unavailable")}
	svc := &OpenAIGatewayService{cache: store}
	c := newOpenAIPromptCacheIdentityTestContext(t, "")
	body := []byte(`{"model":"gpt-5.6","input":"hello"}`)

	resolved := svc.ResolveAndStageOpenAIAutoPromptCacheIdentity(context.Background(), c, 105, "gpt-5.6", body)

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
	require.Equal(t, value, svc.ResolveAndStageOpenAIAutoPromptCacheIdentity(context.Background(), c, 106, "gpt-5.6", body))

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
		context.Background(), c, 107, "gpt-5.6", []byte(`{"model":"gpt-5.6","input":"hello"}`),
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
		context.Background(), c, 108, "gpt-5.6", []byte(`{"model":"gpt-5.6","previous_response_id":"resp_previous","input":"next"}`),
	))

	staged := stagedOpenAIAutoPromptCacheIdentity(c)
	require.NotNil(t, staged)
	require.LessOrEqual(t, staged.ExpiresAt, startedAt.Add(2*time.Minute+10*time.Millisecond))
}
