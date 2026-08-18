package middleware

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestMessageCaptureMiddlewareCapturesClientBodies(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	var session *service.MessageCaptureSession
	r.Use(MessageCapture(config.GatewayMessageStorageConfig{Enabled: true, MaxBodyBytes: 1024, MemoryBudgetBytes: 4096, SpoolDirectory: t.TempDir()}))
	r.POST("/capture", func(c *gin.Context) {
		body, err := io.ReadAll(c.Request.Body)
		require.NoError(t, err)
		require.Equal(t, `{"input":true}`, string(body))
		_, _ = c.Writer.Write([]byte(`{"output":true}`))
		session = service.MessageCaptureFromContext(c.Request.Context())
	})
	req := httptest.NewRequest("POST", "/capture", strings.NewReader(`{"input":true}`))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, `{"output":true}`, rec.Body.String())
	require.NotNil(t, session)
	require.Equal(t, service.BodyStateAvailable, session.Artifact().Response.State)
}
