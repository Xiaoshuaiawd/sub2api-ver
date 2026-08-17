package service

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
	"go.uber.org/zap"
)

const openAIAutoPromptCacheIdentityGinKey = "openai_auto_prompt_cache_identity"

const openAIPromptCacheIdentityDecisionGinKey = "openai_prompt_cache_identity_decision"

const (
	OpenAIPromptCacheIdentityReasonExplicit           = "explicit"
	OpenAIPromptCacheIdentityReasonRedisHit           = "redis_hit"
	OpenAIPromptCacheIdentityReasonRedisMiss          = "redis_miss"
	OpenAIPromptCacheIdentityReasonNoSource           = "no_source"
	OpenAIPromptCacheIdentityReasonStoreUnavailable   = "store_unavailable"
	OpenAIPromptCacheIdentityReasonRedisError         = "redis_error"
	OpenAIPromptCacheIdentityReasonInvalidCachedValue = "invalid_cached_value"
	OpenAIPromptCacheIdentityReasonUUIDError          = "uuid_error"
	OpenAIPromptCacheIdentityReasonUnsupportedPath    = "unsupported_path"
	OpenAIPromptCacheIdentityReasonInvalidModel       = "invalid_model"
)

// OpenAIPromptCacheIdentityDecision explains automatic identity handling
// without retaining the prompt or any raw session identifier.
type OpenAIPromptCacheIdentityDecision struct {
	Reason           string
	Source           string
	Hit              bool
	RemainingTTL     time.Duration
	PrefixSHA256     string
	IdentitySHA256   string
	ShardCount       int
	ShardIndex       int
	BreakpointReason string
}

type openAIAutoPromptCacheIdentity struct {
	Value         string
	ModelIdentity string
	StoreSource   OpenAIPromptCacheIdentitySource
	APIKeyID      int64
	ExpiresAt     time.Time
	Source        string
	Hit           bool
}

