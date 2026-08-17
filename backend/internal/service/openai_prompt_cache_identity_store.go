package service

import (
	"context"
	"time"
)

// OpenAIPromptCacheIdentityRecord is a Redis-backed automatic prompt-cache
// identity together with the unrefreshed lifetime remaining on its record.
type OpenAIPromptCacheIdentityRecord struct {
	Value        string
	RemainingTTL time.Duration
	Hit          bool
}

// OpenAIPromptCacheIdentityStore keeps automatic prompt-cache identities and
// response-id aliases without exposing the Redis implementation to services.
type OpenAIPromptCacheIdentityStore interface {
	ResolveOpenAIPromptCacheIdentity(
		ctx context.Context,
		apiKeyID int64,
		modelIdentity string,
		sourceIdentity string,
		candidate string,
		ttl time.Duration,
	) (*OpenAIPromptCacheIdentityRecord, error)
	GetOpenAIPromptCacheResponseAlias(
		ctx context.Context,
		apiKeyID int64,
		modelIdentity string,
		responseID string,
	) (*OpenAIPromptCacheIdentityRecord, error)
	SetOpenAIPromptCacheResponseAlias(
		ctx context.Context,
		apiKeyID int64,
		modelIdentity string,
		responseID string,
		value string,
		ttl time.Duration,
	) (bool, error)
}
