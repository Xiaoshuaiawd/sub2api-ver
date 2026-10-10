//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMergePreservingSensitiveCreds_PreservesSensitiveWhenIncomingMissing(t *testing.T) {
	existing := map[string]any{
		"refresh_token": "rt-old",
		"access_token":  "at-old",
		"api_key":       "sk-old",
		"base_url":      "https://old.example.com",
	}
	incoming := map[string]any{
		"base_url":      "https://new.example.com",
		"model_mapping": map[string]any{"foo": "bar"},
	}

	out := MergePreservingSensitiveCreds(existing, incoming)

	require.Equal(t, "rt-old", out["refresh_token"], "incoming 没传 refresh_token，应保留 existing")
	require.Equal(t, "at-old", out["access_token"])
	require.Equal(t, "sk-old", out["api_key"])
	require.Equal(t, "https://new.example.com", out["base_url"], "非敏感键由 incoming 决定")
	require.Equal(t, map[string]any{"foo": "bar"}, out["model_mapping"])
}

func TestMergePreservingSensitiveCreds_OverwritesWhenIncomingProvidesSensitive(t *testing.T) {
	existing := map[string]any{
		"refresh_token": "rt-old",
		"api_key":       "sk-old",
	}
	incoming := map[string]any{
		"refresh_token": "rt-new",
		// 显式没传 api_key —— 应保留
	}
	out := MergePreservingSensitiveCreds(existing, incoming)
	require.Equal(t, "rt-new", out["refresh_token"], "incoming 显式传入应覆盖")
	require.Equal(t, "sk-old", out["api_key"], "incoming 没传应保留")
}

func TestMergePreservingSensitiveCreds_DoesNotMutateInputs(t *testing.T) {
	existing := map[string]any{"refresh_token": "rt"}
	incoming := map[string]any{"base_url": "x"}

	_ = MergePreservingSensitiveCreds(existing, incoming)

	require.Equal(t, "rt", existing["refresh_token"])
	require.NotContains(t, existing, "base_url")
	require.Equal(t, "x", incoming["base_url"])
	require.NotContains(t, incoming, "refresh_token")
}

func TestMergePreservingSensitiveCreds_NilInputs(t *testing.T) {
	out := MergePreservingSensitiveCreds(nil, map[string]any{"base_url": "x"})
	require.Equal(t, "x", out["base_url"])
	require.NotContains(t, out, "refresh_token")

	out2 := MergePreservingSensitiveCreds(map[string]any{"refresh_token": "rt"}, nil)
	require.Equal(t, "rt", out2["refresh_token"])
}

func TestMergePreservingSensitiveCreds_NonSensitiveDeletionAllowed(t *testing.T) {
	existing := map[string]any{
		"refresh_token": "rt",
		"base_url":      "https://old",
		"project_id":    "p1",
	}
	incoming := map[string]any{
		"base_url": "https://new",
		// 不带 project_id —— 等同删除（非敏感键由 incoming 决定）
	}
	out := MergePreservingSensitiveCreds(existing, incoming)
	require.Equal(t, "rt", out["refresh_token"], "敏感键保留")
	require.Equal(t, "https://new", out["base_url"])
	require.NotContains(t, out, "project_id", "非敏感键 incoming 不传 = 删除")
}

func TestIsSensitiveCredentialKey(t *testing.T) {
	require.True(t, IsSensitiveCredentialKey("refresh_token"))
	require.True(t, IsSensitiveCredentialKey("api_key"))
	require.True(t, IsSensitiveCredentialKey("private_key"))
	require.False(t, IsSensitiveCredentialKey("base_url"))
	require.False(t, IsSensitiveCredentialKey(""))
	require.False(t, IsSensitiveCredentialKey("model_mapping"))
}

