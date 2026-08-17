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

const (
	OpenAIPromptCacheIdentitySourcePrefix  = "prefix"
	OpenAIPromptCacheIdentitySourceSession = "session"
)

// OpenAIPromptCacheIdentitySource contains only pre-hashed routing material.
// Raw prompts, session IDs, and response IDs must never cross this boundary.
type OpenAIPromptCacheIdentitySource struct {
	Kind       string
	Hash       string
	ShardCount int
	ShardIndex int
}

// OpenAIPromptCacheIdentityStore keeps automatic prompt-cache identities and
// response-id aliases without exposing the Redis implementation to services.
type OpenAIPromptCacheIdentityStore interface {
	ResolveOpenAIPromptCacheIdentity(
		ctx context.Context,
		apiKeyID int64,
		modelIdentity string,
		source OpenAIPromptCacheIdentitySource,
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
