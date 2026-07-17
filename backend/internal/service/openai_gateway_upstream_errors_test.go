package service

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewOpenAIUpstreamFailoverError_SwitchesOnlyFor429WithCooldown(t *testing.T) {
	tests := []struct {
		name              string
		statusCode        int
		headers           http.Header
		wantSwitchAccount bool
		wantSameRetry     bool
	}{
		{
			name:          "burst_429_without_reset",
			statusCode:    http.StatusTooManyRequests,
			headers:       http.Header{},
			wantSameRetry: false,
		},
		{
			name:              "429_with_retry_after",
			statusCode:        http.StatusTooManyRequests,
			headers:           http.Header{"Retry-After": []string{"30"}},
			wantSwitchAccount: true,
		},
		{
			name:          "gateway_error_retries_same_account",
			statusCode:    http.StatusBadGateway,
			headers:       http.Header{},
			wantSameRetry: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := newOpenAIUpstreamFailoverError(tt.statusCode, tt.headers, nil, "upstream failed", false)

			require.Equal(t, tt.wantSwitchAccount, err.ShouldRetryNextAccount())
			require.Equal(t, tt.wantSameRetry, err.RetryableOnSameAccount)
		})
	}
}

func TestApplyOpenAIAccountFailoverPolicy_CoversDirectFailoverErrors(t *testing.T) {
	tests := []struct {
		name              string
		platform          string
		statusCode        int
		headers           http.Header
		wantSwitchAccount bool
		wantSameRetry     bool
	}{
		{
			name:          "openai_gateway_error",
			platform:      PlatformOpenAI,
			statusCode:    http.StatusBadGateway,
			wantSameRetry: true,
		},
		{
			name:       "openai_burst_429",
			platform:   PlatformOpenAI,
			statusCode: http.StatusTooManyRequests,
		},
		{
			name:              "openai_429_with_cooldown",
			platform:          PlatformOpenAI,
			statusCode:        http.StatusTooManyRequests,
			headers:           http.Header{"Retry-After": []string{"30"}},
			wantSwitchAccount: true,
		},
		{
			name:              "non_openai_is_unchanged",
			platform:          PlatformGemini,
			statusCode:        http.StatusBadGateway,
			wantSwitchAccount: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := &UpstreamFailoverError{StatusCode: tt.statusCode, ResponseHeaders: tt.headers}
			ApplyOpenAIAccountFailoverPolicy(tt.platform, err)

			require.Equal(t, tt.wantSwitchAccount, err.ShouldRetryNextAccount())
			require.Equal(t, tt.wantSameRetry, err.RetryableOnSameAccount)
		})
	}
}
