package handler

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
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
	require.False(t, routing.SessionHashUsesAutoIdentity)
}

func TestResolveOpenAIChatPromptCacheRoutingUsesAutomaticIdentityBeforeLegacyHeader(t *testing.T) {
	identity := "019b2745-7252-73f1-93da-ede850329211"
	routing := resolveOpenAIChatPromptCacheRouting(
		[]byte(`{"model":"gpt-5.6","messages":[{"role":"user","content":"hello"}]}`),
		"legacy-session-hash",
		"header-session",
		identity,
	)

	require.Equal(t, identity, routing.PromptCacheKey)
	require.Equal(t, identity, routing.AutoIdentity)
	require.Equal(t, service.DeriveSessionHashFromSeed(identity), routing.SessionHash)
	require.True(t, routing.SessionHashUsesAutoIdentity)
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
	require.False(t, routing.SessionHashUsesAutoIdentity)
}

func TestResolveOpenAIChatPromptCacheRoutingUsesAutomaticIdentityForMissingSessionHash(t *testing.T) {
	identity := "019b2745-7252-73f1-93da-ede850329211"
	routing := resolveOpenAIChatPromptCacheRouting(
		[]byte(`{"model":"gpt-5.6","input":"hello"}`),
		"",
		"",
		identity,
	)

	require.Equal(t, identity, routing.PromptCacheKey)
	require.Equal(t, service.DeriveSessionHashFromSeed(identity), routing.SessionHash)
	require.True(t, routing.SessionHashUsesAutoIdentity)
}

func TestRefreshOpenAIChatPromptCacheRoutingRotatesPromptKeyAndSessionHash(t *testing.T) {
	oldIdentity := "019b2745-7252-73f1-93da-ede850329211"
	newIdentity := "019b2745-7252-73f1-93da-ede850329212"
	routing := openAIChatPromptCacheRouting{
		PromptCacheKey:              oldIdentity,
		AutoIdentity:                oldIdentity,
		SessionHash:                 service.DeriveSessionHashFromSeed(oldIdentity),
		SessionHashUsesAutoIdentity: true,
	}

	refreshed, reselect := refreshOpenAIChatPromptCacheRouting(routing, newIdentity)

	require.True(t, reselect)
	require.Equal(t, newIdentity, refreshed.PromptCacheKey)
	require.Equal(t, newIdentity, refreshed.AutoIdentity)
	require.Equal(t, service.DeriveSessionHashFromSeed(newIdentity), refreshed.SessionHash)
}

func TestRefreshOpenAIChatPromptCacheRoutingSwitchesFromLegacyToAutomaticIdentity(t *testing.T) {
	oldIdentity := "019b2745-7252-73f1-93da-ede850329211"
	newIdentity := "019b2745-7252-73f1-93da-ede850329212"
	routing := openAIChatPromptCacheRouting{
		PromptCacheKey: oldIdentity,
		AutoIdentity:   oldIdentity,
		SessionHash:    "legacy-session-hash",
	}

	refreshed, reselect := refreshOpenAIChatPromptCacheRouting(routing, newIdentity)

	require.True(t, reselect)
	require.Equal(t, newIdentity, refreshed.PromptCacheKey)
	require.Equal(t, newIdentity, refreshed.AutoIdentity)
	require.Equal(t, service.DeriveSessionHashFromSeed(newIdentity), refreshed.SessionHash)
	require.True(t, refreshed.SessionHashUsesAutoIdentity)
}

func TestRefreshOpenAIChatPromptCacheRoutingDropsExpiredIdentityWhenRefreshFails(t *testing.T) {
	oldIdentity := "019b2745-7252-73f1-93da-ede850329211"
	routing := openAIChatPromptCacheRouting{
		PromptCacheKey:         oldIdentity,
		FallbackPromptCacheKey: "header-session",
		AutoIdentity:           oldIdentity,
		SessionHash:            "legacy-session-hash",
	}

	refreshed, reselect := refreshOpenAIChatPromptCacheRouting(routing, "")

	require.False(t, reselect)
	require.Equal(t, "header-session", refreshed.PromptCacheKey)
	require.Empty(t, refreshed.AutoIdentity)
	require.Equal(t, "legacy-session-hash", refreshed.SessionHash)
}

