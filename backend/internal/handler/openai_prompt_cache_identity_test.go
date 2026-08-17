package handler

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

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
