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
}

func TestNewOpenAIGatewayHandlerLegacyFailoverUnchanged(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.MaxAccountSwitches = 10

	h := NewOpenAIGatewayHandler(nil, nil, nil, nil, nil, nil, nil, nil, cfg)

	require.Equal(t, 10, h.maxAccountSwitches)
}
