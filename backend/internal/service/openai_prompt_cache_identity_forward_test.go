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

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai_compat"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func newOpenAIAutoPromptCacheForwardContext(t *testing.T, path string, body []byte, value string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("api_key", &APIKey{ID: 707})
	stageOpenAIAutoPromptCacheIdentity(c, &openAIAutoPromptCacheIdentity{
		Value:         value,
		ModelIdentity: "gpt-5.6",
		APIKeyID:      707,
		ExpiresAt:     time.Now().Add(5 * time.Minute),
		Source:        "content",
	})
	return c
}

func openAIAutoPromptCacheCaptureResponse() *http.Response {
	return &http.Response{
		StatusCode: http.StatusBadRequest,
		Header:     http.Header{"Content-Type": []string{"application/json"}, "x-request-id": []string{"rid_capture"}},
		Body:       io.NopCloser(strings.NewReader(`{"error":{"type":"invalid_request_error","message":"capture"}}`)),
	}
}

func TestOpenAIAutoPromptCacheForwardInjectsOAuthBodyAndSessionHeaders(t *testing.T) {
	value := mustOpenAIPromptCacheUUIDv7(t)
	body := []byte(`{"model":"gpt-5.6","stream":false,"input":"hello"}`)
	c := newOpenAIAutoPromptCacheForwardContext(t, "/v1/responses", body, value)
	upstream := &httpUpstreamRecorder{resp: openAIAutoPromptCacheCaptureResponse()}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	account := &Account{
		ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Concurrency: 1,
		Credentials: map[string]any{"access_token": "oauth-token", "chatgpt_account_id": "chatgpt-account"},
		Status:      StatusActive, Schedulable: true,
	}

	_, err := svc.Forward(context.Background(), c, account, body)
	require.Error(t, err)
	require.Equal(t, value, gjson.GetBytes(upstream.lastBody, "prompt_cache_key").String())
	require.Equal(t, isolateOpenAISessionID(707, value), upstream.lastReq.Header.Get("session_id"))
	require.Equal(t, isolateOpenAISessionID(707, value), upstream.lastReq.Header.Get("conversation_id"))
}

func TestOpenAIAutoPromptCacheForwardInjectsAPIKeyBody(t *testing.T) {
	value := mustOpenAIPromptCacheUUIDv7(t)
	body := []byte(`{"model":"gpt-5.6","stream":false,"input":"hello"}`)
	c := newOpenAIAutoPromptCacheForwardContext(t, "/v1/responses", body, value)
	upstream := &httpUpstreamRecorder{resp: openAIAutoPromptCacheCaptureResponse()}
	svc := &OpenAIGatewayService{
		cfg:          &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: false, AllowInsecureHTTP: true}}},
		httpUpstream: upstream,
	}
	account := &Account{
		ID: 2, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Concurrency: 1,
		Credentials: map[string]any{"api_key": "sk-test", "base_url": "http://upstream.example"},
		Extra: map[string]any{
			openai_compat.ExtraKeyResponsesMode:      string(openai_compat.ResponsesSupportModeAuto),
			openai_compat.ExtraKeyResponsesSupported: true,
		},
		Status: StatusActive, Schedulable: true,
	}

	_, err := svc.Forward(context.Background(), c, account, body)
	require.Error(t, err)
	require.Equal(t, value, gjson.GetBytes(upstream.lastBody, "prompt_cache_key").String())
	require.Empty(t, upstream.lastReq.Header.Get("session_id"))
}

func TestOpenAIAutoPromptCacheForwardKeepsExplicitClientKey(t *testing.T) {
	autoValue := mustOpenAIPromptCacheUUIDv7(t)
	body := []byte(`{"model":"gpt-5.6","stream":false,"prompt_cache_key":"client-explicit","input":"hello"}`)
	c := newOpenAIAutoPromptCacheForwardContext(t, "/v1/responses", body, autoValue)
	upstream := &httpUpstreamRecorder{resp: openAIAutoPromptCacheCaptureResponse()}
	svc := &OpenAIGatewayService{
		cfg:          &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: false, AllowInsecureHTTP: true}}},
		httpUpstream: upstream,
	}
	account := &Account{
		ID: 3, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Concurrency: 1,
		Credentials: map[string]any{"api_key": "sk-test", "base_url": "http://upstream.example"},
		Extra:       map[string]any{openai_compat.ExtraKeyResponsesSupported: true},
		Status:      StatusActive, Schedulable: true,
	}

	_, err := svc.Forward(context.Background(), c, account, body)
	require.Error(t, err)
	require.Equal(t, "client-explicit", gjson.GetBytes(upstream.lastBody, "prompt_cache_key").String())
}

func TestOpenAIAutoPromptCacheForwardSkipsCompactPath(t *testing.T) {
	value := mustOpenAIPromptCacheUUIDv7(t)
	body := []byte(`{"model":"gpt-5.6","stream":false,"input":"compact"}`)
	c := newOpenAIAutoPromptCacheForwardContext(t, "/v1/responses/compact", body, value)
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}

	patched, changed, err := injectStagedOpenAIAutoPromptCacheIdentity(c, account, body)
	require.NoError(t, err)
	require.False(t, changed)
	require.False(t, gjson.GetBytes(patched, "prompt_cache_key").Exists())
}

