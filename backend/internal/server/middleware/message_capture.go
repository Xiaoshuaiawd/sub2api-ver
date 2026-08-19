package middleware

import (
	"io"
	"net/http"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// MessageCapture records the bytes consumed from the client request and the
// bytes actually accepted by the client response writer. It is best-effort:
// capture errors are represented in the artifact and never returned to Gin.
func MessageCapture(cfg config.GatewayMessageStorageConfig, enabledProviders ...interface{ Enabled() bool }) gin.HandlerFunc {
	factory := service.NewMessageCaptureFactory(service.MessageCaptureConfig{
		MaxBodyBytes: cfg.MaxBodyBytes, MemoryBudgetBytes: cfg.MemoryBudgetBytes,
		SpoolDirectory: cfg.SpoolDirectory, SpoolBudgetBytes: cfg.SpoolBudgetBytes,
	})
	var enabledProvider interface{ Enabled() bool }
	if len(enabledProviders) > 0 {
		enabledProvider = enabledProviders[0]
	}
	return func(c *gin.Context) {
		enabled := cfg.Enabled
		if enabledProvider != nil {
			enabled = enabledProvider.Enabled()
		}
		if !enabled {
			c.Next()
			return
		}
		session := factory.NewSession()
		ctx := service.WithMessageCapture(c.Request.Context(), session)
		c.Request = c.Request.WithContext(ctx)
		c.Request.Body = session.WrapRequestBody(c.Request.Body)
		c.Writer = &messageCaptureResponseWriter{ResponseWriter: c.Writer, capture: session.Response()}
		c.Next()
		session.SetStatus(c.Writer.Status())
		session.Finalize(c.Request.Context())
		// Usage/billing records are intentionally written after the handler returns.
		// Keep the artifact briefly so detached workers can claim it; the timer is
		// the leak guard for requests that never produce a usage log.
		session.ScheduleCleanup(30 * time.Second)
	}
}

type messageCaptureResponseWriter struct {
	gin.ResponseWriter
	capture io.Writer
}

func (w *messageCaptureResponseWriter) Write(p []byte) (int, error) {
	n, err := w.ResponseWriter.Write(p)
	if n > 0 {
		_, _ = w.capture.Write(p[:n])
	}
	return n, err
}

func (w *messageCaptureResponseWriter) WriteString(s string) (int, error) {
	n, err := w.ResponseWriter.WriteString(s)
	if n > 0 {
		_, _ = w.capture.Write([]byte(s[:n]))
	}
	return n, err
}

func (w *messageCaptureResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func (w *messageCaptureResponseWriter) Flush() {
	w.ResponseWriter.Flush()
}