// ResolveAndStageOpenAIAutoPromptCacheIdentity resolves a fixed-lifetime
// UUIDv7 for requests that will be forwarded through OpenAI Responses and do
// not provide prompt_cache_key. Cache and UUID failures deliberately fail open.
func (s *OpenAIGatewayService) ResolveAndStageOpenAIAutoPromptCacheIdentity(
	ctx context.Context,
	c *gin.Context,
	apiKeyID int64,
	finalModel string,
	sessionHash string,
	body []byte,
) string {
	if s == nil || c == nil || apiKeyID <= 0 || len(body) == 0 {
		return ""
	}
	if isOpenAIResponsesCompactPath(c) {
		setOpenAIPromptCacheIdentityDecision(c, OpenAIPromptCacheIdentityDecision{Reason: OpenAIPromptCacheIdentityReasonUnsupportedPath})
		return ""
	}
	if strings.TrimSpace(gjson.GetBytes(body, "prompt_cache_key").String()) != "" {
		setOpenAIPromptCacheIdentityDecision(c, OpenAIPromptCacheIdentityDecision{Reason: OpenAIPromptCacheIdentityReasonExplicit})
		return ""
	}
	modelIdentity := canonicalOpenAIPromptCacheModel(finalModel)
	if modelIdentity == "" {
		setOpenAIPromptCacheIdentityDecision(c, OpenAIPromptCacheIdentityDecision{Reason: OpenAIPromptCacheIdentityReasonInvalidModel})
		return ""
	}
	store, ok := s.cache.(OpenAIPromptCacheIdentityStore)
	if !ok || store == nil {
		setOpenAIPromptCacheIdentityDecision(c, OpenAIPromptCacheIdentityDecision{Reason: OpenAIPromptCacheIdentityReasonStoreUnavailable})
		return ""
	}
	if ctx == nil {
		ctx = context.Background()
	}

	previousResponseID := strings.TrimSpace(gjson.GetBytes(body, "previous_response_id").String())
	validPreviousResponseID := ClassifyOpenAIPreviousResponseIDKind(previousResponseID) == OpenAIPreviousResponseIDKindResponseID
	if validPreviousResponseID {
		aliasDeadlineBase := time.Now()
		alias, err := store.GetOpenAIPromptCacheResponseAlias(ctx, apiKeyID, modelIdentity, previousResponseID)
		if err != nil {
			setOpenAIPromptCacheIdentityDecision(c, OpenAIPromptCacheIdentityDecision{
				Reason: OpenAIPromptCacheIdentityReasonRedisError,
				Source: "previous_response_alias",
			})
			logOpenAIAutoPromptCacheIdentityError("alias_lookup", apiKeyID, modelIdentity, err)
			return ""
		}
		if alias != nil && validOpenAIAutoPromptCacheUUIDv7(alias.Value) && alias.RemainingTTL > 0 {
			stageOpenAIAutoPromptCacheIdentity(c, &openAIAutoPromptCacheIdentity{
				Value:         strings.ToLower(strings.TrimSpace(alias.Value)),
				ModelIdentity: modelIdentity,
				APIKeyID:      apiKeyID,
				ExpiresAt:     aliasDeadlineBase.Add(alias.RemainingTTL),
				Source:        "previous_response_alias",
				Hit:           true,
			})
			setOpenAIPromptCacheIdentityDecision(c, OpenAIPromptCacheIdentityDecision{
				Reason:         OpenAIPromptCacheIdentityReasonRedisHit,
				Source:         "previous_response_alias",
				Hit:            true,
				RemainingTTL:   alias.RemainingTTL,
				IdentitySHA256: hashOpenAIPromptCacheDecisionValue(alias.Value),
			})
			return strings.ToLower(strings.TrimSpace(alias.Value))
		}
		if alias != nil {
			setOpenAIPromptCacheIdentityDecision(c, OpenAIPromptCacheIdentityDecision{
				Reason: OpenAIPromptCacheIdentityReasonInvalidCachedValue,
				Source: "previous_response_alias",
			})
		}
		// A valid response ID identifies the exact prefix continued by this turn.
		// Prefer it over the current delta body so successive turns do not derive
		// a different identity from each new user message when an alias is absent.
		return s.resolveAndStageOpenAIAutoPromptCacheIdentity(
			ctx,
			c,
			store,
			apiKeyID,
			modelIdentity,
			"previous_response",
			openAIPromptCacheIdentitySessionSource("previous_response:"+previousResponseID),
		)
	}

	if prefix, ok := deriveOpenAIPromptCachePrefix(body, modelIdentity); ok {
		shardCount := openAIPromptCacheIdentityShardCount(s)
		storeSource := OpenAIPromptCacheIdentitySource{
			Kind:       OpenAIPromptCacheIdentitySourcePrefix,
			Hash:       prefix.Hash,
			ShardCount: shardCount,
			ShardIndex: shardForSessionHash(sessionHash, shardCount),
		}
		return s.resolveAndStageOpenAIAutoPromptCacheIdentity(ctx, c, store, apiKeyID, modelIdentity, "prefix", storeSource)
	}

	if strings.TrimSpace(sessionHash) != "" {
		return s.resolveAndStageOpenAIAutoPromptCacheIdentity(
			ctx,
			c,
			store,
			apiKeyID,
			modelIdentity,
			"session",
			openAIPromptCacheIdentitySessionSource(sessionHash),
		)
	}
	setOpenAIPromptCacheIdentityDecision(c, OpenAIPromptCacheIdentityDecision{Reason: OpenAIPromptCacheIdentityReasonNoSource})
	return ""
}