func TestMergePreservingAPIKeyBaseURLs_OmittedAndPartialUpdates(t *testing.T) {
	existing := map[string]any{
		"api_key": "sk-old", "base_url": "https://private-relay.example/v1",
		"api_base_urls": map[string]any{
			"chat_completions": "https://chat-relay.example/v1",
			"anthropic":        "https://messages-relay.example",
		},
	}
	kept := MergePreservingAPIKeyBaseURLs(PlatformZhipu, existing, map[string]any{"model_mapping": map[string]any{"gpt": "upstream"}})
	require.Equal(t, existing["base_url"], kept["base_url"])
	require.Equal(t, existing["api_base_urls"], kept["api_base_urls"])
	require.Equal(t, "sk-old", kept["api_key"])

	changed := MergePreservingAPIKeyBaseURLs(PlatformZhipu, existing, map[string]any{
		"api_base_urls": map[string]any{"anthropic": "https://new-messages.example"},
	})
	require.Equal(t, "https://chat-relay.example/v1", changed["api_base_urls"].(map[string]any)["chat_completions"])
	require.Equal(t, "https://new-messages.example", changed["api_base_urls"].(map[string]any)["anthropic"])
	require.Equal(t, "https://messages-relay.example", existing["api_base_urls"].(map[string]any)["anthropic"])

	cleared := MergePreservingAPIKeyBaseURLs(PlatformZhipu, existing, map[string]any{"api_base_urls": map[string]any{}})
	require.Equal(t, map[string]any{}, cleared["api_base_urls"])

	fixed := map[string]any{"api_protocol": APIProtocolAnthropic, "base_url": "https://hidden-relay.example/messages"}
	adaptive := MergePreservingAPIKeyBaseURLs(PlatformZhipu, fixed, map[string]any{
		"api_protocol":  APIProtocolAdaptive,
		"api_base_urls": map[string]any{"chat_completions": "https://official.example/v1"},
	})
	require.Equal(t, "https://hidden-relay.example/messages", adaptive["api_base_urls"].(map[string]any)[APIProtocolAnthropic])
}

func TestMergePreservingAPIKeyBaseURLs_ModeAndProtocolChanges(t *testing.T) {
	modeSwitch := MergePreservingAPIKeyBaseURLs(PlatformKimi, map[string]any{
		"account_mode": AccountModePayG,
		"api_protocol": APIProtocolAdaptive,
		"base_url":     DefaultKimiPayGBaseURL,
		"api_base_urls": map[string]any{
			APIProtocolChatCompletions: DefaultKimiPayGBaseURL,
			APIProtocolAnthropic:       "https://private-relay.example/anthropic",
		},
	}, map[string]any{"account_mode": AccountModeCoding, "api_protocol": APIProtocolAdaptive})
	require.Equal(t, DefaultKimiCodingBaseURL, modeSwitch["base_url"])
	require.Equal(t, DefaultKimiCodingBaseURL, modeSwitch["api_base_urls"].(map[string]any)[APIProtocolChatCompletions])
	require.Equal(t, "https://private-relay.example/anthropic", modeSwitch["api_base_urls"].(map[string]any)[APIProtocolAnthropic])

	protocolSwitch := MergePreservingAPIKeyBaseURLs(PlatformZhipu, map[string]any{
		"api_protocol": APIProtocolAdaptive,
		"base_url":     "https://chat-relay.example/v1",
		"api_base_urls": map[string]any{
			APIProtocolChatCompletions: "https://chat-relay.example/v1",
			APIProtocolAnthropic:       "https://messages-relay.example",
		},
	}, map[string]any{"api_protocol": APIProtocolAnthropic, "api_base_urls": map[string]any{}})
	require.Equal(t, "https://messages-relay.example", protocolSwitch["base_url"])
	require.Equal(t, map[string]any{}, protocolSwitch["api_base_urls"])

	legacyChat := MergePreservingAPIKeyBaseURLs(PlatformZhipu, map[string]any{
		"api_protocol": APIProtocolAdaptive,
		"base_url":     "https://legacy-chat-relay.example/v1",
		"api_base_urls": map[string]any{
			APIProtocolChatCompletions: "   ",
		},
	}, map[string]any{"api_protocol": APIProtocolChatCompletions, "api_base_urls": map[string]any{}})
	require.Equal(t, "https://legacy-chat-relay.example/v1", legacyChat["base_url"])
}
