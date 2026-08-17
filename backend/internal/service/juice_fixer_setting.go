package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/sync/singleflight"
)

// JuiceFixerRule 一条 Juice 值修正规则：命中模型 + reasoning_effort 组合时，
// 把响应中出现的 "Juice" 数值替换为 Value。
type JuiceFixerRule struct {
	Model           string `json:"model"`
	ReasoningEffort string `json:"reasoning_effort"`
	Value           int    `json:"value"`
}

// JuiceFixerSetting Juice 值修正配置（管理员可控）。
type JuiceFixerSetting struct {
	Enabled bool             `json:"enabled"`
	Rules   []JuiceFixerRule `json:"rules"`
}

// MaxJuiceFixerValue 单条规则允许的最大替换值。
const MaxJuiceFixerValue = int(^uint32(0) >> 1)

// DefaultJuiceFixerSetting 返回默认配置（禁用、无规则）。
func DefaultJuiceFixerSetting() *JuiceFixerSetting {
	return &JuiceFixerSetting{Enabled: false, Rules: []JuiceFixerRule{}}
}

// ValidateJuiceFixerSetting 校验规则合法性：模型名必填、值非负、同一
// (model, reasoning_effort) 组合不重复。
func ValidateJuiceFixerSetting(setting *JuiceFixerSetting) error {
	if setting == nil {
		return fmt.Errorf("setting cannot be nil")
	}
	seen := make(map[string]struct{}, len(setting.Rules))
	for i, rule := range setting.Rules {
		model := strings.TrimSpace(rule.Model)
		if model == "" {
			return fmt.Errorf("rules[%d].model is required", i)
		}
		if rule.Value < 0 || rule.Value > MaxJuiceFixerValue {
			return fmt.Errorf("rules[%d].value must be between 0 and %d", i, MaxJuiceFixerValue)
		}
		key := model + "\x00" + strings.TrimSpace(rule.ReasoningEffort)
		if _, ok := seen[key]; ok {
			return fmt.Errorf("duplicate Juice rule for model %q and reasoning_effort %q", model, strings.TrimSpace(rule.ReasoningEffort))
		}
		seen[key] = struct{}{}
	}
	return nil
}

// NormalizeJuiceFixerSetting 校验并裁剪规则字段，返回深拷贝后的配置。
func NormalizeJuiceFixerSetting(setting *JuiceFixerSetting) (*JuiceFixerSetting, error) {
	if err := ValidateJuiceFixerSetting(setting); err != nil {
		return nil, err
	}
	normalized := &JuiceFixerSetting{Enabled: setting.Enabled, Rules: make([]JuiceFixerRule, len(setting.Rules))}
	for i, rule := range setting.Rules {
		normalized.Rules[i] = JuiceFixerRule{
			Model:           strings.TrimSpace(rule.Model),
			ReasoningEffort: strings.TrimSpace(rule.ReasoningEffort),
			Value:           rule.Value,
		}
	}
	return normalized, nil
}

// CloneJuiceFixerSetting 返回配置的深拷贝。
func CloneJuiceFixerSetting(setting *JuiceFixerSetting) *JuiceFixerSetting {
	if setting == nil {
		return DefaultJuiceFixerSetting()
	}
	clone := &JuiceFixerSetting{Enabled: setting.Enabled, Rules: make([]JuiceFixerRule, len(setting.Rules))}
	copy(clone.Rules, setting.Rules)
	return clone
}

// FindJuiceFixerValue 在配置中查找 (model, reasoning_effort) 的替换值。
// 未启用或未命中时返回 false。
func FindJuiceFixerValue(setting *JuiceFixerSetting, model, reasoningEffort string) (int, bool) {
	if setting == nil || !setting.Enabled {
		return 0, false
	}
	model = strings.TrimSpace(model)
	reasoningEffort = strings.TrimSpace(reasoningEffort)
	var fallback *int
	for _, rule := range setting.Rules {
		if rule.Model != model {
			continue
		}
		if rule.ReasoningEffort == reasoningEffort {
			return rule.Value, true
		}
		if strings.TrimSpace(rule.ReasoningEffort) == "" {
			value := rule.Value
			fallback = &value
		}
	}
	if fallback != nil {
		return *fallback, true
	}
	return 0, false
}

