package service

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai_compat"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const openAIPromptCacheBreakpointInjectionGinKey = "openai_prompt_cache_breakpoint_injection"

type openAIPromptCacheBreakpointDecision struct {
	Injected     bool
	Reason       string
	InputIndex   int
	ContentIndex int
}

type openAIPromptCacheBreakpointInjection struct {
	InputIndex   int
	ContentIndex int
}

func injectOpenAIPromptCacheBreakpoint(
	cfg *config.Config,
	account *Account,
	finalModel string,
	body []byte,
) ([]byte, openAIPromptCacheBreakpointDecision, error) {
	decision := openAIPromptCacheBreakpointDecision{InputIndex: -1, ContentIndex: -1}
	if cfg != nil && !cfg.Gateway.OpenAIPromptCache.ExplicitBreakpointsEnabledValue() {
		decision.Reason = "disabled"
		return body, decision, nil
	}
	if account == nil || account.Platform != PlatformOpenAI || account.Type != AccountTypeAPIKey {
		decision.Reason = "unsupported_account"
		return body, decision, nil
	}
	if openai_compat.ResolveResponsesSupport(account.Extra) != openai_compat.ResponsesSupportYes {
		decision.Reason = "unknown_responses_capability"
		return body, decision, nil
	}
	modelFamily := canonicalOpenAIPromptCacheModel(finalModel)
	if !strings.HasPrefix(modelFamily, "gpt-5.6-") {
		decision.Reason = "unsupported_model"
		return body, decision, nil
	}
	if len(body) == 0 || !gjson.ValidBytes(body) || !gjson.GetBytes(body, "input").IsArray() || gjson.GetBytes(body, "messages").Exists() {
		decision.Reason = "unsupported_body"
		return body, decision, nil
	}
	if openAIRequestHasPromptCachePolicy(body) {
		decision.Reason = "client_policy"
		return body, decision, nil
	}

	inputIndex, contentIndex := lastOpenAIPromptCacheBreakpointTarget(body)
	if inputIndex < 0 {
		decision.Reason = "no_supported_block"
		return body, decision, nil
	}

	patched := body
	var err error
	if contentIndex < 0 {
		text := gjson.GetBytes(body, fmt.Sprintf("input.%d.content", inputIndex)).String()
		patched, err = sjson.SetBytes(patched, fmt.Sprintf("input.%d.content", inputIndex), []map[string]any{{
			"type": "input_text",
			"text": text,
		}})
		if err != nil {
			return nil, decision, fmt.Errorf("normalize prompt cache breakpoint content: %w", err)
		}
		contentIndex = 0
	}
	patched, err = sjson.SetBytes(patched, "prompt_cache_options.mode", "explicit")
	if err != nil {
		return nil, decision, fmt.Errorf("set prompt_cache_options: %w", err)
	}
	path := fmt.Sprintf("input.%d.content.%d.prompt_cache_breakpoint.mode", inputIndex, contentIndex)
	patched, err = sjson.SetBytes(patched, path, "explicit")
	if err != nil {
		return nil, decision, fmt.Errorf("set prompt cache breakpoint: %w", err)
	}
	decision.Injected = true
	decision.Reason = "injected"
	decision.InputIndex = inputIndex
	decision.ContentIndex = contentIndex
	return patched, decision, nil
}

func lastOpenAIPromptCacheBreakpointTarget(body []byte) (int, int) {
	input := gjson.GetBytes(body, "input")
	lastInput, lastContent := -1, -1
	for inputIndex, item := range input.Array() {
		role := strings.ToLower(strings.TrimSpace(item.Get("role").String()))
		if role != "system" && role != "developer" {
			continue
		}
		content := item.Get("content")
		if content.Type == gjson.String && strings.TrimSpace(content.String()) != "" {
			lastInput, lastContent = inputIndex, -1
			continue
		}
		if !content.IsArray() {
			continue
		}
		for contentIndex, block := range content.Array() {
			blockType := strings.ToLower(strings.TrimSpace(block.Get("type").String()))
			if blockType != "input_text" && blockType != "text" {
				continue
			}
			if strings.TrimSpace(block.Get("text").String()) == "" {
				continue
			}
			lastInput, lastContent = inputIndex, contentIndex
		}
	}
	return lastInput, lastContent
}

func openAIRequestHasPromptCachePolicy(body []byte) bool {
	var value any
	if len(body) == 0 || json.Unmarshal(body, &value) != nil {
		return false
	}
	return containsOpenAIPromptCachePolicy(value)
}

func containsOpenAIPromptCachePolicy(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if key == "prompt_cache_options" || key == "prompt_cache_breakpoint" {
				return true
			}
			if containsOpenAIPromptCachePolicy(child) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if containsOpenAIPromptCachePolicy(child) {
				return true
			}
		}
	}
	return false
}

func stageOpenAIPromptCacheBreakpointInjection(c *gin.Context, decision openAIPromptCacheBreakpointDecision) {
	if c == nil {
		return
	}
	if !decision.Injected {
		c.Set(openAIPromptCacheBreakpointInjectionGinKey, nil)
		setOpenAIPromptCacheBreakpointDecisionReason(c, decision.Reason)
		return
	}
	c.Set(openAIPromptCacheBreakpointInjectionGinKey, &openAIPromptCacheBreakpointInjection{
		InputIndex:   decision.InputIndex,
		ContentIndex: decision.ContentIndex,
	})
	setOpenAIPromptCacheBreakpointDecisionReason(c, decision.Reason)
}

func setOpenAIPromptCacheBreakpointDecisionReason(c *gin.Context, reason string) {
	if c == nil {
		return
	}
	decision, ok := GetOpenAIPromptCacheIdentityDecision(c)
	if !ok {
		return
	}
	decision.BreakpointReason = strings.TrimSpace(reason)
	setOpenAIPromptCacheIdentityDecision(c, decision)
}

func stagedOpenAIPromptCacheBreakpointInjection(c *gin.Context) *openAIPromptCacheBreakpointInjection {
	if c == nil {
		return nil
	}
	value, ok := c.Get(openAIPromptCacheBreakpointInjectionGinKey)
	if !ok {
		return nil
	}
	injection, _ := value.(*openAIPromptCacheBreakpointInjection)
	return injection
}
