package service

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/proxyurl"
)

const DefaultOpenAIDefaultProxyURL = "socks5h://warp-proxy:1080"

type OpenAIProxyFailurePolicy string

const (
	OpenAIProxyFailurePolicyFailClosed     OpenAIProxyFailurePolicy = "fail_closed"
	OpenAIProxyFailurePolicyFallbackDirect OpenAIProxyFailurePolicy = "fallback_direct"
)

type OpenAIProxySettings struct {
	Enabled       bool
	ProxyURL      string
	FailurePolicy OpenAIProxyFailurePolicy
}

func DefaultOpenAIProxySettings() OpenAIProxySettings {
	return OpenAIProxySettings{
		Enabled:       true,
		ProxyURL:      DefaultOpenAIDefaultProxyURL,
		FailurePolicy: OpenAIProxyFailurePolicyFailClosed,
	}
}

func NormalizeOpenAIProxySettings(value OpenAIProxySettings) (OpenAIProxySettings, error) {
	proxyURL, _, err := proxyurl.Parse(value.ProxyURL)
	if err != nil {
		return OpenAIProxySettings{}, fmt.Errorf("invalid OpenAI default proxy URL: %w", err)
	}
	if value.Enabled && proxyURL == "" {
		return OpenAIProxySettings{}, fmt.Errorf("OpenAI default proxy URL is required when proxying is enabled")
	}
	if value.FailurePolicy != OpenAIProxyFailurePolicyFailClosed && value.FailurePolicy != OpenAIProxyFailurePolicyFallbackDirect {
		return OpenAIProxySettings{}, fmt.Errorf("invalid OpenAI proxy failure policy %q", value.FailurePolicy)
	}
	value.ProxyURL = proxyURL
	return value, nil
}

func parseOpenAIProxySettings(settings map[string]string) OpenAIProxySettings {
	value := DefaultOpenAIProxySettings()
	if raw, ok := settings[SettingKeyOpenAIDefaultProxyEnabled]; ok {
		if enabled, err := strconv.ParseBool(strings.TrimSpace(raw)); err == nil {
			value.Enabled = enabled
		}
	}
	if raw, ok := settings[SettingKeyOpenAIDefaultProxyURL]; ok {
		value.ProxyURL = raw
	}
	if raw, ok := settings[SettingKeyOpenAIDefaultProxyFailurePolicy]; ok {
		value.FailurePolicy = OpenAIProxyFailurePolicy(strings.TrimSpace(raw))
	}

	normalized, err := NormalizeOpenAIProxySettings(value)
	if err != nil {
		return DefaultOpenAIProxySettings()
	}
	return normalized
}
