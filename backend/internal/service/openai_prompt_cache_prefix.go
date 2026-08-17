package service

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
)

const openAIPromptCachePrefixVersion = 1

type openAIPromptCachePrefix struct {
	Hash string
}

type openAICanonicalPromptPrefix struct {
	Version           int                     `json:"version"`
	ModelFamily       string                  `json:"model_family"`
	Instructions      []openAICanonicalPrompt `json:"instructions,omitempty"`
	Tools             []json.RawMessage       `json:"tools,omitempty"`
	TextFormat        json.RawMessage         `json:"text_format,omitempty"`
	Reasoning         json.RawMessage         `json:"reasoning,omitempty"`
	ToolChoice        json.RawMessage         `json:"tool_choice,omitempty"`
	ParallelToolCalls *bool                   `json:"parallel_tool_calls,omitempty"`
}

type openAICanonicalPrompt struct {
	Role    string            `json:"role"`
	Content []json.RawMessage `json:"content"`
}

// deriveOpenAIPromptCachePrefix produces the same stable-prefix fingerprint
// for semantically equivalent Chat Completions and Responses requests. It
// intentionally excludes conversation content and request/session metadata.
func deriveOpenAIPromptCachePrefix(body []byte, finalModel string) (openAIPromptCachePrefix, bool) {
	var root map[string]json.RawMessage
	if len(body) == 0 || json.Unmarshal(body, &root) != nil {
		return openAIPromptCachePrefix{}, false
	}

	canonical := openAICanonicalPromptPrefix{
		Version:     openAIPromptCachePrefixVersion,
		ModelFamily: canonicalOpenAIPromptCacheModel(finalModel),
	}
	if canonical.ModelFamily == "" {
		return openAIPromptCachePrefix{}, false
	}

	if raw := root["instructions"]; hasNonEmptyJSONValue(raw) {
		if content := canonicalOpenAIPromptContent(raw); len(content) > 0 {
			canonical.Instructions = append(canonical.Instructions, openAICanonicalPrompt{
				Role:    "instructions",
				Content: content,
			})
		}
	}

	messages := root["messages"]
	if !hasNonEmptyJSONValue(messages) {
		messages = root["input"]
	}
	canonical.Instructions = append(canonical.Instructions, canonicalOpenAIStableMessages(messages)...)

	canonical.Tools = canonicalOpenAITools(root["tools"], root["functions"])
	canonical.TextFormat = canonicalOpenAITextFormat(root)

	if raw := canonicalJSON(root["reasoning"]); hasNonEmptyJSONValue(raw) {
		canonical.Reasoning = raw
	}
	canonical.ToolChoice = canonicalOpenAIToolChoice(root)
	if raw := root["parallel_tool_calls"]; len(bytes.TrimSpace(raw)) > 0 {
		var value bool
		if json.Unmarshal(raw, &value) == nil {
			canonical.ParallelToolCalls = &value
		}
	}

	if len(canonical.Instructions) == 0 && len(canonical.Tools) == 0 && len(canonical.TextFormat) == 0 {
		return openAIPromptCachePrefix{}, false
	}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return openAIPromptCachePrefix{}, false
	}
	sum := sha256.Sum256(encoded)
	return openAIPromptCachePrefix{Hash: hex.EncodeToString(sum[:])}, true
}

func canonicalOpenAIPromptCacheModel(model string) string {
	if known := normalizeKnownOpenAICodexModel(model); known != "" {
		return known
	}
	model = strings.ToLower(lastOpenAIModelSegment(model))
	return strings.TrimSpace(model)
}

func canonicalOpenAIStableMessages(raw json.RawMessage) []openAICanonicalPrompt {
	var messages []json.RawMessage
	if json.Unmarshal(raw, &messages) != nil {
		return nil
	}
	out := make([]openAICanonicalPrompt, 0, len(messages))
	for _, message := range messages {
		var fields map[string]json.RawMessage
		if json.Unmarshal(message, &fields) != nil {
			continue
		}
		var role string
		_ = json.Unmarshal(fields["role"], &role)
		role = strings.ToLower(strings.TrimSpace(role))
		if role != "system" && role != "developer" {
			continue
		}
		content := canonicalOpenAIPromptContent(fields["content"])
		if len(content) == 0 {
			continue
		}
		out = append(out, openAICanonicalPrompt{Role: role, Content: content})
	}
	return out
}

