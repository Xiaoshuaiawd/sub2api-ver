package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestSettingService_OpenAIUpstreamRetryCountDefaultsToThree(t *testing.T) {
	resetGatewayForwardingSettingsCacheForTest(t)
	svc := NewSettingService(&gatewayTTLSettingRepo{data: map[string]string{}}, &config.Config{})

	settings := svc.parseSettings(map[string]string{})
	require.Equal(t, 3, settings.OpenAIUpstreamRetryCount)
	require.Equal(t, 3, svc.GetOpenAIUpstreamRetryCount(context.Background()))
}

func TestSettingService_OpenAIUpstreamRetryCountParsesConfiguredValue(t *testing.T) {
	resetGatewayForwardingSettingsCacheForTest(t)
	repo := &gatewayTTLSettingRepo{data: map[string]string{
		SettingKeyOpenAIUpstreamRetryCount: "5",
	}}
	svc := NewSettingService(repo, &config.Config{})

	settings := svc.parseSettings(repo.data)
	require.Equal(t, 5, settings.OpenAIUpstreamRetryCount)
	require.Equal(t, 5, svc.GetOpenAIUpstreamRetryCount(context.Background()))
}

func TestSettingService_OpenAIUpstreamRetryCountPersistsAndRefreshesCache(t *testing.T) {
	resetGatewayForwardingSettingsCacheForTest(t)
	repo := &gatewayTTLSettingRepo{data: map[string]string{}}
	svc := NewSettingService(repo, &config.Config{})

	err := svc.UpdateSettings(context.Background(), &SystemSettings{OpenAIUpstreamRetryCount: 4})
	require.NoError(t, err)
	require.Equal(t, "4", repo.data[SettingKeyOpenAIUpstreamRetryCount])
	require.Equal(t, 4, svc.GetOpenAIUpstreamRetryCount(context.Background()))
}

func TestSettingService_OpenAIUpstreamRetryCountRejectsOutOfRange(t *testing.T) {
	for _, value := range []int{-1, MaxOpenAIUpstreamRetryLimit + 1} {
		t.Run("value", func(t *testing.T) {
			repo := &gatewayTTLSettingRepo{data: map[string]string{}}
			svc := NewSettingService(repo, &config.Config{})

			err := svc.UpdateSettings(context.Background(), &SystemSettings{OpenAIUpstreamRetryCount: value})
			require.Error(t, err)
			require.Empty(t, repo.data)
		})
	}
}
