package service

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/sync/singleflight"
)

// OpenAI 账号用量轮休：
//
// 账号的 codex primary 主窗口（实际为月度，window_minutes=43200，约 30 天）用量
// 达到 100% 后，OpenAI 直接返回 429 并携带 reset_after ≈ 30 天，账号会被限流封到
// 下一次窗口重置。观察到的雪崩模式：流量集中 → 账号逐个触顶 → 调度池缩水 →
// 剩余账号更快触顶 → 全组只剩极少数账号可用、请求全部排队。
//
// 轮休语义：主窗口用量 >= 阈值（默认 80%）后暂停该账号参与调度，
// 把流量让给用量更低的账号，避免账号被持续压到 100% 后整月报废。
// 阈值 0 表示关闭；运行时通过 settings 键 openai_scheduling_usage_rest_threshold_percent
// 动态调整（管理后台可直接改）。
const (
	openAIUsageRestSettingKey       = "openai_scheduling_usage_rest_threshold_percent"
	openAIUsageRestDefaultPercent   = 80
	openAIUsageRestSettingCacheTTL  = 5 * time.Second
	openAIUsageRestSettingDBTimeout = 2 * time.Second

	// openAIUsageRestedFallbackMax 全池账号都被轮休时的兜底放回数量：
	// 放回用量最低的少数账号，保证整组仍有最小可用容量。
	openAIUsageRestedFallbackMax = 5
)

var (
	openAIUsageRestSettingCache atomic.Value // *openAIUsageRestCachedSetting
	openAIUsageRestSettingSF    singleflight.Group
)

type openAIUsageRestCachedSetting struct {
	thresholdPercent int   // 0 = 关闭
	expiresAt        int64 // unix nano
}

func openAIUsageRestRepo(settingService *SettingService) SettingRepository {
	if settingService == nil {
		return nil
	}
	return settingService.settingRepo
}

// openAIUsageRestThresholdPercent 返回当前轮休阈值（0 表示关闭）。
// 读取失败或配置非法时回退默认值，绝不让调度层因此报错。
func openAIUsageRestThresholdPercent(ctx context.Context, settingService *SettingService) int {
	if cached, ok := openAIUsageRestSettingCache.Load().(*openAIUsageRestCachedSetting); ok && cached != nil && time.Now().UnixNano() < cached.expiresAt {
		return cached.thresholdPercent
	}
	result, _, _ := openAIUsageRestSettingSF.Do(openAIUsageRestSettingKey, func() (any, error) {
		if cached, ok := openAIUsageRestSettingCache.Load().(*openAIUsageRestCachedSetting); ok && cached != nil && time.Now().UnixNano() < cached.expiresAt {
			return cached, nil
		}
		threshold := openAIUsageRestDefaultPercent
		if repo := openAIUsageRestRepo(settingService); repo != nil {
			dbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), openAIUsageRestSettingDBTimeout)
			defer cancel()
			if values, err := repo.GetMultiple(dbCtx, []string{openAIUsageRestSettingKey}); err == nil {
				if raw := strings.TrimSpace(values[openAIUsageRestSettingKey]); raw != "" {
					if v, perr := strconv.Atoi(raw); perr == nil && v >= 0 && v <= 100 {
						threshold = v
					} else if perr != nil {
						slog.Warn("usage_rest_threshold_parse_failed", "raw", raw, "error", perr)
					}
				}
			}
		}
		cached := &openAIUsageRestCachedSetting{
			thresholdPercent: threshold,
			expiresAt:        time.Now().Add(openAIUsageRestSettingCacheTTL).UnixNano(),
		}
		openAIUsageRestSettingCache.Store(cached)
		return cached, nil
	})
	if cached, ok := result.(*openAIUsageRestCachedSetting); ok {
		return cached.thresholdPercent
	}
	return 0
}

// openAIAccountPrimaryUsedPercent 返回账号 codex 主窗口用量百分比。
// 优先主窗口字段（codex_primary_used_percent），缺失时回退旧的 7d 字段，
// 与高级调度器 openAIQuotaHeadroomFactor 的解析口径保持一致。
func openAIAccountPrimaryUsedPercent(account *Account) (float64, bool) {
	if account == nil || len(account.Extra) == 0 {
		return 0, false
	}
	return resolveAccountExtraNumber(account.Extra, "codex_primary_used_percent", "codex_7d_used_percent")
}

// openAIAccountUsageRestedByThreshold 报告账号主窗口用量是否达到轮休阈值。
// 仅对 OpenAI OAuth 账号生效（只有这类账号携带 codex 用量快照）。
func openAIAccountUsageRestedByThreshold(account *Account, thresholdPercent int) bool {
	if account == nil || thresholdPercent <= 0 || !account.IsOpenAIOAuthLike() {
		return false
	}
	used, ok := openAIAccountPrimaryUsedPercent(account)
	if !ok {
		return false
	}
	return used >= float64(thresholdPercent)
}