func TestRefreshOpenAIChatPromptCacheRoutingReselectsWhenRedisRecoversAfterExpiry(t *testing.T) {
	oldIdentity := "019b2745-7252-73f1-93da-ede850329211"
	newIdentity := "019b2745-7252-73f1-93da-ede850329212"
	routing := openAIChatPromptCacheRouting{
		PromptCacheKey:              oldIdentity,
		FallbackPromptCacheKey:      "header-session",
		AutoIdentity:                oldIdentity,
		SessionHash:                 service.DeriveSessionHashFromSeed(oldIdentity),
		SessionHashUsesAutoIdentity: true,
	}

	degraded, reselect := refreshOpenAIChatPromptCacheRouting(routing, "")
	require.False(t, reselect)
	require.Equal(t, service.DeriveSessionHashFromSeed(oldIdentity), degraded.SessionHash)
	require.True(t, degraded.SessionHashUsesAutoIdentity)

	recovered, reselect := refreshOpenAIChatPromptCacheRouting(degraded, newIdentity)
	require.True(t, reselect)
	require.Equal(t, newIdentity, recovered.PromptCacheKey)
	require.Equal(t, service.DeriveSessionHashFromSeed(newIdentity), recovered.SessionHash)
}

func TestOpenAIAutoPromptCacheSessionHashFallsBackToGeneratedIdentity(t *testing.T) {
	identity := "019b2745-7252-73f1-93da-ede850329211"

	sessionHash := openAIAutoPromptCacheSessionHash("", identity)

	require.Equal(t, service.DeriveSessionHashFromSeed(identity), sessionHash)
}

func TestOpenAIAutoPromptCacheSessionHashKeepsExistingSchedulingSignal(t *testing.T) {
	sessionHash := openAIAutoPromptCacheSessionHash("existing-session-hash", "019b2745-7252-73f1-93da-ede850329211")

	require.Equal(t, "existing-session-hash", sessionHash)
}

func TestOpenAIAutoPromptCacheRotatedFallbackIdentityRequiresReselection(t *testing.T) {
	previousIdentity := "019b2745-7252-73f1-93da-ede850329211"
	refreshedIdentity := "019b2745-7252-73f1-93da-ede850329212"

	sessionHash, reselect := refreshOpenAIAutoPromptCacheSessionHash(
		service.DeriveSessionHashFromSeed(previousIdentity),
		previousIdentity,
		refreshedIdentity,
		true,
	)

	require.True(t, reselect)
	require.Equal(t, service.DeriveSessionHashFromSeed(refreshedIdentity), sessionHash)
}

func TestOpenAIAutoPromptCacheRotationKeepsExplicitSchedulingSignal(t *testing.T) {
	sessionHash, reselect := refreshOpenAIAutoPromptCacheSessionHash(
		"existing-session-hash",
		"019b2745-7252-73f1-93da-ede850329211",
		"019b2745-7252-73f1-93da-ede850329212",
		false,
	)

	require.False(t, reselect)
	require.Equal(t, "existing-session-hash", sessionHash)
}

func TestOpenAIAutoPromptCacheReselectionDoesNotAdvancePassthroughFailoverState(t *testing.T) {
	h := &OpenAIGatewayHandler{}
	state := &openAIPassthroughFailoverState{}
	previousIdentity := "019b2745-7252-73f1-93da-ede850329211"
	refreshedIdentity := "019b2745-7252-73f1-93da-ede850329212"

	attemptBody, sessionHash, reselect := h.prepareOpenAIForwardAttemptBody(
		nil,
		[]byte(kiroReasoningCanonicalBody),
		newOpenAIPassthroughAccount(70, true),
		state,
		service.DeriveSessionHashFromSeed(previousIdentity),
		previousIdentity,
		refreshedIdentity,
		true,
	)

	require.True(t, reselect)
	require.Nil(t, attemptBody)
	require.Equal(t, service.DeriveSessionHashFromSeed(refreshedIdentity), sessionHash)
	bedrockBody := h.deriveOpenAIForwardAttemptBody(nil, []byte(kiroReasoningCanonicalBody), newOpenAIPassthroughAccount(71, false), state)
	require.Equal(t, 1, reasoningItemCount(t, bedrockBody), "unforwarded passthrough selection must not mutate failover state")
}
