//go:build unit

package service

import (
	"context"
	"math"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestGetOAuthPurchaseCostCNY_DefaultsToZero(t *testing.T) {
	svc := NewSettingService(newMockSettingRepo(), &config.Config{})

	got, err := svc.GetOAuthPurchaseCostCNY(context.Background())

	require.NoError(t, err)
	require.Equal(t, 0.0, got)
}

func TestSetOAuthPurchaseCostCNY_RoundTrip(t *testing.T) {
	repo := newMockSettingRepo()
	svc := NewSettingService(repo, &config.Config{})

	require.NoError(t, svc.SetOAuthPurchaseCostCNY(context.Background(), 123.456789))

	got, err := svc.GetOAuthPurchaseCostCNY(context.Background())
	require.NoError(t, err)
	require.InDelta(t, 123.456789, got, 1e-9)
	require.Equal(t, "123.45678900", repo.data[SettingKeyOAuthPurchaseCostCNY])
}

func TestSetOAuthPurchaseCostCNY_RejectsInvalidValues(t *testing.T) {
	svc := NewSettingService(newMockSettingRepo(), &config.Config{})

	for _, value := range []float64{-0.01, math.NaN(), math.Inf(1), math.Inf(-1)} {
		err := svc.SetOAuthPurchaseCostCNY(context.Background(), value)
		require.Error(t, err, "value=%v should be rejected", value)
	}
}
