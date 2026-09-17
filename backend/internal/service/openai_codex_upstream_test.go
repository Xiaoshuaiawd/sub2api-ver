package service

import (
	"context"
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
