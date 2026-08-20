package handler

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestNewOpenAIGatewayHandlerAdaptiveFailoverCap(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.MaxAccountSwitches = 10
	cfg.Gateway.OpenAIScheduler.AdaptiveEnabled = true
	cfg.Gateway.OpenAIScheduler.MaxDistinctAccountAttempts = 2

	h := NewOpenAIGatewayHandler(nil, nil, nil, nil, nil, nil, nil, nil, cfg)

	require.Equal(t, 1, h.maxAccountSwitches)
	require.Equal(t, 2, h.openAIMaxDistinctAccounts)
}

func TestNewOpenAIGatewayHandlerAdaptiveFailoverDefensivelyCapsInvalidConfig(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.MaxAccountSwitches = 10
	cfg.Gateway.OpenAIScheduler.AdaptiveEnabled = true
	cfg.Gateway.OpenAIScheduler.MaxDistinctAccountAttempts = 10

	h := NewOpenAIGatewayHandler(nil, nil, nil, nil, nil, nil, nil, nil, cfg)

	require.Equal(t, 1, h.maxAccountSwitches)
	require.Equal(t, 2, h.openAIMaxDistinctAccounts)
}

func TestNewOpenAIGatewayHandlerAdaptiveShadowModeDoesNotChangeFailover(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.MaxAccountSwitches = 10
	cfg.Gateway.OpenAIScheduler.AdaptiveEnabled = true
	cfg.Gateway.OpenAIScheduler.ShadowMode = true
	cfg.Gateway.OpenAIScheduler.FailoverTotalBudgetMS = 800
	cfg.Gateway.OpenAIScheduler.MaxDistinctAccountAttempts = 2

	h := NewOpenAIGatewayHandler(nil, nil, nil, nil, nil, nil, nil, nil, cfg)

	require.Equal(t, 10, h.maxAccountSwitches)
	require.Zero(t, h.openAIFailoverBudget)
	require.Zero(t, h.openAIMaxDistinctAccounts)
}

func TestNewOpenAIGatewayHandlerLegacyFailoverUnchanged(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.MaxAccountSwitches = 10

	h := NewOpenAIGatewayHandler(nil, nil, nil, nil, nil, nil, nil, nil, cfg)

	require.Equal(t, 10, h.maxAccountSwitches)
}
