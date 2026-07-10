package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type openAIRetrySettingStub struct {
	count int
}

func (s openAIRetrySettingStub) GetOpenAIUpstreamRetryCount(context.Context) int {
	return s.count
}

func TestOpenAIUpstreamRetryPolicySetsRequestRetryBudget(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(OpenAIUpstreamRetryPolicy(openAIRetrySettingStub{count: 4}))
	router.GET("/test", func(c *gin.Context) {
		require.Equal(t, 4, service.OpenAIUpstreamRetryLimitFromContext(c.Request.Context()))
		c.Status(http.StatusNoContent)
	})

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusNoContent, recorder.Code)
}

func TestOpenAIUpstreamRetryPolicyUsesDefaultWithoutSettingService(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(OpenAIUpstreamRetryPolicy(nil))
	router.GET("/test", func(c *gin.Context) {
		require.Equal(t, service.DefaultOpenAIUpstreamRetryLimit, service.OpenAIUpstreamRetryLimitFromContext(c.Request.Context()))
		c.Status(http.StatusNoContent)
	})

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusNoContent, recorder.Code)
}
