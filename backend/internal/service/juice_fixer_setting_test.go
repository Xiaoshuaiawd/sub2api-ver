//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type juiceFixerSettingRepoStub struct {
	values map[string]string
}

func (s *juiceFixerSettingRepoStub) Get(ctx context.Context, key string) (*Setting, error) {
	panic("unexpected Get call")
}

func (s *juiceFixerSettingRepoStub) GetValue(ctx context.Context, key string) (string, error) {
	if s.values != nil {
		if value, ok := s.values[key]; ok {
			return value, nil
		}
	}
	return "", nil
}

func (s *juiceFixerSettingRepoStub) Set(ctx context.Context, key, value string) error {
	if s.values == nil {
		s.values = map[string]string{}
	}
	s.values[key] = value
	return nil
}

func (s *juiceFixerSettingRepoStub) GetMultiple(ctx context.Context, keys []string) (map[string]string, error) {
	out := make(map[string]string, len(keys))
	for _, key := range keys {
		if value, ok := s.values[key]; ok {
			out[key] = value
		}
	}
	return out, nil
}

func (s *juiceFixerSettingRepoStub) SetMultiple(ctx context.Context, settings map[string]string) error {
	for key, value := range settings {
		if s.values == nil {
			s.values = map[string]string{}
		}
		s.values[key] = value
	}
	return nil
}

func (s *juiceFixerSettingRepoStub) GetAll(ctx context.Context) (map[string]string, error) {
	out := make(map[string]string, len(s.values))
	for key, value := range s.values {
		out[key] = value
	}
	return out, nil
}

func (s *juiceFixerSettingRepoStub) Delete(ctx context.Context, key string) error {
	panic("unexpected Delete call")
}

func newJuiceFixerTestSettingService(repo SettingRepository) *SettingService {
	return NewSettingService(repo, &config.Config{})
}

// resetJuiceFixerSettingCacheForTest 清空包级缓存，避免跨测试污染。
func resetJuiceFixerSettingCacheForTest() {
	juiceFixerSettingSF.Forget("juice_fixer_setting")
	juiceFixerSettingCache.Store(&cachedJuiceFixerSetting{expiresAt: 0})
}

func TestValidateJuiceFixerSettingRejectsInvalidRules(t *testing.T) {
	tests := []*JuiceFixerSetting{
		{Enabled: true, Rules: []JuiceFixerRule{{Model: "", ReasoningEffort: "low", Value: 8}}},
		{Enabled: true, Rules: []JuiceFixerRule{{Model: "gpt", ReasoningEffort: "low", Value: -1}}},
		{Enabled: true, Rules: []JuiceFixerRule{{Model: "gpt", ReasoningEffort: "low", Value: 8}, {Model: "gpt", ReasoningEffort: "low", Value: 9}}},
	}
	for _, cfg := range tests {
		assert.Error(t, ValidateJuiceFixerSetting(cfg))
	}
	assert.NoError(t, ValidateJuiceFixerSetting(&JuiceFixerSetting{Enabled: true, Rules: []JuiceFixerRule{{Model: "gpt", ReasoningEffort: "low", Value: 8}}}))
}

func TestNormalizeJuiceFixerSettingTrimsRules(t *testing.T) {
	cfg, err := NormalizeJuiceFixerSetting(&JuiceFixerSetting{Enabled: true, Rules: []JuiceFixerRule{{Model: " gpt ", ReasoningEffort: " low ", Value: 8}}})
	require.NoError(t, err)
	assert.Equal(t, "gpt", cfg.Rules[0].Model)
	assert.Equal(t, "low", cfg.Rules[0].ReasoningEffort)
	assert.Equal(t, 8, cfg.Rules[0].Value)
}

func TestFindJuiceFixerValue(t *testing.T) {
	setting := &JuiceFixerSetting{
		Enabled: true,
		Rules: []JuiceFixerRule{
			{Model: "gpt-5.6-sol", ReasoningEffort: "low", Value: 8},
			{Model: "gpt-5.6-sol", ReasoningEffort: "", Value: 6},
		},
	}
	value, ok := FindJuiceFixerValue(setting, "gpt-5.6-sol", "low")
	require.True(t, ok)
	assert.Equal(t, 8, value)

	value, ok = FindJuiceFixerValue(setting, "gpt-5.6-sol", "")
	require.True(t, ok)
	assert.Equal(t, 6, value)

	_, ok = FindJuiceFixerValue(setting, "gpt-5.6-sol", "high")
	assert.False(t, ok)

	_, ok = FindJuiceFixerValue(&JuiceFixerSetting{Enabled: false, Rules: setting.Rules}, "gpt-5.6-sol", "low")
	assert.False(t, ok)
}

func TestSettingService_JuiceFixerGetSetRoundTrip(t *testing.T) {
	resetJuiceFixerSettingCacheForTest()
	repo := &juiceFixerSettingRepoStub{}
	svc := newJuiceFixerTestSettingService(repo)
	ctx := context.Background()

	// 默认禁用
	setting := svc.GetJuiceFixerSetting(ctx)
	require.NotNil(t, setting)
	assert.False(t, setting.Enabled)

	// 保存后立即可见
	err := svc.SetJuiceFixerSetting(ctx, &JuiceFixerSetting{
		Enabled: true,
		Rules:   []JuiceFixerRule{{Model: " gpt-5.6-sol ", ReasoningEffort: "low", Value: 8}},
	})
	require.NoError(t, err)

	setting = svc.GetJuiceFixerSetting(ctx)
	assert.True(t, setting.Enabled)
	require.Len(t, setting.Rules, 1)
	assert.Equal(t, "gpt-5.6-sol", setting.Rules[0].Model)

	// 非法规则拒绝保存
	err = svc.SetJuiceFixerSetting(ctx, &JuiceFixerSetting{Enabled: true, Rules: []JuiceFixerRule{{Model: "", Value: 1}}})
	assert.Error(t, err)
}

func TestSettingService_JuiceFixerSettingInvalidJSONFailsClosed(t *testing.T) {
	resetJuiceFixerSettingCacheForTest()
	repo := &juiceFixerSettingRepoStub{values: map[string]string{
		SettingKeyJuiceFixerSetting: `{"enabled":true,"rules":not-json`,
	}}
	svc := newJuiceFixerTestSettingService(repo)
	setting := svc.GetJuiceFixerSetting(context.Background())
	require.NotNil(t, setting)
	assert.False(t, setting.Enabled)
}
