package service

import (
	"net/http"
	"regexp"
	"strings"
	"unicode"

	"github.com/gin-gonic/gin"
)

// 上游错误文案里若回显了模型标识符，通常是渠道/账号映射后的真实上游模型。
// 原样透传给客户端等于暴露映射关系（客户端可反推真实模型），因此识别到后
// 统一归一成 5xx 通用错误。
//
// 只影响下发给客户端的响应：ops 错误日志仍在更早的位置记录了原始上游错误，
// 管理员可据此定位。
const (
	// UpstreamModelLeakClientStatus 是识别到模型名泄露时下发的状态码，与
	// mapUpstreamError 对同一条文案（"Upstream service temporarily unavailable"）
	// 使用的 502 保持一致。
	UpstreamModelLeakClientStatus = http.StatusBadGateway
	// UpstreamModelLeakClientMessage 是识别到模型名泄露时下发给客户端的统一文案。
	UpstreamModelLeakClientMessage = "Upstream service temporarily unavailable"
	UpstreamModelLeakClientType    = "upstream_error"
)

// UpstreamModelLeakClientError 返回模型名泄露时下发给客户端的统一错误
// （status, type, message），供 handler 层以协议正确的形式回写。
func UpstreamModelLeakClientError() (int, string, string) {
	return UpstreamModelLeakClientStatus, UpstreamModelLeakClientType, UpstreamModelLeakClientMessage
}

var (
	// 锚定 "model"/"models" 之后的 token，覆盖
	// "unknown provider for model gpt-5.6-luna"、"model: gpt-5.6-luna"、"model `x`" 等写法。
	upstreamErrorModelKeywordPattern = regexp.MustCompile(
		`(?i)\bmodels?\b\s*[:=]?\s*["'\x60]?([A-Za-z0-9][A-Za-z0-9._:\-]{1,})["'\x60]?`,
	)
	// 已知厂商/家族前缀开头的 token，覆盖没有 "model" 字样的回显，
	// 如 "gpt-5.6-luna is not supported"。整段 token 仍需通过 looksLikeUpstreamModelID，
	// 所以 "command not found" 这类普通英文不会命中。
	upstreamErrorModelFamilyPattern = regexp.MustCompile(
		`(?i)\b(?:gpt|chatgpt|claude|gemini|grok|llama|mistral|mixtral|qwen|deepseek|glm|moonshot|kimi|` +
			`command|ernie|hunyuan|doubao|minimax|abab|gemma|phi|nova|titan|falcon|internlm|baichuan|` +
			`solar|sonnet|opus|haiku|o[1-9])[A-Za-z0-9._:\-]*`,
	)
)

// upstreamErrorModelStopwords 是 "model" 之后常见但不构成模型标识符的英文单词，
// 避免把 "model is required"、"invalid model"、"model not found" 这类正常错误归一成 5xx。
var upstreamErrorModelStopwords = map[string]struct{}{
	"a": {}, "an": {}, "and": {}, "are": {}, "available": {}, "be": {}, "can": {}, "cannot": {},
	"could": {}, "did": {}, "do": {}, "does": {}, "error": {}, "exist": {}, "exists": {}, "failed": {},
	"failure": {}, "field": {}, "for": {}, "found": {}, "from": {}, "if": {}, "in": {}, "invalid": {},
	"is": {}, "list": {}, "missing": {}, "must": {}, "name": {}, "no": {}, "not": {}, "not_found": {},
	"of": {}, "on": {}, "or": {}, "parameter": {}, "required": {}, "should": {}, "the": {}, "to": {},
	"type": {}, "unknown": {}, "unsupported": {}, "value": {}, "was": {}, "were": {}, "with": {},
	"would": {},
}

// looksLikeUpstreamModelID 判断 token 是否像模型标识符：
// 长度 >= 2，含分隔符（- . _ :）或数字，且不是常见停用词。
func looksLikeUpstreamModelID(token string) bool {
	t := strings.ToLower(strings.Trim(strings.TrimSpace(token), `"'`+"`"))
	if len(t) < 2 {
		return false
	}
	if _, stop := upstreamErrorModelStopwords[t]; stop {
		return false
	}
	if strings.ContainsAny(t, "-._:") {
		return true
	}
	return strings.IndexFunc(t, unicode.IsDigit) >= 0
}

// UpstreamErrorMessageLeaksModel 判断将要下发给客户端的错误文案是否回显了模型名。
func UpstreamErrorMessageLeaksModel(msg string) bool {
	if strings.TrimSpace(msg) == "" {
		return false
	}
	// 厂商前缀命中的整段 token 仍需通过模型标识符校验，
	// 否则 "command not found" 这类普通英文会被误判。
	for _, token := range upstreamErrorModelFamilyPattern.FindAllString(msg, -1) {
		if looksLikeUpstreamModelID(token) {
			return true
		}
	}
	for _, match := range upstreamErrorModelKeywordPattern.FindAllStringSubmatch(msg, -1) {
		if len(match) > 1 && looksLikeUpstreamModelID(match[1]) {
			return true
		}
	}
	return false
}

// writeUpstreamModelLeakError 以 OpenAI 错误体形状回写归一化后的统一错误，
// 隐藏上游回显的真实模型名。Responses/OpenAI 协议路径使用。
func writeUpstreamModelLeakError(c *gin.Context) {
	if c == nil {
		return
	}
	MarkResponseCommitted(c)
	c.JSON(UpstreamModelLeakClientStatus, gin.H{
		"error": gin.H{
			"type":    UpstreamModelLeakClientType,
			"message": UpstreamModelLeakClientMessage,
		},
	})
}

// writeUpstreamModelLeakAnthropicError 以 Anthropic 错误体形状回写统一错误，
// 供 Anthropic Messages 原生路径使用，避免协议形状不匹配。
func writeUpstreamModelLeakAnthropicError(c *gin.Context) {
	if c == nil {
		return
	}
	MarkResponseCommitted(c)
	c.JSON(UpstreamModelLeakClientStatus, gin.H{
		"type": "error",
		"error": gin.H{
			"type":    UpstreamModelLeakClientType,
			"message": UpstreamModelLeakClientMessage,
		},
	})
}
