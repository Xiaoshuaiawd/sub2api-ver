package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

const (
	openAIPromptCacheIdentityPrefix      = "openai:prompt_cache_identity:v1:"
	openAIPromptCacheResponseAliasPrefix = "openai:prompt_cache_response_alias:v1:"
)

var openAIPromptCacheIdentityResolveScript = redis.NewScript(`
local value = redis.call('GET', KEYS[1])
if value then
  local ttl = redis.call('PTTL', KEYS[1])
  if ttl > 0 then
    return {value, ttl, 1}
  end
end
redis.call('SET', KEYS[1], ARGV[1], 'PX', ARGV[2])
return {ARGV[1], tonumber(ARGV[2]), 0}
`)

var openAIPromptCacheAliasGetScript = redis.NewScript(`
local value = redis.call('GET', KEYS[1])
if not value then
  return {'', -2}
end
local ttl = redis.call('PTTL', KEYS[1])
if ttl <= 0 then
  return {'', ttl}
end
return {value, ttl}
`)

var _ service.OpenAIPromptCacheIdentityStore = (*gatewayCache)(nil)

func hashOpenAIPromptCacheKeyComponent(value string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(value)))
	return hex.EncodeToString(sum[:])
}

func openAIPromptCacheIdentityKey(apiKeyID int64, modelIdentity, sourceIdentity string) string {
	return fmt.Sprintf(
		"%s%d:%s:%s",
		openAIPromptCacheIdentityPrefix,
		apiKeyID,
		hashOpenAIPromptCacheKeyComponent(modelIdentity),
		hashOpenAIPromptCacheKeyComponent(sourceIdentity),
	)
}

func openAIPromptCacheResponseAliasKey(apiKeyID int64, modelIdentity, responseID string) string {
	return fmt.Sprintf(
		"%s%d:%s:%s",
		openAIPromptCacheResponseAliasPrefix,
		apiKeyID,
		hashOpenAIPromptCacheKeyComponent(modelIdentity),
		hashOpenAIPromptCacheKeyComponent(responseID),
	)
}

func (c *gatewayCache) ResolveOpenAIPromptCacheIdentity(
	ctx context.Context,
	apiKeyID int64,
	modelIdentity string,
	sourceIdentity string,
	candidate string,
	ttl time.Duration,
) (*service.OpenAIPromptCacheIdentityRecord, error) {
	if c == nil || c.rdb == nil {
		return nil, errors.New("gateway cache unavailable")
	}
	if apiKeyID <= 0 || strings.TrimSpace(modelIdentity) == "" || strings.TrimSpace(sourceIdentity) == "" || strings.TrimSpace(candidate) == "" {
		return nil, errors.New("invalid OpenAI prompt cache identity input")
	}
	if ttl <= 0 {
		return nil, errors.New("invalid OpenAI prompt cache identity TTL")
	}

	result, err := openAIPromptCacheIdentityResolveScript.Run(
		ctx,
		c.rdb,
		[]string{openAIPromptCacheIdentityKey(apiKeyID, modelIdentity, sourceIdentity)},
		strings.TrimSpace(candidate),
		ttl.Milliseconds(),
	).Slice()
	if err != nil {
		return nil, err
	}
	if len(result) != 3 {
		return nil, fmt.Errorf("invalid OpenAI prompt cache identity result length: %d", len(result))
	}
	value := redisScriptStringValue(result[0])
	remainingMillis, err := redisScriptInt64Value(result[1])
	if err != nil {
		return nil, fmt.Errorf("parse OpenAI prompt cache identity TTL: %w", err)
	}
	hitValue, err := redisScriptInt64Value(result[2])
	if err != nil {
		return nil, fmt.Errorf("parse OpenAI prompt cache identity hit: %w", err)
	}
	if value == "" || remainingMillis <= 0 {
		return nil, errors.New("invalid OpenAI prompt cache identity result")
	}
	return &service.OpenAIPromptCacheIdentityRecord{
		Value:        value,
		RemainingTTL: time.Duration(remainingMillis) * time.Millisecond,
		Hit:          hitValue == 1,
	}, nil
}

func (c *gatewayCache) GetOpenAIPromptCacheResponseAlias(
	ctx context.Context,
	apiKeyID int64,
	modelIdentity string,
	responseID string,
) (*service.OpenAIPromptCacheIdentityRecord, error) {
	if c == nil || c.rdb == nil {
		return nil, errors.New("gateway cache unavailable")
	}
	if apiKeyID <= 0 || strings.TrimSpace(modelIdentity) == "" || strings.TrimSpace(responseID) == "" {
		return nil, nil
	}
	result, err := openAIPromptCacheAliasGetScript.Run(
		ctx,
		c.rdb,
		[]string{openAIPromptCacheResponseAliasKey(apiKeyID, modelIdentity, responseID)},
	).Slice()
	if err != nil {
		return nil, err
	}
	if len(result) != 2 {
		return nil, fmt.Errorf("invalid OpenAI prompt cache alias result length: %d", len(result))
	}
	value := redisScriptStringValue(result[0])
	remainingMillis, err := redisScriptInt64Value(result[1])
	if err != nil {
		return nil, fmt.Errorf("parse OpenAI prompt cache alias TTL: %w", err)
	}
	if value == "" || remainingMillis <= 0 {
		return nil, nil
	}
	return &service.OpenAIPromptCacheIdentityRecord{
		Value:        value,
		RemainingTTL: time.Duration(remainingMillis) * time.Millisecond,
		Hit:          true,
	}, nil
}

func (c *gatewayCache) SetOpenAIPromptCacheResponseAlias(
	ctx context.Context,
	apiKeyID int64,
	modelIdentity string,
	responseID string,
	value string,
	ttl time.Duration,
) (bool, error) {
	if c == nil || c.rdb == nil {
		return false, errors.New("gateway cache unavailable")
	}
	if apiKeyID <= 0 || strings.TrimSpace(modelIdentity) == "" || strings.TrimSpace(responseID) == "" || strings.TrimSpace(value) == "" || ttl <= 0 {
		return false, nil
	}
	return c.rdb.SetNX(
		ctx,
		openAIPromptCacheResponseAliasKey(apiKeyID, modelIdentity, responseID),
		strings.TrimSpace(value),
		ttl,
	).Result()
}

func redisScriptStringValue(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case []byte:
		return strings.TrimSpace(string(typed))
	default:
		return strings.TrimSpace(fmt.Sprint(value))
	}
}

func redisScriptInt64Value(value any) (int64, error) {
	switch typed := value.(type) {
	case int64:
		return typed, nil
	case int:
		return int64(typed), nil
	case string:
		return strconv.ParseInt(typed, 10, 64)
	case []byte:
		return strconv.ParseInt(string(typed), 10, 64)
	default:
		return 0, fmt.Errorf("unexpected Redis integer type %T", value)
	}
}
