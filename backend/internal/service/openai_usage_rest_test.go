//go:build unit

package service

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenAIAccountPrimaryUsedPercent(t *testing.T) {
	t.Run("primary field", func(t *testing.T) {
		account := &Account{Extra: map[string]any{"codex_primary_used_percent": float64(85)}}
		used, ok := openAIAccountPrimaryUsedPercent(account)
		require.True(t, ok)
		require.Equal(t, 85.0, used)
	})

	t.Run("fallback to 7d field", func(t *testing.T) {
		account := &Account{Extra: map[string]any{"codex_7d_used_percent": float64(42)}}
		used, ok := openAIAccountPrimaryUsedPercent(account)
		require.True(t, ok)
		require.Equal(t, 42.0, used)
	})

	t.Run("string value is parsed", func(t *testing.T) {
		account := &Account{Extra: map[string]any{"codex_primary_used_percent": "95"}}
		used, ok := openAIAccountPrimaryUsedPercent(account)
		require.True(t, ok)
		require.Equal(t, 95.0, used)
	})

	t.Run("missing extra", func(t *testing.T) {
		_, ok := openAIAccountPrimaryUsedPercent(&Account{})
		require.False(t, ok)
	})

	t.Run("nil account", func(t *testing.T) {
		_, ok := openAIAccountPrimaryUsedPercent(nil)
		require.False(t, ok)
	})
}

func TestOpenAIAccountUsageRestedByThreshold(t *testing.T) {
	newAccount := func(usedPercent float64) *Account {
		return &Account{
			Platform: PlatformOpenAI,
			Type:     AccountTypeOAuth,
			Extra:    map[string]any{"codex_primary_used_percent": usedPercent},
		}
	}

	t.Run("at threshold is rested", func(t *testing.T) {
		require.True(t, openAIAccountUsageRestedByThreshold(newAccount(80), 80))
	})

	t.Run("above threshold is rested", func(t *testing.T) {
		require.True(t, openAIAccountUsageRestedByThreshold(newAccount(97), 80))
	})

	t.Run("below threshold is not rested", func(t *testing.T) {
		require.False(t, openAIAccountUsageRestedByThreshold(newAccount(60), 80))
	})

	t.Run("threshold 0 disables rest", func(t *testing.T) {
		require.False(t, openAIAccountUsageRestedByThreshold(newAccount(100), 0))
	})

	t.Run("non-OAuth accounts are never rested", func(t *testing.T) {
		apiKey := &Account{
			Platform: PlatformOpenAI,
			Type:     AccountTypeAPIKey,
			Extra:    map[string]any{"codex_primary_used_percent": float64(100)},
		}
		require.False(t, openAIAccountUsageRestedByThreshold(apiKey, 80))
	})

	t.Run("missing usage snapshot is not rested", func(t *testing.T) {
		require.False(t, openAIAccountUsageRestedByThreshold(&Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}, 80))
	})
}

// 用量已达轮休阈值的账号，429 视为配额压力信号：跳过同账号 6-8s 重试，
// 立即交给 failover 换号（实现 2）。
func TestShouldRetryOpenAIOAuth429_UsageRestedSkipsSameAccountRetry(t *testing.T) {
	svc := &OpenAIGatewayService{}

	rested := &Account{
		ID:     1,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Extra:    map[string]any{"codex_primary_used_percent": float64(85)},
	}
	require.False(t, svc.shouldRetryOpenAIOAuth429OnSameAccountWithResponse(rested, http.StatusTooManyRequests, false, http.Header{}, nil),
		"用量 >= 默认阈值 80 的账号收到瞬时 429 不应同账号重试")

	healthy := &Account{
		ID:     2,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Extra:    map[string]any{"codex_primary_used_percent": float64(30)},
	}
	require.True(t, svc.shouldRetryOpenAIOAuth429OnSameAccountWithResponse(healthy, http.StatusTooManyRequests, false, http.Header{}, nil),
		"用量健康的账号仍允许同账号短暂重试")
}

// 主窗口 100% 的 429 必须走 quota 分类（Quota7d），绝不允许同账号重试。
func TestShouldRetryOpenAIOAuth429_ExhaustedQuotaNeverSameAccountRetry(t *testing.T) {
	svc := &OpenAIGatewayService{}
	account := &Account{ID: 3, Platform: PlatformOpenAI, Type: AccountTypeOAuth}

	headers := http.Header{
		"X-Codex-Primary-Used-Percent":      []string{"100"},
		"X-Codex-Primary-Reset-After-Seconds": []string{"2590088"},
	}
	require.False(t, svc.shouldRetryOpenAIOAuth429OnSameAccountWithResponse(account, http.StatusTooManyRequests, false, headers, nil),
		"主窗口 100% 的 429 必须立即切换账号，不做同账号重试")
}