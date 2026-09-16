package service

import (
	"context"
	"errors"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

// defaultOpenAIRequestMaxDuration 是单个 OpenAI 请求的总时长上限默认值（10 分钟）。
const defaultOpenAIRequestMaxDuration = 10 * time.Minute

// OpenAIRequestMaxDuration 返回单个 OpenAI 请求的总时长上限；0 表示禁用。
// 配置未注入时回落到默认值，与 viper 默认保持一致。
func (s *OpenAIGatewayService) OpenAIRequestMaxDuration() time.Duration {
	if s == nil || s.cfg == nil {
		return defaultOpenAIRequestMaxDuration
	}
	seconds := s.cfg.Gateway.OpenAIRequestMaxDurationSeconds
	if seconds <= 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}

// WithOpenAIRequestMaxDuration 给请求上下文加总时长上限，返回供 defer 调用的收尾函数。
//
// 到点后上游请求随 ctx 取消，已下发的流式内容保持原样（客户端看到流提前结束），
// 超长请求不会再继续占用账号并发槽位。maxDuration <= 0 表示禁用，不做任何包装。
func WithOpenAIRequestMaxDuration(c *gin.Context, maxDuration time.Duration) func() {
	if c == nil || c.Request == nil || maxDuration <= 0 {
		return func() {}
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), maxDuration)
	c.Request = c.Request.WithContext(ctx)
	return func() {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			logger.L().Warn("openai.request_max_duration_exceeded",
				zap.String("method", c.Request.Method),
				zap.String("path", c.Request.URL.Path),
				zap.String("max_duration", maxDuration.String()),
			)
		}
		cancel()
	}
}
