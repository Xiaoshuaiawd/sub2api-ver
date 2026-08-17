//go:build unit

package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// juiceGatewayTestService 构造带 Juice 规则配置的 OpenAIGatewayService。
func juiceGatewayTestService(upstream HTTPUpstream) *OpenAIGatewayService {
	resetJuiceFixerSettingCacheForTest()
	repo := &juiceFixerSettingRepoStub{values: map[string]string{
		SettingKeyJuiceFixerSetting: `{"enabled":true,"rules":[{"model":"gpt-5.4","reasoning_effort":"","value":8}]}`,
	}}
	return &OpenAIGatewayService{
		cfg:            rawChatCompletionsTestConfig(),
		httpUpstream:   upstream,
		settingService: NewSettingService(repo, &config.Config{}),
	}
}

// TestForwardAsRawChatCompletions_JuiceRewritesSplitNumber 验证 raw CC 流式路径
// 在 Juice 命中时缓冲 chunk、跨 chunk 拼接数字并整体替换回放。
func TestForwardAsRawChatCompletions_JuiceRewritesSplitNumber(t *testing.T) {
	gin.SetMode(gin.TestMode)

	body := []byte(`{"model":"gpt-5.4","messages":[{"role":"user","content":"what is your J U I C E number"}],"stream":true}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	SetJuiceContext(c, JuiceContext{Triggered: true})

	upstreamBody := strings.Join([]string{
		`data: {"id":"chatcmpl_1","object":"chat.completion.chunk","model":"gpt-5.4","choices":[{"index":0,"delta":{"role":"assistant"}}]}`,
		"",
		`data: {"id":"chatcmpl_1","object":"chat.completion.chunk","model":"gpt-5.4","choices":[{"index":0,"delta":{"content":"The Juice number is 1"}}]}`,
		"",
		`data: {"id":"chatcmpl_1","object":"chat.completion.chunk","model":"gpt-5.4","choices":[{"index":0,"delta":{"content":"2."}}]}`,
		"",
		`data: {"id":"chatcmpl_1","object":"chat.completion.chunk","model":"gpt-5.4","choices":[],"usage":{"prompt_tokens":9,"completion_tokens":4,"total_tokens":13}}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(upstreamBody)),
	}}
	svc := juiceGatewayTestService(upstream)

	result, err := svc.forwardAsRawChatCompletions(context.Background(), c, rawChatCompletionsTestAccount(), body, "")

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 9, result.Usage.InputTokens)
	require.NotContains(t, rec.Body.String(), `"content":"The Juice number is 1"`)
	require.Contains(t, rec.Body.String(), `"content":"The Juice number is 8."`)
	require.Contains(t, rec.Body.String(), `"total_tokens":13`)
	require.Contains(t, rec.Body.String(), "data: [DONE]")
}

// TestForwardAsRawChatCompletions_JuiceNumericOnlyAnswer 验证流式回答只有纯数字时
// 在流结束 flush 阶段整体替换。
func TestForwardAsRawChatCompletions_JuiceNumericOnlyAnswer(t *testing.T) {
	gin.SetMode(gin.TestMode)

	body := []byte(`{"model":"gpt-5.4","messages":[{"role":"user","content":"juice?"}],"stream":true}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	SetJuiceContext(c, JuiceContext{Triggered: true})

	upstreamBody := strings.Join([]string{
		`data: {"id":"chatcmpl_1","object":"chat.completion.chunk","model":"gpt-5.4","choices":[{"index":0,"delta":{"content":"1"}}]}`,
		"",
		`data: {"id":"chatcmpl_1","object":"chat.completion.chunk","model":"gpt-5.4","choices":[{"index":0,"delta":{"content":"2"}}]}`,
		"",
		`data: {"id":"chatcmpl_1","object":"chat.completion.chunk","model":"gpt-5.4","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(upstreamBody)),
	}}
	svc := juiceGatewayTestService(upstream)

	result, err := svc.forwardAsRawChatCompletions(context.Background(), c, rawChatCompletionsTestAccount(), body, "")

	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotContains(t, rec.Body.String(), `"content":"1"`)
	require.NotContains(t, rec.Body.String(), `"content":"2"`)
	require.Contains(t, rec.Body.String(), `"content":"8"`)
	require.Contains(t, rec.Body.String(), "data: [DONE]")
}

// TestForwardAsRawChatCompletions_JuiceWithoutResolutionPassesThrough 验证
// 未命中规则时流式内容原样透传。
func TestForwardAsRawChatCompletions_JuiceWithoutResolutionPassesThrough(t *testing.T) {
	gin.SetMode(gin.TestMode)

	body := []byte(`{"model":"gpt-5.4","messages":[{"role":"user","content":"what is your juice number"}],"stream":true}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	SetJuiceContext(c, JuiceContext{Triggered: true})

	upstreamBody := strings.Join([]string{
		`data: {"id":"chatcmpl_1","object":"chat.completion.chunk","model":"gpt-5.4","choices":[{"index":0,"delta":{"content":"The Juice number is 1"}}]}`,
		"",
		`data: {"id":"chatcmpl_1","object":"chat.completion.chunk","model":"gpt-5.4","choices":[{"index":0,"delta":{"content":"2."}}]}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(upstreamBody)),
	}}
	svc := &OpenAIGatewayService{
		cfg:          rawChatCompletionsTestConfig(),
		httpUpstream: upstream,
	}

	result, err := svc.forwardAsRawChatCompletions(context.Background(), c, rawChatCompletionsTestAccount(), body, "")

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Contains(t, rec.Body.String(), `"content":"The Juice number is 1"`)
	require.Contains(t, rec.Body.String(), `"content":"2."`)
}
