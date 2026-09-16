//go:build unit

package handler

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type openAIChatChannelMappingUpstream struct {
	service.HTTPUpstream

	mu            sync.Mutex
	requestModels []string
}

func (u *openAIChatChannelMappingUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}

	u.mu.Lock()
	u.requestModels = append(u.requestModels, gjson.GetBytes(body, "model").String())
	u.mu.Unlock()

	if gjson.GetBytes(body, "stream").Bool() {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body: io.NopCloser(bytes.NewBufferString(
				"data: {\"id\":\"chatcmpl_mapped\",\"object\":\"chat.completion.chunk\",\"model\":\"upstream-reported-model\",\"response\":{\"model\":\"upstream-reported-model\"},\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"}}]}\n\n" +
					"data: {\"id\":\"chatcmpl_mapped\",\"object\":\"chat.completion.chunk\",\"model\":\"upstream-reported-model\",\"choices\":[],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":2,\"total_tokens\":5}}\n\n" +
					"data: [DONE]\n\n",
			)),
		}, nil
	}

	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(bytes.NewBufferString(
			`{"id":"chatcmpl_mapped","object":"chat.completion","model":"upstream-reported-model","response":{"model":"upstream-reported-model"},"choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`,
		)),
	}, nil
}

func (u *openAIChatChannelMappingUpstream) models() []string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]string(nil), u.requestModels...)
}

func TestOpenAIChatCompletions_ChannelMappingRestoresClientModel(t *testing.T) {
	gin.SetMode(gin.TestMode)

	for _, stream := range []bool{false, true} {
		stream := stream
		t.Run(map[bool]string{false: "non_streaming", true: "streaming"}[stream], func(t *testing.T) {
			groupID := int64(4203)
			account := service.Account{
				ID:          9904,
				Name:        "openai-channel-mapping",
				Platform:    service.PlatformOpenAI,
				Type:        service.AccountTypeAPIKey,
				Status:      service.StatusActive,
				Schedulable: true,
				Concurrency: 1,
				Credentials: map[string]any{
					"api_key":  "sk-test",
					"base_url": "https://upstream.example/v1",
					"model_mapping": map[string]any{
						"client-model-a": "client-model-a",
					},
				},
				Extra: map[string]any{"openai_responses_supported": false},
			}
			accountRepo := &openAIWSUsageHandlerAccountRepoStub{account: account}
			channelSvc := service.NewChannelService(&openAIWSUsageHandlerChannelRepoStub{
				channels: []service.Channel{{
					ID:       7702,
					Name:     "openai-chat-channel",
					Status:   service.StatusActive,
					GroupIDs: []int64{groupID},
					ModelMapping: map[string]map[string]string{
						service.PlatformOpenAI: {"client-model-a": "upstream-model-b"},
					},
				}},
				groupPlatforms: map[int64]string{groupID: service.PlatformOpenAI},
			}, nil, nil, nil, nil)
			upstream := &openAIChatChannelMappingUpstream{}
			cfg := &config.Config{RunMode: config.RunModeSimple}
			cfg.Security.URLAllowlist.Enabled = false
			billingCacheSvc := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
			t.Cleanup(billingCacheSvc.Stop)
			gatewaySvc := service.NewOpenAIGatewayService(
				accountRepo, nil, nil, nil, nil, nil, nil, cfg, nil, nil,
				service.NewBillingService(cfg, nil), nil, billingCacheSvc, upstream,
				&service.DeferredService{}, nil, nil, nil, channelSvc, nil, nil, nil,
			)
			cache := &concurrencyCacheMock{
				acquireUserSlotFn:    func(context.Context, int64, int, string) (bool, error) { return true, nil },
				acquireAccountSlotFn: func(context.Context, int64, int, string) (bool, error) { return true, nil },
			}
			h := NewOpenAIGatewayHandler(
				gatewaySvc,
				service.NewConcurrencyService(cache),
				billingCacheSvc,
				service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg),
				nil, nil, nil, nil, cfg,
			)

			requestBody := []byte(`{"model":"client-model-a","messages":[{"role":"user","content":"hello"}],"stream":` + map[bool]string{false: "false", true: "true"}[stream] + `}`)
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/openai/v1/chat/completions", bytes.NewReader(requestBody))
			c.Request.Header.Set("Content-Type", "application/json")
			c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{
				ID:      1805,
				GroupID: &groupID,
				User:    &service.User{ID: 1705, Status: service.StatusActive},
				Group:   &service.Group{ID: groupID, Platform: service.PlatformOpenAI, Status: service.StatusActive},
			})
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 1705, Concurrency: 1})

			h.ChatCompletions(c)

			require.Equal(t, []string{"upstream-model-b"}, upstream.models())
			if stream {
				require.Contains(t, rec.Body.String(), `"model":"client-model-a"`)
				require.NotContains(t, rec.Body.String(), `"model":"upstream-reported-model"`)
				return
			}
			require.Equal(t, "client-model-a", gjson.GetBytes(rec.Body.Bytes(), "model").String())
			require.Equal(t, "client-model-a", gjson.GetBytes(rec.Body.Bytes(), "response.model").String())
		})
	}
}