func (s *OpenAIGatewayService) resolveAndStageOpenAIAutoPromptCacheIdentity(
	ctx context.Context,
	c *gin.Context,
	store OpenAIPromptCacheIdentityStore,
	apiKeyID int64,
	modelIdentity string,
	sourceKind string,
	storeSource OpenAIPromptCacheIdentitySource,
) string {
	if staged := stagedOpenAIAutoPromptCacheIdentity(c); staged != nil &&
		staged.APIKeyID == apiKeyID && staged.ModelIdentity == modelIdentity &&
		staged.StoreSource == storeSource && validOpenAIAutoPromptCacheUUIDv7(staged.Value) {
		now := time.Now()
		if now.Before(staged.ExpiresAt) {
			reason := OpenAIPromptCacheIdentityReasonRedisMiss
			if staged.Hit {
				reason = OpenAIPromptCacheIdentityReasonRedisHit
			}
			setOpenAIPromptCacheIdentityDecision(c, OpenAIPromptCacheIdentityDecision{
				Reason:         reason,
				Source:         staged.Source,
				Hit:            staged.Hit,
				RemainingTTL:   staged.ExpiresAt.Sub(now),
				PrefixSHA256:   openAIPromptCacheDecisionPrefixHash(staged.StoreSource),
				IdentitySHA256: hashOpenAIPromptCacheDecisionValue(staged.Value),
				ShardCount:     staged.StoreSource.ShardCount,
				ShardIndex:     staged.StoreSource.ShardIndex,
			})
			return staged.Value
		}
	}
	clearStagedOpenAIAutoPromptCacheIdentity(c)
	candidate, err := uuid.NewV7()
	if err != nil {
		setOpenAIPromptCacheIdentityDecision(c, OpenAIPromptCacheIdentityDecision{
			Reason: OpenAIPromptCacheIdentityReasonUUIDError,
			Source: sourceKind,
		})
		logOpenAIAutoPromptCacheIdentityError("uuid_v7", apiKeyID, modelIdentity, err)
		return ""
	}
	deadlineBase := time.Now()
	record, err := store.ResolveOpenAIPromptCacheIdentity(
		ctx,
		apiKeyID,
		modelIdentity,
		storeSource,
		candidate.String(),
		openAIPromptCacheIdentityTTL(s),
	)
	if err != nil {
		setOpenAIPromptCacheIdentityDecision(c, OpenAIPromptCacheIdentityDecision{
			Reason: OpenAIPromptCacheIdentityReasonRedisError,
			Source: sourceKind,
		})
		logOpenAIAutoPromptCacheIdentityError("resolve", apiKeyID, modelIdentity, err)
		return ""
	}
	if record == nil || record.RemainingTTL <= 0 || !validOpenAIAutoPromptCacheUUIDv7(record.Value) {
		setOpenAIPromptCacheIdentityDecision(c, OpenAIPromptCacheIdentityDecision{
			Reason: OpenAIPromptCacheIdentityReasonInvalidCachedValue,
			Source: sourceKind,
		})
		logger.L().Warn("openai.auto_prompt_cache_identity_invalid",
			zap.Int64("api_key_id", apiKeyID),
			zap.String("model", modelIdentity),
			zap.String("source", sourceKind),
		)
		return ""
	}

	value := strings.ToLower(strings.TrimSpace(record.Value))
	stageOpenAIAutoPromptCacheIdentity(c, &openAIAutoPromptCacheIdentity{
		Value:         value,
		ModelIdentity: modelIdentity,
		StoreSource:   storeSource,
		APIKeyID:      apiKeyID,
		ExpiresAt:     deadlineBase.Add(record.RemainingTTL),
		Source:        sourceKind,
		Hit:           record.Hit,
	})
	reason := OpenAIPromptCacheIdentityReasonRedisMiss
	if record.Hit {
		reason = OpenAIPromptCacheIdentityReasonRedisHit
	}
	setOpenAIPromptCacheIdentityDecision(c, OpenAIPromptCacheIdentityDecision{
		Reason:         reason,
		Source:         sourceKind,
		Hit:            record.Hit,
		RemainingTTL:   record.RemainingTTL,
		PrefixSHA256:   openAIPromptCacheDecisionPrefixHash(storeSource),
		IdentitySHA256: hashOpenAIPromptCacheDecisionValue(value),
		ShardCount:     storeSource.ShardCount,
		ShardIndex:     storeSource.ShardIndex,
	})
	logger.L().Debug("openai.auto_prompt_cache_identity_resolved",
		zap.Int64("api_key_id", apiKeyID),
		zap.String("model", modelIdentity),
		zap.String("source", sourceKind),
		zap.Bool("hit", record.Hit),
		zap.Int64("ttl_ms", record.RemainingTTL.Milliseconds()),
		zap.String("identity_sha256", hashSensitiveValueForLog(value)),
	)
	return value
}

