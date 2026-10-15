package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenAIAccountTestLunaIgnoresOAuthModelMapping(t *testing.T) {
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{
		"model_mapping": map[string]any{"gpt-6-luna": "gpt-6-astra", "gpt-5.5": "gpt-6-astra"},
	}}
	require.Equal(t, "gpt-6-luna", resolveOpenAIAccountTestModel(account, "gpt-6-luna"))
	require.Equal(t, "gpt-6-astra", resolveOpenAIAccountTestModel(account, "gpt-5.5"))
}
