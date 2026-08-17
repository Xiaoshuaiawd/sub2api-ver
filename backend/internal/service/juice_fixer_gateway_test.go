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
	"time"

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

func TestHandleChatStreamingResponse_JuiceRewritesResponsesToChatSSE(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	SetJuiceResolvedValue(c, JuiceResolvedValue{Value: 8, OK: true})

	upstreamBody := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_juice","model":"gpt-5.4","status":"in_progress","output":[]}}`,
		"",
		`data: {"type":"response.output_text.delta","delta":"Juice: 1"}`,
		"",
		`data: {"type":"response.output_text.delta","delta":"2."}`,
		"",
		`data: {"type":"response.completed","response":{"id":"resp_juice","model":"gpt-5.4","status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"Juice: 12."}]}],"usage":{"input_tokens":2,"output_tokens":1,"total_tokens":3}}}`,
		"",
	}, "\n")
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(upstreamBody)),
	}
	svc := &OpenAIGatewayService{cfg: &config.Config{}}

	result, err := svc.handleChatStreamingResponse(
		resp,
		c,
		&Account{ID: 1, Platform: PlatformOpenAI},
		"gpt-5.4",
		"gpt-5.4",
		"gpt-5.4",
		time.Now(),
		64,
	)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Contains(t, rec.Body.String(), `"content":"Juice: 8."`)
	require.NotContains(t, rec.Body.String(), `"content":"Juice: 12."`)
}

func TestStreamRawChatCompletions_JuiceFlushesBeforeUpstreamEOF(t *testing.T) {
	gin.SetMode(gin.TestMode)
	allowTerminal := make(chan struct{})
	terminalWaiting := make(chan struct{})
	reader := &stagedOpenAISSEReadCloser{
		segments: [][]byte{
			[]byte("data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"" + strings.Repeat("a", 300) + "\"}}]}\n\n"),
			[]byte("data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"more text\"}}]}\n\n"),
			[]byte("data: [DONE]\n\n"),
		},
		gates:   []<-chan struct{}{nil, nil, allowTerminal},
		waiting: []chan struct{}{nil, nil, terminalWaiting},
	}
	recorder := newOpenAIResponseFlushRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	SetJuiceResolvedValue(c, JuiceResolvedValue{Value: 8, OK: true})
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       reader,
	}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig()}

	resultCh := make(chan *OpenAIForwardResult, 1)
	errCh := make(chan error, 1)
	go func() {
		result, err := svc.streamRawChatCompletions(
			c,
			resp,
			rawChatCompletionsTestAccount(),
			"gpt-5.4",
			"gpt-5.4",
			"gpt-5.4",
			nil,
			nil,
			time.Now(),
			0,
		)
		resultCh <- result
		errCh <- err
	}()

	waitOpenAIResponseFlushSignal(t, terminalWaiting)
	waitOpenAIResponseFlushCount(t, recorder, 1)
	bodyBeforeEOF, _ := recorder.snapshot()
	require.NotEmpty(t, bodyBeforeEOF)
	require.NotContains(t, bodyBeforeEOF, "[DONE]")

	close(allowTerminal)
	require.NoError(t, <-errCh)
	require.NotNil(t, <-resultCh)
}
