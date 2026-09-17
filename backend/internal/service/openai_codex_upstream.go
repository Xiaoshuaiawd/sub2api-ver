package service

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// openAICodexUpstreamURLOverride 返回管理员配置的自定义 Codex 上游地址；未配置时为空。
//
// 读取顺序与其它网关转发设置一致：优先 SettingService 的 60s 进程内缓存，
// 拿不到（例如测试里未注入 settingService）时回落到同一份进程缓存快照。
func (s *OpenAIGatewayService) openAICodexUpstreamURLOverride(ctx context.Context) string {
	if s == nil {
		return ""
	}
	if s.settingService != nil {
		return strings.TrimSpace(s.settingService.GetOpenAICodexUpstreamURL(ctx))
	}
	if cached, ok := gatewayForwardingCache.Load().(*cachedGatewayForwardingSettings); ok && cached != nil {
		if cached.expiresAt == 0 || time.Now().UnixNano() < cached.expiresAt {
			return strings.TrimSpace(cached.openAICodexUpstreamURL)
		}
	}
	return ""
}

// resolveOpenAICodexResponsesTarget 解析 Codex Responses 的上游地址。
//
// 未配置自定义地址时返回官方地址；配置后只替换目标 URL——请求体、请求头、
// 鉴权与路径后缀的处理全部保持不变。自定义地址会先按 security.url_allowlist
// 策略做一次出站校验，避免把 Codex 流量打到未授权主机。
func (s *OpenAIGatewayService) resolveOpenAICodexResponsesTarget(ctx context.Context, officialURL string) (string, error) {
	override := s.openAICodexUpstreamURLOverride(ctx)
	if override == "" {
		return officialURL, nil
	}
	normalized, err := s.validateOutboundURL(override)
	if err != nil {
		return "", fmt.Errorf("invalid %s: %w", SettingKeyOpenAICodexUpstreamURL, err)
	}
	return normalized, nil
}
