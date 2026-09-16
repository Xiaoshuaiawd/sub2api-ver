package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

// maxDurationUpstreamStub 只实现 Do：记录请求并按预设响应返回，其余方法沿用接口零值。
type maxDurationUpstreamStub struct {
	HTTPUpstream

	lastReq *http.Request
	resp    *http.Response
}

func (u *maxDurationUpstreamStub) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	u.lastReq = req
	return u.resp, nil
}

// deadlineBlockingBody 先吐出一段数据，随后一直阻塞到上游请求上下文被取消（模拟超长请求）。
type deadlineBlockingBody struct {
	first []byte
	once  sync.Once
	ctxFn func() context.Context
}

func (b *deadlineBlockingBody) Read(p []byte) (int, error) {
	var n int
	b.once.Do(func() { n = copy(p, b.first) })
	if n > 0 {
		return n, nil
	}
	ctx := b.ctxFn()
	if ctx == nil {
		return 0, io.EOF
	}
	<-ctx.Done()
	return 0, ctx.Err()
}

func (b *deadlineBlockingBody) Close() error { return nil }

func maxDurationTestConfig() *config.Config {
	return &config.Config{Security: config.SecurityConfig{
		URLAllowlist: config.URLAllowlistConfig{Enabled: false, AllowInsecureHTTP: true},
	}}
}

func maxDurationTestAccount() *Account {
	return &Account{
		ID:          2601,
		Name:        "openai-max-duration",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Credentials: map[string]any{
			"api_key":  "sk-test",
			"base_url": "http://upstream.example",
		},
	}
}

func TestOpenAIRequestMaxDurationConfigSemantics(t *testing.T) {
	require.Equal(t, 10*time.Minute, (&OpenAIGatewayService{}).OpenAIRequestMaxDuration(),
		"未注入配置时回落到默认 10 分钟")

	disabled := &OpenAIGatewayService{cfg: maxDurationTestConfig()}
	require.Zero(t, disabled.OpenAIRequestMaxDuration(), "显式 0 表示禁用")

	enabled := &OpenAIGatewayService{cfg: maxDurationTestConfig()}
	enabled.cfg.Gateway.OpenAIRequestMaxDurationSeconds = 3600
	require.Equal(t, time.Hour, enabled.OpenAIRequestMaxDuration())
}

func TestWithOpenAIRequestMaxDurationTruncatesLongStream(t *testing.T) {
	gin.SetMode(gin.TestMode)

	body := []byte(`{"model":"gpt-5.4","messages":[{"role":"user","content":"hello"}],"stream":true}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(string(body)))
	c.Request.Header.Set("Content-Type", "application/json")

	upstream := &maxDurationUpstreamStub{}
	upstream.resp = &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: &deadlineBlockingBody{
			first: []byte("data: {\"id\":\"chatcmpl_slow\",\"object\":\"chat.completion.chunk\",\"model\":\"gpt-5.4\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"partial\"}}]}\n\n"),
			ctxFn: func() context.Context {
				if upstream.lastReq == nil {
					return nil
				}
				return upstream.lastReq.Context()
			},
		},
	}

	cfg := maxDurationTestConfig()
	cfg.Gateway.OpenAIRequestMaxDurationSeconds = 600
	svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: upstream}

	// 入口给请求上下文加总时长上限（这里用 200ms 代替配置里的 10 分钟）。
	finish := WithOpenAIRequestMaxDuration(c, 200*time.Millisecond)
	defer finish()

	started := time.Now()
	result, err := svc.forwardAsRawChatCompletions(c.Request.Context(), c, maxDurationTestAccount(), body, "")
	elapsed := time.Since(started)

	require.Less(t, elapsed, 5*time.Second, "超长请求必须在总时长上限处被截断")
	require.NoError(t, err, "到点截断按正常收尾处理，不报上游错误")
	require.NotNil(t, result)
	require.Contains(t, rec.Body.String(), `"content":"partial"`, "截断前已下发的流式内容保持原样")
	require.ErrorIs(t, c.Request.Context().Err(), context.DeadlineExceeded)
}

func TestWithOpenAIRequestMaxDurationDisabledKeepsContext(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader("{}"))

	before := c.Request.Context()
	finish := WithOpenAIRequestMaxDuration(c, 0)
	finish()

	require.True(t, before == c.Request.Context(), "禁用时不应包装请求上下文")
}
