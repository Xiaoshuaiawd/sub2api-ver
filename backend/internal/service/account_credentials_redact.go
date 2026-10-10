package service

import "strings"

// SensitiveCredentialKeys 列出 Account.Credentials JSON map 中绝不允许返回到前端的子键。
// dto 层做响应脱敏、service 层做更新合并都引用此清单——新增凭证类型时务必同步。
var SensitiveCredentialKeys = []string{
	// OAuth
	"access_token", "refresh_token", "id_token", "agent_private_key",
	// API Key 类
	"api_key", "session_key", "cookie",
	// Grok Web SSO / password (must never persist or echo after Build OAuth)
	"password", "sso_token", "sso", "sso-rw", "clearTextPassword",
	// 云服务凭据
	"aws_secret_access_key", "aws_session_token",
	"service_account_json", "service_account", "private_key",
}

var sensitiveCredentialKeySet = func() map[string]struct{} {
	m := make(map[string]struct{}, len(SensitiveCredentialKeys))
	for _, k := range SensitiveCredentialKeys {
		m[k] = struct{}{}
	}
	return m
}()

// IsSensitiveCredentialKey 判断指定键是否为敏感凭证子键。
func IsSensitiveCredentialKey(key string) bool {
	_, ok := sensitiveCredentialKeySet[key]
	return ok
}

// MergePreservingSensitiveCreds 把 incoming 写入 existing 之上，但敏感子键采用"incoming 没提供就保留 existing"
// 的语义。返回新的 map，不修改入参。
//
// 用途：前端编辑账号通常采用"全对象 PUT"模式；脱敏后前端 spread 旧 credentials 时不会带上敏感键，
// 直接覆盖会清空已有 token。此函数保证：
//   - 非敏感键：完全由 incoming 决定（用户可以编辑、删除非敏感字段）。
//   - 敏感键：incoming 显式提供则覆盖（用户主动旋转 token），否则保留 existing。
func MergePreservingSensitiveCreds(existing, incoming map[string]any) map[string]any {
	out := make(map[string]any, len(incoming)+len(SensitiveCredentialKeys))
	for k, v := range incoming {
		out[k] = v
	}
	for _, key := range SensitiveCredentialKeys {
		if _, hasIncoming := incoming[key]; hasIncoming {
			continue
		}
		if existingVal, ok := existing[key]; ok {
			out[key] = existingVal
		}
	}
	return out
}

// MergePreservingAPIKeyBaseURLs extends the redacted-key update contract to
// API-key endpoints. Omitted URLs keep their stored values; a supplied URL
// replaces it. Adaptive endpoint updates merge per protocol so changing one
// hidden URL cannot reset the others.
func MergePreservingAPIKeyBaseURLs(platform string, existing, incoming map[string]any) map[string]any {
	out := MergePreservingSensitiveCreds(existing, incoming)
	_, baseSupplied := incoming["base_url"]
	if !baseSupplied {
		if value, present := existing["base_url"]; present {
			out["base_url"] = value
		}
	}

	provided, supplied := incoming["api_base_urls"]
	updates, _ := provided.(map[string]any)
	merged := make(map[string]any, len(updates)+3)
	if stored, ok := existing["api_base_urls"].(map[string]any); ok {
		for key, value := range stored {
			merged[key] = value
		}
	}
	if supplied {
		if len(updates) == 0 {
			merged = map[string]any{} // Explicit empty map clears Adaptive endpoints.
		} else {
			for key, value := range updates {
				if value == nil {
					delete(merged, key)
				} else {
					merged[key] = value
				}
			}
		}
	}
	oldProtocol, _ := existing["api_protocol"].(string)
	if oldProtocol == "" {
		oldProtocol = APIProtocolChatCompletions
	}
	newProtocol, _ := incoming["api_protocol"].(string)
	if newProtocol == "" {
		newProtocol = oldProtocol
	}
	profile := LookupProviderProfile(platform)
	oldMode, _ := existing["account_mode"].(string)
	newMode, _ := incoming["account_mode"].(string)
	if newMode == "" {
		newMode = oldMode
	}

	// A fixed-protocol account may have a custom relay only in base_url.
	// Carry it into that protocol's adaptive slot without exposing it to the UI.
	if newProtocol == APIProtocolAdaptive && oldProtocol != APIProtocolAdaptive && !(supplied && len(updates) == 0) {
		if _, overridden := updates[oldProtocol]; !overridden {
			if value, ok := existing["base_url"].(string); ok && strings.TrimSpace(value) != "" {
				merged[oldProtocol] = value
			}
		}
	}
	// On leaving Adaptive, the selected protocol's stored endpoint becomes the
	// fixed URL. The incoming empty map clears only the now-unused protocol map.
	if !baseSupplied && oldProtocol == APIProtocolAdaptive && newProtocol != APIProtocolAdaptive {
		selectedURL := ""
		if stored, ok := existing["api_base_urls"].(map[string]any); ok {
			if value, ok := stored[newProtocol].(string); ok && strings.TrimSpace(value) != "" {
				selectedURL = value
			}
		}
		if selectedURL == "" && newProtocol == APIProtocolChatCompletions {
			selectedURL, _ = existing["base_url"].(string)
		}
		if strings.TrimSpace(selectedURL) == "" && profile != nil {
			selectedURL = profile.DefaultBaseURL(newMode, newProtocol)
		}
		if selectedURL != "" {
			out["base_url"] = selectedURL
		}
	}
	// A mode change updates explicit official defaults while preserving custom
	// relays. Empty/missing endpoints continue using runtime profile fallbacks.
	if profile != nil && oldMode != newMode {
		baseProtocol := newProtocol
		if baseProtocol == APIProtocolAdaptive {
			baseProtocol = APIProtocolChatCompletions
		}
		if !baseSupplied {
			if value, ok := out["base_url"].(string); ok && value == profile.DefaultBaseURL(oldMode, baseProtocol) {
				out["base_url"] = profile.DefaultBaseURL(newMode, baseProtocol)
			}
		}
		for protocol, raw := range merged {
			if _, overridden := updates[protocol]; overridden {
				continue
			}
			if value, ok := raw.(string); ok && value == profile.DefaultBaseURL(oldMode, protocol) {
				if replacement := profile.DefaultBaseURL(newMode, protocol); replacement != "" {
					merged[protocol] = replacement
				} else {
					delete(merged, protocol)
				}
			}
		}
	}
	if supplied || len(merged) > 0 {
		out["api_base_urls"] = merged
	}
	return out
}
