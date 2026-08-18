//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestNormalizeOpenAIProxySettings(t *testing.T) {
	got, err := NormalizeOpenAIProxySettings(OpenAIProxySettings{
		Enabled:       true,
		ProxyURL:      " socks5://warp-proxy:1080 ",
		FailurePolicy: OpenAIProxyFailurePolicyFallbackDirect,
	})

	require.NoError(t, err)
	require.True(t, got.Enabled)
	require.Equal(t, "socks5h://warp-proxy:1080", got.ProxyURL)
	require.Equal(t, OpenAIProxyFailurePolicyFallbackDirect, got.FailurePolicy)
}

func TestNormalizeOpenAIProxySettingsRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name     string
		settings OpenAIProxySettings
	}{
		{
			name: "unsupported proxy scheme",
			settings: OpenAIProxySettings{
				Enabled:       true,
				ProxyURL:      "ftp://warp-proxy:21",
				FailurePolicy: OpenAIProxyFailurePolicyFailClosed,
			},
		},
		{
			name: "enabled proxy requires URL",
			settings: OpenAIProxySettings{
				Enabled:       true,
				FailurePolicy: OpenAIProxyFailurePolicyFailClosed,
			},
		},
		{
			name: "unknown failure policy",
			settings: OpenAIProxySettings{
				Enabled:       true,
				ProxyURL:      DefaultOpenAIDefaultProxyURL,
				FailurePolicy: OpenAIProxyFailurePolicy("always_direct"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NormalizeOpenAIProxySettings(tt.settings)
			require.Error(t, err)
		})
	}
}

func TestSettingServiceParseSettingsDefaultsOpenAIProxyFailClosed(t *testing.T) {
	svc := NewSettingService(&settingUpdateRepoStub{}, &config.Config{})

	settings := svc.parseSettings(map[string]string{})

	require.True(t, settings.OpenAIDefaultProxyEnabled)
	require.Equal(t, DefaultOpenAIDefaultProxyURL, settings.OpenAIDefaultProxyURL)
	require.Equal(t, OpenAIProxyFailurePolicyFailClosed, settings.OpenAIDefaultProxyFailurePolicy)
}

func TestSettingServiceUpdateSettingsPersistsNormalizedOpenAIProxy(t *testing.T) {
	repo := &settingUpdateRepoStub{}
	svc := NewSettingService(repo, &config.Config{})
	settings := svc.parseSettings(map[string]string{})
	settings.OpenAIDefaultProxyEnabled = true
	settings.OpenAIDefaultProxyURL = "socks5://warp-proxy:1080"
	settings.OpenAIDefaultProxyFailurePolicy = OpenAIProxyFailurePolicyFallbackDirect

	err := svc.UpdateSettings(context.Background(), settings)

	require.NoError(t, err)
	require.Equal(t, "true", repo.updates[SettingKeyOpenAIDefaultProxyEnabled])
	require.Equal(t, "socks5h://warp-proxy:1080", repo.updates[SettingKeyOpenAIDefaultProxyURL])
	require.Equal(t, "fallback_direct", repo.updates[SettingKeyOpenAIDefaultProxyFailurePolicy])
}

func TestSettingServiceUpdateSettingsRejectsInvalidOpenAIProxyBeforeWrite(t *testing.T) {
	repo := &settingUpdateRepoStub{}
	svc := NewSettingService(repo, &config.Config{})
	settings := svc.parseSettings(map[string]string{})
	settings.OpenAIDefaultProxyURL = "ftp://warp-proxy:21"

	err := svc.UpdateSettings(context.Background(), settings)

	require.Error(t, err)
	require.Nil(t, repo.updates)
}
