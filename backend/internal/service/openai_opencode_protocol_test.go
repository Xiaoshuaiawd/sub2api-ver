package service

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newOpenCodeProtocolTestContext(t *testing.T, headers map[string]string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	for key, value := range headers {
		c.Request.Header.Set(key, value)
	}
	return c
}

func TestPrepareOpenCodeProtocolRequestDisabledIsNoop(t *testing.T) {
	c := newOpenCodeProtocolTestContext(t, nil)

	err := prepareOpenCodeProtocolRequest(c, OpenCodeProtocolSettings{Version: DefaultOpenCodeProtocolVersion})

	require.NoError(t, err)
	require.Empty(t, c.GetHeader(openCodeSessionHeader))
	require.Empty(t, c.GetHeader(openCodeSessionAffinityHeader))
	require.Empty(t, c.GetHeader(openCodeSessionIDHeader))
}

func TestPrepareOpenCodeProtocolRequestGeneratesSharedSession(t *testing.T) {
	c := newOpenCodeProtocolTestContext(t, nil)

	err := prepareOpenCodeProtocolRequest(c, OpenCodeProtocolSettings{Enabled: true, Version: DefaultOpenCodeProtocolVersion})

	require.NoError(t, err)
	sessionID := c.GetHeader(openCodeSessionHeader)
	require.True(t, strings.HasPrefix(sessionID, "ses_"))
	require.Equal(t, sessionID, c.GetHeader(openCodeSessionAffinityHeader))
	require.Equal(t, sessionID, c.GetHeader(openCodeSessionIDHeader))
	require.Equal(t, sessionID, openCodeProtocolSessionFromContext(c))
}

func TestPrepareOpenCodeProtocolRequestUsesDeterministicHeaderPrecedence(t *testing.T) {
	tests := []struct {
		name    string
		headers map[string]string
		want    string
	}{
		{name: "session-id", headers: map[string]string{openCodeSessionHeader: "ses-primary"}, want: "ses-primary"},
		{name: "affinity fallback", headers: map[string]string{openCodeSessionAffinityHeader: "ses-affinity"}, want: "ses-affinity"},
		{name: "x-session-id fallback", headers: map[string]string{openCodeSessionIDHeader: "ses-secondary"}, want: "ses-secondary"},
		{name: "conflicting headers", headers: map[string]string{
			openCodeSessionHeader:         "ses-primary",
			openCodeSessionAffinityHeader: "ses-affinity",
			openCodeSessionIDHeader:       "ses-secondary",
		}, want: "ses-primary"},
		{name: "invalid primary is ignored", headers: map[string]string{
			openCodeSessionHeader:         "bad\nvalue",
			openCodeSessionAffinityHeader: "ses-valid",
		}, want: "ses-valid"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newOpenCodeProtocolTestContext(t, tt.headers)
			err := prepareOpenCodeProtocolRequest(c, OpenCodeProtocolSettings{Enabled: true, Version: DefaultOpenCodeProtocolVersion})
			require.NoError(t, err)
			require.Equal(t, tt.want, c.GetHeader(openCodeSessionHeader))
			require.Equal(t, tt.want, c.GetHeader(openCodeSessionAffinityHeader))
			require.Equal(t, tt.want, c.GetHeader(openCodeSessionIDHeader))
		})
	}
}

func TestApplyOpenCodeProtocolHeadersOverwritesIdentity(t *testing.T) {
	headers := make(http.Header)
	headers.Set("User-Agent", "codex-tui/0.146.0")
	headers.Set("Originator", "codex-tui")

	applyOpenCodeProtocolHeaders(headers, "ses-shared", OpenCodeProtocolSettings{Enabled: true, Version: "1.19.0"})

	require.Equal(t, "opencode/1.19.0 (darwin 24.6.0; arm64) ai-sdk/provider-utils/4.0.38 runtime/bun/1.3.14", headers.Get("User-Agent"))
	require.Equal(t, "opencode", headers.Get("Originator"))
	require.Equal(t, "ses-shared", headers.Get(openCodeSessionHeader))
	require.Equal(t, "ses-shared", headers.Get(openCodeSessionAffinityHeader))
	require.Equal(t, "ses-shared", headers.Get(openCodeSessionIDHeader))
}
