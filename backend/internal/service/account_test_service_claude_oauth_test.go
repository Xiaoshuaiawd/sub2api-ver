//go:build unit

package service

import (
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func newAnthropicSetupTokenAccountForTest() *Account {
	return &Account{
		ID:          301,
		Name:        "anthropic-setup-token-test",
		Platform:    PlatformAnthropic,
		Type:        AccountTypeSetupToken,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token": "sk-ant-oat01-test",
		},
		Extra:       map[string]any{},
		Status:      StatusActive,
		Schedulable: true,
	}
}

func TestAccountTestService_ClaudeOAuthMimicsClaudeCode(t *testing.T) {
	upstream := &anthropicHTTPUpstreamRecorder{resp: adaptiveCNAnthropicTestResponse()}
	svc := &AccountTestService{cfg: &config.Config{}, httpUpstream: upstream}
	c, rec := newTestContext()

	require.NoError(t, svc.testClaudeAccountConnection(c, newAnthropicSetupTokenAccountForTest(), "claude-sonnet-5-5"))

	require.NotNil(t, upstream.lastReq)
	require.Equal(t, "https://api.anthropic.com/v1/messages?beta=true", upstream.lastReq.URL.String())
	require.Equal(t, "Bearer sk-ant-oat01-test", upstream.lastReq.Header.Get("Authorization"))
	require.Equal(t, "application/json", getHeaderRaw(upstream.lastReq.Header, "Accept"))
	require.Equal(t, "stream", getHeaderRaw(upstream.lastReq.Header, "x-stainless-helper-method"))
	require.NotEmpty(t, getHeaderRaw(upstream.lastReq.Header, "x-client-request-id"))

	beta := upstream.lastReq.Header.Get("anthropic-beta")
	for _, token := range claude.FullClaudeCodeMimicryBetas() {
		require.True(t, strings.Contains(beta, token), "missing beta token %q in %q", token, beta)
	}

	require.True(t,
		strings.HasPrefix(gjson.GetBytes(upstream.lastBody, "system.0.text").String(), "x-anthropic-billing-header: cc_version="),
		"system[0] must be the billing attribution block: %s", upstream.lastBody,
	)
	require.Contains(t, gjson.GetBytes(upstream.lastBody, "system.1.text").String(), "You are Claude Code")
	require.True(t, gjson.GetBytes(upstream.lastBody, "tools").IsArray())
	require.NotEmpty(t, gjson.GetBytes(upstream.lastBody, "metadata.user_id").String())

	require.Contains(t, rec.Body.String(), `"success":true`)
}
