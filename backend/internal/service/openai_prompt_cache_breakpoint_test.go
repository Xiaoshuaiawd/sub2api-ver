package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai_compat"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestInjectOpenAIPromptCacheBreakpoint(t *testing.T) {
	enabled := true
	disabled := false
	baseConfig := &config.Config{Gateway: config.GatewayConfig{OpenAIPromptCache: config.GatewayOpenAIPromptCacheConfig{ExplicitBreakpointsEnabled: &enabled}}}
	account := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Extra: map[string]any{
			openai_compat.ExtraKeyResponsesSupported: true,
		},
	}
	body := []byte(`{"model":"gpt-5.6-sol","input":[{"type":"message","role":"developer","content":[{"type":"input_text","text":"policy"}]},{"type":"message","role":"system","content":[{"type":"input_text","text":"rules"}]},{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}]}`)

	patched, decision, err := injectOpenAIPromptCacheBreakpoint(baseConfig, account, "gpt-5.6-sol", body)
	require.NoError(t, err)
	require.True(t, decision.Injected)
	require.Equal(t, "explicit", gjson.GetBytes(patched, "prompt_cache_options.mode").String())
	require.False(t, gjson.GetBytes(patched, "input.0.content.0.prompt_cache_breakpoint").Exists())
	require.Equal(t, "explicit", gjson.GetBytes(patched, "input.1.content.0.prompt_cache_breakpoint.mode").String())

	tests := []struct {
		name    string
		cfg     *config.Config
		account *Account
		model   string
		body    []byte
	}{
		{name: "disabled", cfg: &config.Config{Gateway: config.GatewayConfig{OpenAIPromptCache: config.GatewayOpenAIPromptCacheConfig{ExplicitBreakpointsEnabled: &disabled}}}, account: account, model: "gpt-5.6-sol", body: body},
		{name: "oauth", cfg: baseConfig, account: &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}, model: "gpt-5.6-sol", body: body},
		{name: "non openai", cfg: baseConfig, account: &Account{Platform: PlatformGrok, Type: AccountTypeAPIKey, Extra: account.Extra}, model: "gpt-5.6-sol", body: body},
		{name: "unknown capability", cfg: baseConfig, account: &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}, model: "gpt-5.6-sol", body: body},
		{name: "pre 5.6", cfg: baseConfig, account: account, model: "gpt-5.5", body: body},
		{name: "chat body", cfg: baseConfig, account: account, model: "gpt-5.6-sol", body: []byte(`{"messages":[{"role":"system","content":"rules"}]}`)},
		{name: "instructions only", cfg: baseConfig, account: account, model: "gpt-5.6-sol", body: []byte(`{"instructions":"rules","input":"hello"}`)},
		{name: "client options", cfg: baseConfig, account: account, model: "gpt-5.6-sol", body: []byte(`{"prompt_cache_options":{"mode":"explicit"},"input":[{"role":"system","content":[{"type":"input_text","text":"rules"}]}]}`)},
		{name: "client breakpoint", cfg: baseConfig, account: account, model: "gpt-5.6-sol", body: []byte(`{"input":[{"role":"system","content":[{"type":"input_text","text":"rules","prompt_cache_breakpoint":{"mode":"explicit"}}]}]}`)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, gotDecision, gotErr := injectOpenAIPromptCacheBreakpoint(tt.cfg, tt.account, tt.model, tt.body)
			require.NoError(t, gotErr)
			require.False(t, gotDecision.Injected)
			require.Equal(t, tt.body, got)
		})
	}
}
