package middleware

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type OpenAIUpstreamRetrySettingReader interface {
	GetOpenAIUpstreamRetryCount(ctx context.Context) int
}

// OpenAIUpstreamRetryPolicy installs one shared retry budget for the entire
// downstream request. The repository consumes it without reselecting accounts.
func OpenAIUpstreamRetryPolicy(settings OpenAIUpstreamRetrySettingReader) gin.HandlerFunc {
	return func(c *gin.Context) {
		retries := service.DefaultOpenAIUpstreamRetryLimit
		if settings != nil {
			retries = settings.GetOpenAIUpstreamRetryCount(c.Request.Context())
		}
		ctx := service.WithOpenAIUpstreamRetryLimit(c.Request.Context(), retries)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}