func openAIPromptCacheIdentityTTL(s *OpenAIGatewayService) time.Duration {
	if s == nil || s.cfg == nil {
		return 30 * time.Minute
	}
	return s.cfg.Gateway.OpenAIPromptCache.IdentityTTL()
}

func openAIPromptCacheIdentityShardCount(s *OpenAIGatewayService) int {
	if s == nil || s.cfg == nil {
		return 4
	}
	return s.cfg.Gateway.OpenAIPromptCache.ShardCountValue()
}

func shardForSessionHash(sessionHash string, shardCount int) int {
	if shardCount <= 1 {
		return 0
	}
	sum := sha256.Sum256([]byte(strings.TrimSpace(sessionHash)))
	return int(binary.BigEndian.Uint64(sum[:8]) % uint64(shardCount))
}

func openAIPromptCacheIdentitySessionSource(sourceIdentity string) OpenAIPromptCacheIdentitySource {
	sum := sha256.Sum256([]byte(strings.TrimSpace(sourceIdentity)))
	return OpenAIPromptCacheIdentitySource{
		Kind: OpenAIPromptCacheIdentitySourceSession,
		Hash: hex.EncodeToString(sum[:]),
	}
}

func openAIPromptCacheDecisionPrefixHash(source OpenAIPromptCacheIdentitySource) string {
	if source.Kind == OpenAIPromptCacheIdentitySourcePrefix {
		return source.Hash
	}
	return ""
}

func hashOpenAIPromptCacheDecisionValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

// BindStagedOpenAIAutoPromptCacheResponseAlias binds a successful response ID
// to the staged UUID using only the original fixed window's remaining TTL.
func (s *OpenAIGatewayService) BindStagedOpenAIAutoPromptCacheResponseAlias(ctx context.Context, c *gin.Context, responseID string) {
	if s == nil || c == nil || ClassifyOpenAIPreviousResponseIDKind(responseID) != OpenAIPreviousResponseIDKindResponseID {
		return
	}
	staged := stagedOpenAIAutoPromptCacheIdentity(c)
	if staged == nil {
		return
	}
	remainingTTL := time.Until(staged.ExpiresAt)
	if remainingTTL <= 0 {
		return
	}
	store, ok := s.cache.(OpenAIPromptCacheIdentityStore)
	if !ok || store == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	bound, err := store.SetOpenAIPromptCacheResponseAlias(
		ctx,
		staged.APIKeyID,
		staged.ModelIdentity,
		strings.TrimSpace(responseID),
		staged.Value,
		remainingTTL,
	)
	if err != nil {
		logOpenAIAutoPromptCacheIdentityError("alias_bind", staged.APIKeyID, staged.ModelIdentity, err)
		return
	}
	logger.L().Debug("openai.auto_prompt_cache_response_alias_bound",
		zap.Int64("api_key_id", staged.APIKeyID),
		zap.String("model", staged.ModelIdentity),
		zap.Bool("bound", bound),
		zap.Int64("ttl_ms", remainingTTL.Milliseconds()),
		zap.String("response_id_sha256", hashSensitiveValueForLog(responseID)),
	)
}

