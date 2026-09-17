package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

func codexUpstreamTestService() *OpenAIGatewayService {
	return &OpenAIGatewayService{cfg: &config.Config{Security: config.SecurityConfig{
		URLAllowlist: config.URLAllowlistConfig{Enabled: false, AllowInsecureHTTP: true},
	}}}
}

func codexUpstreamTestAccount() *Account {
	return &Account{
		ID:       3301,
		Name:     "codex-upstream",
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token":       "test-access-token",
			"chatgpt_account_id": "acc-123",
		},
	}
}

func setCodexUpstreamOverride(t *testing.T, rawURL string) {
	t.Helper()
	gatewayForwardingCache.Store(&cachedGatewayForwardingSettings{
		openAICodexUpstreamURL: rawURL,
		expiresAt:              time.Now().Add(time.Minute).UnixNano(),
	})
	t.Cleanup(func() {
		gatewayForwardingCache.Store(&cachedGatewayForwardingSettings{
			expiresAt: time.Now().Add(time.Minute).UnixNano(),
		})
	})
}

func TestCodexUpstreamDefaultsToOfficialURL(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := codexUpstreamTestService()
	account := codexUpstreamTestAccount()

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader("{}"))

	req, err := svc.buildUpstreamRequest(context.Background(), c, account, []byte(`{"model":"gpt-5.4"}`), "token", true, "", true)
	require.NoError(t, err)
	require.Equal(t, chatgptCodexURL, req.URL.String())

	wsURL, err := svc.buildOpenAIResponsesWSURL(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, "wss://chatgpt.com/backend-api/codex/responses", wsURL)
}

func TestCodexUpstreamOverrideReplacesTargetURL(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := codexUpstreamTestService()
	account := codexUpstreamTestAccount()
	setCodexUpstreamOverride(t, "http://180.178.56.226:9620/v1/responses")

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader("{}"))

	req, err := svc.buildUpstreamRequest(context.Background(), c, account, []byte(`{"model":"gpt-5.4"}`), "token", true, "", true)
	require.NoError(t, err)
	require.Equal(t, "http://180.178.56.226:9620/v1/responses", req.URL.String())
	// 请求头等其余内容保持不变：鉴权与 Codex 身份头照常下发。
	require.Equal(t, "Bearer token", req.Header.Get("Authorization"))

	wsURL, err := svc.buildOpenAIResponsesWSURL(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, "ws://180.178.56.226:9620/v1/responses", wsURL)
}

func TestCodexUpstreamOverrideKeepsRequestPathSuffix(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := codexUpstreamTestService()
	account := codexUpstreamTestAccount()
	setCodexUpstreamOverride(t, "https://codex-mirror.example/v1/responses")

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", strings.NewReader("{}"))
	c.Request.URL.Path = "/openai/v1/responses/compact"

	req, err := svc.buildUpstreamRequest(context.Background(), c, account, []byte(`{"model":"gpt-5.4"}`), "token", true, "", true)
	require.NoError(t, err)
	require.Equal(t, "https://codex-mirror.example/v1/responses/compact", req.URL.String())
}

func TestCodexUpstreamOverrideRejectsInvalidURL(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := codexUpstreamTestService()
	account := codexUpstreamTestAccount()
	setCodexUpstreamOverride(t, "http://")

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader("{}"))

	_, err := svc.buildUpstreamRequest(context.Background(), c, account, []byte(`{"model":"gpt-5.4"}`), "token", true, "", true)
	require.Error(t, err)
	require.Contains(t, err.Error(), SettingKeyOpenAICodexUpstreamURL)

	_, wsErr := svc.buildOpenAIResponsesWSURL(context.Background(), account)
	require.Error(t, wsErr)
}

func TestAccountTestUsesCustomCodexUpstream(t *testing.T) {
	gin.SetMode(gin.TestMode)

	account := Account{
		ID:          1,
		Name:        "openai-oauth",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token":       "oauth-token",
			"chatgpt_account_id": "chatgpt-acc",
		},
	}
	repo := &snapshotUpdateAccountRepo{stubOpenAIAccountRepo: stubOpenAIAccountRepo{accounts: []Account{account}}}
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}, "x-request-id": []string{"rid-probe"}},
		Body:       io.NopCloser(strings.NewReader(compactProbeSSESuccessBody)),
	}}
	svc := &AccountTestService{
		accountRepo:          repo,
		httpUpstream:         upstream,
		openaiGatewayService: codexUpstreamTestService(),
	}
	setCodexUpstreamOverride(t, "http://180.178.56.226:9620/v1/responses")

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/1/test", bytes.NewReader(nil))

	err := svc.TestAccountConnection(c, account.ID, "gpt-5.4", "", AccountTestModeCompact)
	require.NoError(t, err)
	require.Equal(t, "http://180.178.56.226:9620/v1/responses", upstream.lastReq.URL.String())
	// Host 头与线上转发保持一致：官方 OAuth 请求固定 Host: chatgpt.com，只换目标 URL。
	require.Equal(t, "chatgpt.com", upstream.lastReq.Host)
}

func TestAccountTestRejectsInvalidCustomCodexUpstream(t *testing.T) {
	gin.SetMode(gin.TestMode)

	account := Account{
		ID:          1,
		Name:        "openai-oauth",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token":       "oauth-token",
			"chatgpt_account_id": "chatgpt-acc",
		},
	}
	repo := &snapshotUpdateAccountRepo{stubOpenAIAccountRepo: stubOpenAIAccountRepo{accounts: []Account{account}}}
	upstream := &httpUpstreamRecorder{}
	svc := &AccountTestService{
		accountRepo:          repo,
		httpUpstream:         upstream,
		openaiGatewayService: codexUpstreamTestService(),
	}
	setCodexUpstreamOverride(t, "http://")

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/1/test", bytes.NewReader(nil))

	err := svc.TestAccountConnection(c, account.ID, "gpt-5.4", "", AccountTestModeCompact)
	require.Error(t, err)
	require.Contains(t, err.Error(), SettingKeyOpenAICodexUpstreamURL)
	require.Contains(t, rec.Body.String(), "openai_codex_upstream_url")
	require.Nil(t, upstream.lastReq, "校验失败时不应发出上游请求")
}