func canonicalOpenAIPromptContent(raw json.RawMessage) []json.RawMessage {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		if strings.TrimSpace(text) == "" {
			return nil
		}
		return []json.RawMessage{mustCanonicalJSON(map[string]any{"type": "text", "text": text})}
	}

	var blocks []json.RawMessage
	if json.Unmarshal(raw, &blocks) != nil {
		return nil
	}
	out := make([]json.RawMessage, 0, len(blocks))
	for _, block := range blocks {
		var blockText string
		if json.Unmarshal(block, &blockText) == nil {
			if strings.TrimSpace(blockText) != "" {
				out = append(out, mustCanonicalJSON(map[string]any{"type": "text", "text": blockText}))
			}
			continue
		}

		var fields map[string]json.RawMessage
		if json.Unmarshal(block, &fields) != nil {
			continue
		}
		var blockType string
		_ = json.Unmarshal(fields["type"], &blockType)
		switch strings.ToLower(strings.TrimSpace(blockType)) {
		case "text", "input_text", "output_text":
			var value string
			_ = json.Unmarshal(fields["text"], &value)
			if strings.TrimSpace(value) != "" {
				out = append(out, mustCanonicalJSON(map[string]any{"type": "text", "text": value}))
			}
		default:
			delete(fields, "prompt_cache_breakpoint")
			if normalized := mustCanonicalJSON(fields); hasNonEmptyJSONValue(normalized) {
				out = append(out, normalized)
			}
		}
	}
	return out
}

func canonicalOpenAITools(toolsRaw, functionsRaw json.RawMessage) []json.RawMessage {
	var tools []json.RawMessage
	_ = json.Unmarshal(toolsRaw, &tools)
	var functions []json.RawMessage
	_ = json.Unmarshal(functionsRaw, &functions)

	out := make([]json.RawMessage, 0, len(tools)+len(functions))
	for _, tool := range tools {
		if normalized := canonicalOpenAITool(tool, false); len(normalized) > 0 {
			out = append(out, normalized)
		}
	}
	for _, function := range functions {
		if normalized := canonicalOpenAITool(function, true); len(normalized) > 0 {
			out = append(out, normalized)
		}
	}
	return out
}

func canonicalOpenAITool(raw json.RawMessage, legacyFunction bool) json.RawMessage {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return nil
	}
	var toolType string
	_ = json.Unmarshal(fields["type"], &toolType)
	toolType = strings.ToLower(strings.TrimSpace(toolType))
	if legacyFunction {
		toolType = "function"
	}
	if toolType != "function" {
		return canonicalJSON(raw)
	}

	functionFields := fields
	if nested := fields["function"]; len(nested) > 0 {
		var decoded map[string]json.RawMessage
		if json.Unmarshal(nested, &decoded) != nil {
			return nil
		}
		functionFields = decoded
	}
	result := map[string]any{"type": "function"}
	for _, name := range []string{"name", "description", "parameters"} {
		if value, ok := decodeCanonicalJSONValue(functionFields[name]); ok {
			if name != "description" || value != "" {
				result[name] = value
			}
		}
	}
	strict := false
	_ = json.Unmarshal(functionFields["strict"], &strict)
	result["strict"] = strict
	return mustCanonicalJSON(result)
}

func canonicalOpenAITextFormat(root map[string]json.RawMessage) json.RawMessage {
	raw := root["response_format"]
	if !hasNonEmptyJSONValue(raw) {
		var text map[string]json.RawMessage
		if json.Unmarshal(root["text"], &text) == nil {
			raw = text["format"]
		}
	}
	if !hasNonEmptyJSONValue(raw) {
		return nil
	}

	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return canonicalJSON(raw)
	}
	var formatType string
	_ = json.Unmarshal(fields["type"], &formatType)
	if formatType == "json_schema" {
		if nested := fields["json_schema"]; hasNonEmptyJSONValue(nested) {
			var schema map[string]json.RawMessage
			if json.Unmarshal(nested, &schema) == nil {
				schema["type"] = json.RawMessage(`"json_schema"`)
				return mustCanonicalJSON(schema)
			}
		}
	}
	return mustCanonicalJSON(fields)
}

func canonicalOpenAIToolChoice(root map[string]json.RawMessage) json.RawMessage {
	if raw := root["tool_choice"]; hasNonEmptyJSONValue(raw) {
		return canonicalJSON(raw)
	}
	raw := root["function_call"]
	if !hasNonEmptyJSONValue(raw) {
		return nil
	}
	var name string
	if json.Unmarshal(raw, &name) == nil {
		encoded, _ := json.Marshal(name)
		return encoded
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return nil
	}
	var functionName string
	_ = json.Unmarshal(fields["name"], &functionName)
	if strings.TrimSpace(functionName) == "" {
		return nil
	}
	return mustCanonicalJSON(map[string]any{"type": "function", "name": functionName})
}

func canonicalJSON(raw json.RawMessage) json.RawMessage {
	value, ok := decodeCanonicalJSONValue(raw)
	if !ok {
		return nil
	}
	return mustCanonicalJSON(value)
}

func decodeCanonicalJSONValue(raw json.RawMessage) (any, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if decoder.Decode(&value) != nil {
		return nil, false
	}
	return value, true
}

func mustCanonicalJSON(value any) json.RawMessage {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	return encoded
}

func hasNonEmptyJSONValue(raw json.RawMessage) bool {
	raw = bytes.TrimSpace(raw)
	return len(raw) > 0 && !bytes.Equal(raw, []byte("null")) && !bytes.Equal(raw, []byte(`""`)) && !bytes.Equal(raw, []byte("[]")) && !bytes.Equal(raw, []byte("{}"))
}
