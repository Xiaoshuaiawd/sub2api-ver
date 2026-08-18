//go:build unit

package admin

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestUpdateSettingsOpenAIProxyNormalizesAndPersists(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{})

	rec := doUpdateSettings(t, h, map[string]any{
		"openai_default_proxy_enabled":        true,
		"openai_default_proxy_url":            " socks5://warp-proxy:1080 ",
		"openai_default_proxy_failure_policy": "fallback_direct",
	}, nil)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "true", repo.values[service.SettingKeyOpenAIDefaultProxyEnabled])
	require.Equal(t, "socks5h://warp-proxy:1080", repo.values[service.SettingKeyOpenAIDefaultProxyURL])
	require.Equal(t, "fallback_direct", repo.values[service.SettingKeyOpenAIDefaultProxyFailurePolicy])
	require.Contains(t, rec.Body.String(), `"openai_default_proxy_url":"socks5h://warp-proxy:1080"`)
}

func TestUpdateSettingsOpenAIProxyRejectsInvalidValuesBeforeWrite(t *testing.T) {
	tests := []map[string]any{
		{"openai_default_proxy_url": "ftp://warp-proxy:21"},
		{"openai_default_proxy_failure_policy": "always_direct"},
	}

	for _, body := range tests {
		h, repo := newStepUpSwitchTestHandler(t, map[string]string{})

		rec := doUpdateSettings(t, h, body, nil)

		require.Equal(t, http.StatusBadRequest, rec.Code)
		require.Nil(t, repo.lastUpdates)
	}
}

func TestUpdateSettingsOpenAIProxyPartialPayloadPreservesOmittedFields(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{
		service.SettingKeyOpenAIDefaultProxyEnabled:       "false",
		service.SettingKeyOpenAIDefaultProxyURL:           "http://node-proxy:8080",
		service.SettingKeyOpenAIDefaultProxyFailurePolicy: "fail_closed",
	})

	rec := doUpdateSettings(t, h, map[string]any{
		"openai_default_proxy_failure_policy": "fallback_direct",
	}, nil)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "false", repo.values[service.SettingKeyOpenAIDefaultProxyEnabled])
	require.Equal(t, "http://node-proxy:8080", repo.values[service.SettingKeyOpenAIDefaultProxyURL])
	require.Equal(t, "fallback_direct", repo.values[service.SettingKeyOpenAIDefaultProxyFailurePolicy])
}

func TestGetSettingsIncludesOpenAIProxyStatus(t *testing.T) {
	h, _ := newStepUpSwitchTestHandler(t, map[string]string{})
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/settings", nil)

	h.GetSettings(c)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"openai_default_proxy_enabled":true`)
	require.Contains(t, rec.Body.String(), `"openai_default_proxy_url":"socks5h://warp-proxy:1080"`)
	require.Contains(t, rec.Body.String(), `"openai_default_proxy_failure_policy":"fail_closed"`)
	require.Contains(t, rec.Body.String(), `"openai_default_proxy_status":`)
}

func TestDiffSettingsIncludesOpenAIProxyFields(t *testing.T) {
	before := &service.SystemSettings{
		OpenAIDefaultProxyEnabled:       true,
		OpenAIDefaultProxyURL:           "socks5h://warp-proxy:1080",
		OpenAIDefaultProxyFailurePolicy: service.OpenAIProxyFailurePolicyFailClosed,
	}
	after := &service.SystemSettings{
		OpenAIDefaultProxyEnabled:       false,
		OpenAIDefaultProxyURL:           "http://replacement:8080",
		OpenAIDefaultProxyFailurePolicy: service.OpenAIProxyFailurePolicyFallbackDirect,
	}

	changed := diffSettings(before, after, nil, nil, UpdateSettingsRequest{})

	require.Subset(t, changed, []string{
		service.SettingKeyOpenAIDefaultProxyEnabled,
		service.SettingKeyOpenAIDefaultProxyURL,
		service.SettingKeyOpenAIDefaultProxyFailurePolicy,
	})
}