func TestOpenAIAutoPromptCacheForwardBindsSuccessfulResponseAlias(t *testing.T) {
	value := mustOpenAIPromptCacheUUIDv7(t)
	body := []byte(`{"model":"gpt-5.6","stream":false,"input":"hello"}`)
	c := newOpenAIAutoPromptCacheForwardContext(t, "/v1/responses", body, value)
	store := &openAIPromptCacheIdentityStoreStub{}
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(
			`{"id":"resp_auto_cache","object":"response","model":"gpt-5.6","status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`,
		)),
	}}
	svc := &OpenAIGatewayService{
		cache:        store,
		cfg:          &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: false, AllowInsecureHTTP: true}}},
		httpUpstream: upstream,
	}
	account := &Account{
		ID: 4, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Concurrency: 1,
		Credentials: map[string]any{"api_key": "sk-test", "base_url": "http://upstream.example"},
		Extra:       map[string]any{openai_compat.ExtraKeyResponsesSupported: true},
		Status:      StatusActive, Schedulable: true,
	}

	result, err := svc.Forward(context.Background(), c, account, body)

	require.NoError(t, err)
	require.Equal(t, "resp_auto_cache", result.ResponseID)
	require.Equal(t, 1, store.bindCalls)
	require.Equal(t, "resp_auto_cache", store.bindResponseID)
	require.Equal(t, value, store.bindValue)
}

func TestOpenAIAutoPromptCacheForwardDoesNotBindFailedResponseAlias(t *testing.T) {
	value := mustOpenAIPromptCacheUUIDv7(t)
	body := []byte(`{"model":"gpt-5.6","stream":false,"input":"hello"}`)
	c := newOpenAIAutoPromptCacheForwardContext(t, "/v1/responses", body, value)
	store := &openAIPromptCacheIdentityStoreStub{}
	upstream := &httpUpstreamRecorder{resp: openAIAutoPromptCacheCaptureResponse()}
	svc := &OpenAIGatewayService{
		cache:        store,
		cfg:          &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: false, AllowInsecureHTTP: true}}},
		httpUpstream: upstream,
	}
	account := &Account{
		ID: 5, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Concurrency: 1,
		Credentials: map[string]any{"api_key": "sk-test", "base_url": "http://upstream.example"},
		Extra:       map[string]any{openai_compat.ExtraKeyResponsesSupported: true},
		Status:      StatusActive, Schedulable: true,
	}

	_, err := svc.Forward(context.Background(), c, account, body)

	require.Error(t, err)
	require.Zero(t, store.bindCalls)
}

func TestOpenAIAutoPromptCacheForwardDoesNotBindHTTPFailedStatus(t *testing.T) {
	value := mustOpenAIPromptCacheUUIDv7(t)
	body := []byte(`{"model":"gpt-5.6","stream":false,"input":"hello"}`)
	c := newOpenAIAutoPromptCacheForwardContext(t, "/v1/responses", body, value)
	store := &openAIPromptCacheIdentityStoreStub{}
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(
			`{"id":"resp_failed","object":"response","model":"gpt-5.6","status":"failed","output":[],"usage":{"input_tokens":1,"output_tokens":0,"total_tokens":1},"error":{"code":"server_error","message":"failed"}}`,
		)),
	}}
	svc := &OpenAIGatewayService{
		cache:        store,
		cfg:          &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: false, AllowInsecureHTTP: true}}},
		httpUpstream: upstream,
	}
	account := &Account{
		ID: 6, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Concurrency: 1,
		Credentials: map[string]any{"api_key": "sk-test", "base_url": "http://upstream.example"},
		Extra:       map[string]any{openai_compat.ExtraKeyResponsesSupported: true},
		Status:      StatusActive, Schedulable: true,
	}

	result, err := svc.Forward(context.Background(), c, account, body)

	require.NoError(t, err)
	require.Equal(t, "resp_failed", result.ResponseID)
	require.Zero(t, store.bindCalls)
}

func TestOpenAIAutoPromptCacheForwardDoesNotBindStreamingIncompleteStatus(t *testing.T) {
	tests := []struct {
		name    string
		account *Account
		cfg     *config.Config
	}{
		{
			name: "oauth native",
			account: &Account{
				ID: 7, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Concurrency: 1,
				Credentials: map[string]any{"access_token": "oauth-token", "chatgpt_account_id": "chatgpt-account"},
				Status:      StatusActive, Schedulable: true,
			},
			cfg: &config.Config{},
		},
		{
			name: "api key passthrough",
			account: &Account{
				ID: 8, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Concurrency: 1,
				Credentials: map[string]any{"api_key": "sk-test", "base_url": "http://upstream.example"},
				Extra:       map[string]any{openai_compat.ExtraKeyResponsesSupported: true},
				Status:      StatusActive, Schedulable: true,
			},
			cfg: &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: false, AllowInsecureHTTP: true}}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			value := mustOpenAIPromptCacheUUIDv7(t)
			body := []byte(`{"model":"gpt-5.6","stream":true,"input":"hello"}`)
			c := newOpenAIAutoPromptCacheForwardContext(t, "/v1/responses", body, value)
			store := &openAIPromptCacheIdentityStoreStub{}
			upstream := &httpUpstreamRecorder{resp: &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body: io.NopCloser(strings.NewReader(
					"data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_stream_incomplete\",\"status\":\"in_progress\"}}\n\n" +
						"data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\n" +
						"data: {\"type\":\"response.incomplete\",\"response\":{\"id\":\"resp_stream_incomplete\",\"status\":\"incomplete\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2}}}\n\n",
				)),
			}}
			svc := &OpenAIGatewayService{cache: store, cfg: tt.cfg, httpUpstream: upstream}

			result, err := svc.Forward(context.Background(), c, tt.account, body)

			require.NoError(t, err)
			require.Equal(t, "resp_stream_incomplete", result.ResponseID)
			require.Zero(t, store.bindCalls)
		})
	}
}