func resolveOpenAIAutoPromptCacheStableSource(c *gin.Context, body []byte) (string, string) {
	source, value := resolveOpenAIStableSessionSignal(c, body, true)
	if source == "" || value == "" {
		return "", ""
	}
	sourceKind := "body_metadata"
	switch {
	case strings.HasPrefix(source, "header:x-codex-turn-metadata."):
		sourceKind = "header_metadata"
	case strings.HasPrefix(source, "header:"):
		sourceKind = "header"
	case source == "body:conversation":
		sourceKind = "conversation"
	case strings.HasPrefix(source, "body:metadata.user_id."):
		sourceKind = "metadata_user"
	}
	return sourceKind, source + ":" + value
}

func resolveOpenAIAutoPromptCacheContentSource(body []byte) string {
	seed := strings.TrimSpace(deriveOpenAIAnchoredContentSessionSeed(body))
	if seed == "" {
		return ""
	}
	return "content:" + seed
}

func stageOpenAIAutoPromptCacheIdentity(c *gin.Context, identity *openAIAutoPromptCacheIdentity) {
	if c == nil || identity == nil {
		return
	}
	c.Set(openAIAutoPromptCacheIdentityGinKey, identity)
}

func clearStagedOpenAIAutoPromptCacheIdentity(c *gin.Context) {
	if c != nil {
		c.Set(openAIAutoPromptCacheIdentityGinKey, nil)
	}
}

func stagedOpenAIAutoPromptCacheIdentity(c *gin.Context) *openAIAutoPromptCacheIdentity {
	if c == nil {
		return nil
	}
	value, exists := c.Get(openAIAutoPromptCacheIdentityGinKey)
	if !exists {
		return nil
	}
	identity, _ := value.(*openAIAutoPromptCacheIdentity)
	return identity
}

func setOpenAIPromptCacheIdentityDecision(c *gin.Context, decision OpenAIPromptCacheIdentityDecision) {
	if c == nil {
		return
	}
	c.Set(openAIPromptCacheIdentityDecisionGinKey, decision)
}

// GetOpenAIPromptCacheIdentityDecision returns non-sensitive resolution state
// for request-level diagnostics.
func GetOpenAIPromptCacheIdentityDecision(c *gin.Context) (OpenAIPromptCacheIdentityDecision, bool) {
	if c == nil {
		return OpenAIPromptCacheIdentityDecision{}, false
	}
	value, exists := c.Get(openAIPromptCacheIdentityDecisionGinKey)
	if !exists {
		return OpenAIPromptCacheIdentityDecision{}, false
	}
	decision, ok := value.(OpenAIPromptCacheIdentityDecision)
	return decision, ok
}

func validOpenAIAutoPromptCacheUUIDv7(value string) bool {
	trimmed := strings.ToLower(strings.TrimSpace(value))
	parsed, err := uuid.Parse(trimmed)
	return err == nil && parsed.Version() == uuid.Version(7) && parsed.String() == trimmed
}

func injectStagedOpenAIAutoPromptCacheIdentity(c *gin.Context, account *Account, body []byte) ([]byte, bool, error) {
	if c == nil || account == nil || account.Platform != PlatformOpenAI ||
		(account.Type != AccountTypeOAuth && account.Type != AccountTypeAPIKey) ||
		isOpenAIResponsesCompactPath(c) ||
		strings.TrimSpace(gjson.GetBytes(body, "prompt_cache_key").String()) != "" {
		return body, false, nil
	}
	identity := stagedOpenAIAutoPromptCacheIdentity(c)
	if identity == nil || !validOpenAIAutoPromptCacheUUIDv7(identity.Value) || time.Now().After(identity.ExpiresAt) {
		return body, false, nil
	}
	patched, err := sjson.SetBytes(body, "prompt_cache_key", identity.Value)
	if err != nil {
		return nil, false, err
	}
	return patched, true, nil
}

func logOpenAIAutoPromptCacheIdentityError(operation string, apiKeyID int64, model string, err error) {
	logger.L().Warn("openai.auto_prompt_cache_identity_error",
		zap.String("operation", operation),
		zap.Int64("api_key_id", apiKeyID),
		zap.String("model", model),
		zap.Error(err),
	)
}
