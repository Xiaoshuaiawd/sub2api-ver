package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

const (
	openCodeProtocolSessionContextKey  = "opencode_protocol_session_id"
	openCodeProtocolSettingsContextKey = "opencode_protocol_settings"
	openCodeUserAgentSuffix            = " (darwin 24.6.0; arm64) ai-sdk/provider-utils/4.0.38 runtime/bun/1.3.14"
)

func (s *OpenAIGatewayService) PrepareOpenCodeProtocolRequest(ctx context.Context, c *gin.Context) error {
	if s == nil || s.settingService == nil {
		return nil
	}
	return prepareOpenCodeProtocolRequest(c, s.settingService.GetOpenCodeProtocolSettings(ctx))
}

func prepareOpenCodeProtocolRequest(c *gin.Context, settings OpenCodeProtocolSettings) error {
	if !settings.Enabled || c == nil || c.Request == nil {
		return nil
	}

	sessionID := ""
	for _, header := range []string{openCodeSessionHeader, openCodeSessionAffinityHeader, openCodeSessionIDHeader} {
		if candidate := sanitizeSessionID(c.GetHeader(header)); candidate != "" {
			sessionID = candidate
			break
		}
	}
	if sessionID == "" {
		generated, err := generateOpenCodeSessionID()
		if err != nil {
			return err
		}
		sessionID = generated
	}

	c.Request.Header.Set(openCodeSessionHeader, sessionID)
	c.Request.Header.Set(openCodeSessionAffinityHeader, sessionID)
	c.Request.Header.Set(openCodeSessionIDHeader, sessionID)
	c.Set(openCodeProtocolSessionContextKey, sessionID)
	c.Set(openCodeProtocolSettingsContextKey, settings)
	return nil
}

func generateOpenCodeSessionID() (string, error) {
	random := make([]byte, 24)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("generate OpenCode session ID: %w", err)
	}
	return "ses_" + base64.RawURLEncoding.EncodeToString(random), nil
}

func openCodeProtocolSessionFromContext(c *gin.Context) string {
	if c == nil {
		return ""
	}
	if value, ok := c.Get(openCodeProtocolSessionContextKey); ok {
		if sessionID, ok := value.(string); ok {
			return sanitizeSessionID(sessionID)
		}
	}
	return sanitizeSessionID(c.GetHeader(openCodeSessionHeader))
}

func openCodeProtocolSettingsFromContext(c *gin.Context) (OpenCodeProtocolSettings, bool) {
	if c == nil {
		return OpenCodeProtocolSettings{}, false
	}
	value, ok := c.Get(openCodeProtocolSettingsContextKey)
	if !ok {
		return OpenCodeProtocolSettings{}, false
	}
	settings, ok := value.(OpenCodeProtocolSettings)
	return settings, ok && settings.Enabled
}

func applyOpenCodeProtocolHeaders(headers http.Header, sessionID string, settings OpenCodeProtocolSettings) {
	if headers == nil || !settings.Enabled {
		return
	}
	sessionID = sanitizeSessionID(sessionID)
	if sessionID == "" {
		return
	}
	version := normalizeStoredOpenCodeProtocolVersion(settings.Version)
	headers.Set("User-Agent", "opencode/"+version+openCodeUserAgentSuffix)
	headers.Set("Originator", "opencode")
	headers.Set(openCodeSessionHeader, sessionID)
	headers.Set(openCodeSessionAffinityHeader, sessionID)
	headers.Set(openCodeSessionIDHeader, sessionID)
	// Codex-only version metadata contradicts the OpenCode User-Agent.
	headers.Del("Version")
	for key := range headers {
		if strings.EqualFold(key, "version") {
			headers.Del(key)
		}
	}
}

func applyStagedOpenCodeProtocolHeaders(c *gin.Context, headers http.Header) {
	settings, ok := openCodeProtocolSettingsFromContext(c)
	if !ok {
		return
	}
	applyOpenCodeProtocolHeaders(headers, openCodeProtocolSessionFromContext(c), settings)
}
