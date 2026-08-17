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
	openAIPromptCacheIdentityPrefix      = "openai:prompt_cache_identity:v2:"
	openAIPromptCacheResponseAliasPrefix = "openai:prompt_cache_response_alias:v2:"
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

func openAIPromptCacheIdentityKey(apiKeyID int64, modelIdentity string, source service.OpenAIPromptCacheIdentitySource) (string, error) {
	if apiKeyID <= 0 || strings.TrimSpace(modelIdentity) == "" {
		return "", errors.New("invalid OpenAI prompt cache identity scope")
	}
	if err := validateOpenAIPromptCacheIdentitySource(source); err != nil {
		return "", err
	}
	modelHash := hashOpenAIPromptCacheKeyComponent(modelIdentity)
	switch source.Kind {
	case service.OpenAIPromptCacheIdentitySourcePrefix:
		return fmt.Sprintf(
			"%s%d:%s:n%d:s%d:%s",
			openAIPromptCacheIdentityPrefix,
			apiKeyID,
			modelHash,
			source.ShardCount,
			source.ShardIndex,
			source.Hash,
		), nil
	case service.OpenAIPromptCacheIdentitySourceSession:
		return fmt.Sprintf(
			"%s%d:%s:session:%s",
			openAIPromptCacheIdentityPrefix,
			apiKeyID,
			modelHash,
			source.Hash,
		), nil
	default:
		return "", fmt.Errorf("unsupported OpenAI prompt cache identity source kind %q", source.Kind)
	}
}

func validateOpenAIPromptCacheIdentitySource(source service.OpenAIPromptCacheIdentitySource) error {
	if source.Kind != service.OpenAIPromptCacheIdentitySourcePrefix && source.Kind != service.OpenAIPromptCacheIdentitySourceSession {
		return fmt.Errorf("unsupported OpenAI prompt cache identity source kind %q", source.Kind)
	}
	if len(source.Hash) != sha256.Size*2 {
		return errors.New("OpenAI prompt cache identity source hash must be SHA-256 hex")
	}
	if _, err := hex.DecodeString(source.Hash); err != nil {
		return errors.New("OpenAI prompt cache identity source hash must be SHA-256 hex")
	}
	if source.Kind == service.OpenAIPromptCacheIdentitySourceSession {
		if source.ShardCount != 0 || source.ShardIndex != 0 {
			return errors.New("session prompt cache identity source cannot contain shard metadata")
		}
		return nil
	}
	if source.ShardCount != 1 && source.ShardCount != 4 && source.ShardCount != 8 && source.ShardCount != 16 {
		return errors.New("unsupported OpenAI prompt cache identity shard count")
	}
	if source.ShardIndex < 0 || source.ShardIndex >= source.ShardCount {
		return errors.New("invalid OpenAI prompt cache identity shard index")
	}
	return nil
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
	source service.OpenAIPromptCacheIdentitySource,
	candidate string,
	ttl time.Duration,
) (*service.OpenAIPromptCacheIdentityRecord, error) {
	if c == nil || c.rdb == nil {
		return nil, errors.New("gateway cache unavailable")
	}
	if apiKeyID <= 0 || strings.TrimSpace(modelIdentity) == "" || strings.TrimSpace(candidate) == "" {
		return nil, errors.New("invalid OpenAI prompt cache identity input")
	}
	key, err := openAIPromptCacheIdentityKey(apiKeyID, modelIdentity, source)
	if err != nil {
		return nil, err
	}
	if ttl <= 0 {
		return nil, errors.New("invalid OpenAI prompt cache identity TTL")
	}

	result, err := openAIPromptCacheIdentityResolveScript.Run(
		ctx,
		c.rdb,
		[]string{key},
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
