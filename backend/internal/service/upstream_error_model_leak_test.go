package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestUpstreamErrorMessageLeaksModel(t *testing.T) {
	tests := []struct {
		name string
		msg  string
		want bool
	}{
		{
			name: "unknown provider for model (reported case)",
			msg:  "unknown provider for model gpt-5.6-luna",
			want: true,
		},
		{
			name: "model keyword with colon",
			msg:  "invalid model: claude-opus-4-6",
			want: true,
		},
		{
			name: "model keyword with backticks",
			msg:  "The model `gpt-5.6-luna` does not exist",
			want: true,
		},
		{
			name: "family prefix without model keyword",
			msg:  "gpt-5.6-luna is not supported",
			want: true,
		},
		{
			name: "gemini family",
			msg:  "requested gemini-2.0-flash but got an empty response",
			want: true,
		},
		{
			name: "claude family",
			msg:  "no available channel for claude-sonnet-4-5-20250929",
			want: true,
		},
		{
			name: "empty message",
			msg:  "",
			want: false,
		},
		{
			name: "model is required is not a leak",
			msg:  "model is required",
			want: false,
		},
		{
			name: "invalid model is not a leak",
			msg:  "invalid model",
			want: false,
		},
		{
			name: "model not found is not a leak",
			msg:  "model not found",
			want: false,
		},
		{
			name: "generic upstream failure",
			msg:  "Upstream request failed",
			want: false,
		},
		{
			name: "rate limit message",
			msg:  "Upstream rate limit exceeded, please retry later",
			want: false,
		},
		{
			name: "ordinary english with command word",
			msg:  "command not found",
			want: false,
		},
		{
			name: "context window message mentioning tokens",
			msg:  "This model's maximum context length is 131072 tokens",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, UpstreamErrorMessageLeaksModel(tt.msg))
		})
	}
}

func TestWriteUpstreamModelLeakError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)

	writeUpstreamModelLeakError(c)

	require.Equal(t, http.StatusBadGateway, rec.Code)
	var payload struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &payload))
	require.Equal(t, "upstream_error", payload.Error.Type)
	require.Equal(t, "Upstream service temporarily unavailable", payload.Error.Message)
}

// 上游 400 回显映射后真实模型名时，确定性客户端错误通道也必须归一成统一 5xx。
func TestWriteOpenAIUpstreamClientErrorHidesLeakedModel(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("leaked model is sanitized", func(t *testing.T) {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		body := []byte(`{"error":{"message":"unknown provider for model gpt-5.6-luna","type":"invalid_request_error"}}`)

		writeOpenAIUpstreamClientError(c, http.StatusBadRequest, body, "unknown provider for model gpt-5.6-luna")

		require.Equal(t, http.StatusBadGateway, rec.Code)
		require.NotContains(t, rec.Body.String(), "gpt-5.6-luna")
		require.Contains(t, rec.Body.String(), "Upstream service temporarily unavailable")
	})

	t.Run("leaked model in param is sanitized", func(t *testing.T) {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		body := []byte(`{"error":{"message":"invalid request","type":"invalid_request_error","param":"model gpt-5.6-luna"}}`)

		writeOpenAIUpstreamClientError(c, http.StatusBadRequest, body, "invalid request")

		require.Equal(t, http.StatusBadGateway, rec.Code)
		require.NotContains(t, rec.Body.String(), "gpt-5.6-luna")
	})

	t.Run("ordinary 400 keeps upstream detail", func(t *testing.T) {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		body := []byte(`{"error":{"message":"input[0].content: array too long","type":"invalid_request_error","code":"invalid_value"}}`)

		writeOpenAIUpstreamClientError(c, http.StatusBadRequest, body, "input[0].content: array too long")

		require.Equal(t, http.StatusBadRequest, rec.Code)
		require.Contains(t, rec.Body.String(), "array too long")
		require.Contains(t, rec.Body.String(), "invalid_value")
	})
}

func TestWriteUpstreamModelLeakAnthropicError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)

	writeUpstreamModelLeakAnthropicError(c)

	require.Equal(t, http.StatusBadGateway, rec.Code)
	body := rec.Body.String()
	require.Equal(t, "error", gjson.Get(body, "type").String())
	require.Equal(t, "upstream_error", gjson.Get(body, "error.type").String())
	require.Equal(t, "Upstream service temporarily unavailable", gjson.Get(body, "error.message").String())
}

// 复现上报场景：渠道把 gpt-5.5 映射成 gpt-5.6-luna，上游 400 回显映射后模型名。
// Chat Completions 兼容路径必须归一成统一 5xx，客户端不得看到真实模型名。
func TestHandleCompatErrorResponse_HidesLeakedMappedModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &OpenAIGatewayService{cfg: &config.Config{}}
	c, rec := newOpenAIUpstreamErrorTestContext(t)

	_, err := svc.handleCompatErrorResponse(
		newOpenAIUpstreamErrorResponse(
			http.StatusBadRequest,
			`{"error":{"message":"unknown provider for model gpt-5.6-luna","type":"invalid_request_error"}}`,
		),
		c,
		newOpenAIUpstreamErrorTestAccount(),
		writeChatCompletionsError,
	)

	require.Error(t, err)
	require.Equal(t, http.StatusBadGateway, rec.Code)
	body := rec.Body.String()
	require.NotContains(t, body, "gpt-5.6-luna")
	require.Equal(t, "upstream_error", gjson.Get(body, "error.type").String())
	require.Equal(t, "Upstream service temporarily unavailable", gjson.Get(body, "error.message").String())
}