// cachedJuiceFixerSetting 进程内缓存（60s TTL，零锁热路径）
type cachedJuiceFixerSetting struct {
	setting   *JuiceFixerSetting
	expiresAt int64 // unix nano
}

var juiceFixerSettingCache atomic.Value // *cachedJuiceFixerSetting
var juiceFixerSettingSF singleflight.Group

const juiceFixerSettingCacheTTL = 60 * time.Second
const juiceFixerSettingErrorTTL = 5 * time.Second
const juiceFixerSettingDBTimeout = 5 * time.Second

// GetJuiceFixerSetting 获取 Juice 值修正配置。
// 热路径走进程内缓存（60s TTL），DB 错误时 fail-closed（禁用）。
func (s *SettingService) GetJuiceFixerSetting(ctx context.Context) *JuiceFixerSetting {
	if cached, ok := juiceFixerSettingCache.Load().(*cachedJuiceFixerSetting); ok && cached != nil {
		if cached.setting != nil && time.Now().UnixNano() < cached.expiresAt {
			return CloneJuiceFixerSetting(cached.setting)
		}
	}
	// singleflight: 同一时刻只有一个 goroutine 查询 DB，其余复用结果
	result, err, _ := juiceFixerSettingSF.Do("juice_fixer_setting", func() (any, error) {
		// 二次检查，避免排队的 goroutine 重复查询
		if cached, ok := juiceFixerSettingCache.Load().(*cachedJuiceFixerSetting); ok && cached != nil {
			if cached.setting != nil && time.Now().UnixNano() < cached.expiresAt {
				return cached.setting, nil
			}
		}
		dbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), juiceFixerSettingDBTimeout)
		defer cancel()
		value, err := s.settingRepo.GetValue(dbCtx, SettingKeyJuiceFixerSetting)
		if err != nil {
			if !errors.Is(err, ErrSettingNotFound) {
				// fail-closed：DB 错误时缓存禁用配置并快速重试
				juiceFixerSettingCache.Store(&cachedJuiceFixerSetting{
					setting:   DefaultJuiceFixerSetting(),
					expiresAt: time.Now().Add(juiceFixerSettingErrorTTL).UnixNano(),
				})
				return DefaultJuiceFixerSetting(), nil
			}
			value = ""
		}
		setting := DefaultJuiceFixerSetting()
		if strings.TrimSpace(value) != "" {
			if err := json.Unmarshal([]byte(value), setting); err != nil {
				setting = DefaultJuiceFixerSetting()
			}
		}
		if normalized, err := NormalizeJuiceFixerSetting(setting); err == nil {
			setting = normalized
		} else {
			setting = DefaultJuiceFixerSetting()
		}
		juiceFixerSettingCache.Store(&cachedJuiceFixerSetting{
			setting:   setting,
			expiresAt: time.Now().Add(juiceFixerSettingCacheTTL).UnixNano(),
		})
		return setting, nil
	})
	if err != nil {
		return DefaultJuiceFixerSetting()
	}
	if setting, ok := result.(*JuiceFixerSetting); ok {
		return CloneJuiceFixerSetting(setting)
	}
	return DefaultJuiceFixerSetting()
}

// SetJuiceFixerSetting 持久化 Juice 值修正配置，并立即使缓存失效。
func (s *SettingService) SetJuiceFixerSetting(ctx context.Context, setting *JuiceFixerSetting) error {
	normalized, err := NormalizeJuiceFixerSetting(setting)
	if err != nil {
		return err
	}
	data, err := json.Marshal(normalized)
	if err != nil {
		return fmt.Errorf("marshal juice fixer setting: %w", err)
	}
	if err := s.settingRepo.Set(ctx, SettingKeyJuiceFixerSetting, string(data)); err != nil {
		return err
	}
	// 立即让下次读取重新加载，保证热路径快速生效
	juiceFixerSettingSF.Forget("juice_fixer_setting")
	juiceFixerSettingCache.Store(&cachedJuiceFixerSetting{
		setting:   normalized,
		expiresAt: time.Now().Add(juiceFixerSettingCacheTTL).UnixNano(),
	})
	return nil
}
