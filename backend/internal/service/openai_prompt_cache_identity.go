package service

import (
	"context"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
	"go.uber.org/zap"
)

const openAIAutoPromptCacheIdentityTTL = 5 * time.Minute

const openAIAutoPromptCacheIdentityGinKey = "openai_auto_prompt_cache_identity"

type openAIAutoPromptCacheIdentity struct {
	Value         string
	ModelIdentity string
	APIKeyID      int64
	ExpiresAt     time.Time
	Source        string
	Hit           bool
}

// ResolveAndStageOpenAIAutoPromptCacheIdentity resolves a fixed-lifetime
// UUIDv7 for native Responses requests that do not provide prompt_cache_key.
// Cache and UUID failures deliberately fail open.
func (s *OpenAIGatewayService) ResolveAndStageOpenAIAutoPromptCacheIdentity(
	ctx context.Context,
	c *gin.Context,
	apiKeyID int64,
	model string,
	body []byte,
) string {
	if s == nil || c == nil || apiKeyID <= 0 || len(body) == 0 || isOpenAIResponsesCompactPath(c) {
		return ""
	}
	if strings.TrimSpace(gjson.GetBytes(body, "prompt_cache_key").String()) != "" {
		return ""
	}
	modelIdentity := strings.ToLower(strings.TrimSpace(model))
	if modelIdentity == "" {
		modelIdentity = strings.ToLower(strings.TrimSpace(gjson.GetBytes(body, "model").String()))
	}
	if modelIdentity == "" {
		return ""
	}
	if staged := stagedOpenAIAutoPromptCacheIdentity(c); staged != nil &&
		staged.APIKeyID == apiKeyID && staged.ModelIdentity == modelIdentity &&
		validOpenAIAutoPromptCacheUUIDv7(staged.Value) && time.Now().Before(staged.ExpiresAt) {
		return staged.Value
	}
	store, ok := s.cache.(OpenAIPromptCacheIdentityStore)
	if !ok || store == nil {
		return ""
	}
	if ctx == nil {
		ctx = context.Background()
	}

	if sourceKind, sourceIdentity := resolveOpenAIAutoPromptCacheStableSource(c, body); sourceIdentity != "" {
		return s.resolveAndStageOpenAIAutoPromptCacheIdentity(ctx, c, store, apiKeyID, modelIdentity, sourceKind, sourceIdentity)
	}

	previousResponseID := strings.TrimSpace(gjson.GetBytes(body, "previous_response_id").String())
	validPreviousResponseID := ClassifyOpenAIPreviousResponseIDKind(previousResponseID) == OpenAIPreviousResponseIDKindResponseID
	if validPreviousResponseID {
		aliasDeadlineBase := time.Now()
		alias, err := store.GetOpenAIPromptCacheResponseAlias(ctx, apiKeyID, modelIdentity, previousResponseID)
		if err != nil {
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
			return strings.ToLower(strings.TrimSpace(alias.Value))
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
			"previous_response:"+previousResponseID,
		)
	}

	if sourceIdentity := resolveOpenAIAutoPromptCacheContentSource(body); sourceIdentity != "" {
		return s.resolveAndStageOpenAIAutoPromptCacheIdentity(ctx, c, store, apiKeyID, modelIdentity, "content", sourceIdentity)
	}
	return ""
}

func (s *OpenAIGatewayService) resolveAndStageOpenAIAutoPromptCacheIdentity(
	ctx context.Context,
	c *gin.Context,
	store OpenAIPromptCacheIdentityStore,
	apiKeyID int64,
	modelIdentity string,
	sourceKind string,
	sourceIdentity string,
) string {
	candidate, err := uuid.NewV7()
	if err != nil {
		logOpenAIAutoPromptCacheIdentityError("uuid_v7", apiKeyID, modelIdentity, err)
		return ""
	}
	deadlineBase := time.Now()
	record, err := store.ResolveOpenAIPromptCacheIdentity(
		ctx,
		apiKeyID,
		modelIdentity,
		sourceIdentity,
		candidate.String(),
		openAIAutoPromptCacheIdentityTTL,
	)
	if err != nil {
		logOpenAIAutoPromptCacheIdentityError("resolve", apiKeyID, modelIdentity, err)
		return ""
	}
	if record == nil || record.RemainingTTL <= 0 || !validOpenAIAutoPromptCacheUUIDv7(record.Value) {
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
		APIKeyID:      apiKeyID,
		ExpiresAt:     deadlineBase.Add(record.RemainingTTL),
		Source:        sourceKind,
		Hit:           record.Hit,
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
	if c != nil {
		for _, header := range append(append([]string(nil), explicitOpenAIHeaderSessionNames...), claudeCodeSessionHeader) {
			if value := sanitizeSessionID(c.GetHeader(header)); value != "" {
				lowerHeader := strings.ToLower(strings.TrimSpace(header))
				return "header", "header:" + lowerHeader + ":" + value
			}
		}
		if metadata := strings.TrimSpace(c.GetHeader("x-codex-turn-metadata")); metadata != "" && gjson.Valid(metadata) {
			for _, field := range []string{"session_id", "thread_id"} {
				if value := sanitizeSessionID(gjson.Get(metadata, field).String()); value != "" {
					return "header_metadata", "header:x-codex-turn-metadata." + field + ":" + value
				}
			}
		}
	}

	conversation := gjson.GetBytes(body, "conversation")
	if conversation.Exists() {
		value := ""
		if conversation.Type == gjson.String {
			value = sanitizeSessionID(conversation.String())
		} else {
			value = sanitizeSessionID(conversation.Get("id").String())
		}
		if value != "" {
			return "conversation", "body:conversation:" + value
		}
	}

	for _, field := range []string{
		"session_id",
		"conversation_id",
		"metadata.session_id",
		"metadata.conversation_id",
		"client_metadata.session_id",
		"client_metadata.thread_id",
	} {
		if value := sanitizeSessionID(gjson.GetBytes(body, field).String()); value != "" {
			return "body_metadata", "body:" + field + ":" + value
		}
	}

	metadataUserID := strings.TrimSpace(gjson.GetBytes(body, "metadata.user_id").String())
	if metadataUserID != "" && gjson.Valid(metadataUserID) {
		for _, field := range []string{"session_id", "conversation_id", "thread_id"} {
			if value := sanitizeSessionID(gjson.Get(metadataUserID, field).String()); value != "" {
				return "metadata_user", "body:metadata.user_id." + field + ":" + value
			}
		}
	}
	return "", ""
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
