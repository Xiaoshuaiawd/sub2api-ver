package handler

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveOpenAIChatPromptCacheRoutingExplicitBodyKeyWins(t *testing.T) {
	routing := resolveOpenAIChatPromptCacheRouting(
		[]byte(`{"model":"gpt-5.6","prompt_cache_key":" client-key ","messages":[{"role":"user","content":"hello"}]}`),
		"legacy-session-hash",
		"header-session",
		"019b2745-7252-73f1-93da-ede850329211",
	)

	require.Equal(t, "client-key", routing.PromptCacheKey)
	require.Empty(t, routing.AutoIdentity)
	require.Equal(t, "legacy-session-hash", routing.SessionHash)
}

func TestResolveOpenAIChatPromptCacheRoutingNeverChangesSessionHash(t *testing.T) {
	identity := "019b2745-7252-73f1-93da-ede850329211"
	routing := resolveOpenAIChatPromptCacheRouting(
		[]byte(`{"model":"gpt-5.6","messages":[{"role":"user","content":"hello"}]}`),
		"conversation-session-hash",
		"header-session",
		identity,
	)

	require.Equal(t, identity, routing.PromptCacheKey)
	require.Equal(t, identity, routing.AutoIdentity)
	require.Equal(t, "conversation-session-hash", routing.SessionHash)
}

func TestResolveOpenAIChatPromptCacheRoutingKeepsEmptySessionHash(t *testing.T) {
	identity := "019b2745-7252-73f1-93da-ede850329211"
	routing := resolveOpenAIChatPromptCacheRouting(
		[]byte(`{"model":"gpt-5.6","messages":[{"role":"user","content":"hello"}]}`),
		"",
		"",
		identity,
	)

	require.Equal(t, identity, routing.PromptCacheKey)
	require.Empty(t, routing.SessionHash)
}

func TestResolveOpenAIChatPromptCacheRoutingFallsBackWhenAutomaticIdentityUnavailable(t *testing.T) {
	routing := resolveOpenAIChatPromptCacheRouting(
		[]byte(`{"model":"gpt-5.6","messages":[{"role":"user","content":"hello"}]}`),
		"legacy-session-hash",
		"header-session",
		"",
	)

	require.Equal(t, "header-session", routing.PromptCacheKey)
	require.Empty(t, routing.AutoIdentity)
	require.Equal(t, "legacy-session-hash", routing.SessionHash)
}
