//go:build unit

package handler

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestBuildVcodeCatalogOnlyIncludesBindableGroupsAndAllowedModels(t *testing.T) {
	available := []service.Group{
		{ID: 1, Name: "standard"},
		{ID: 3, Name: "restricted", ModelAllowlist: service.GroupModelAllowlist{Enabled: true, Models: []string{"gpt-allowed"}}},
		{ID: 4, Name: "claude-code-only", ClaudeCodeOnly: true},
	}
	plaza := []service.PlazaGroup{
		{ID: 1, Name: "standard", Platform: "openai", Models: []service.PlazaModel{{Name: "gpt-one", Platform: "openai"}}},
		{ID: 2, Name: "subscription-only", Platform: "openai", Models: []service.PlazaModel{{Name: "gpt-hidden", Platform: "openai"}}},
		{ID: 3, Name: "restricted", Platform: "openai", Models: []service.PlazaModel{{Name: "gpt-allowed", Platform: "openai"}, {Name: "gpt-blocked", Platform: "openai"}}},
		{ID: 4, Name: "claude-code-only", Platform: "anthropic", Models: []service.PlazaModel{{Name: "claude-hidden", Platform: "anthropic"}}},
	}

	got := buildVcodeCatalog(available, plaza, nil)
	require.Len(t, got, 2)
	require.Equal(t, int64(1), got[0].ID)
	require.Equal(t, []vcodeCatalogModel{{ID: "gpt-one", Platform: "openai"}}, got[0].Models)
	require.Equal(t, int64(3), got[1].ID)
	require.Equal(t, []vcodeCatalogModel{{ID: "gpt-allowed", Platform: "openai"}}, got[1].Models)
}

func TestBuildVcodeCatalogPreservesUserRateAndSameModelAcrossGroups(t *testing.T) {
	available := []service.Group{{ID: 7}, {ID: 8}}
	plaza := []service.PlazaGroup{
		{ID: 7, Name: "a", RateMultiplier: 1, Models: []service.PlazaModel{{Name: "same", Platform: "anthropic"}}},
		{ID: 8, Name: "b", RateMultiplier: 2, Models: []service.PlazaModel{{Name: "same", Platform: "anthropic"}}},
	}
	got := buildVcodeCatalog(available, plaza, map[int64]float64{8: 0.5})
	require.Len(t, got, 2)
	require.Nil(t, got[0].UserRateMultiplier)
	require.Equal(t, 0.5, *got[1].UserRateMultiplier)
	require.Equal(t, "same", got[0].Models[0].ID)
	require.Equal(t, "same", got[1].Models[0].ID)
}

func TestBuildVcodeCatalogOmitsImageAndVideoOnlyModels(t *testing.T) {
	available := []service.Group{{ID: 9}}
	plaza := []service.PlazaGroup{{
		ID: 9, Name: "mixed", Platform: "openai",
		Models: []service.PlazaModel{
			{Name: "gpt-text", Platform: "openai", Pricing: &service.ChannelModelPricing{BillingMode: service.BillingModeToken}},
			{Name: "gpt-image", Platform: "openai", Pricing: &service.ChannelModelPricing{BillingMode: service.BillingModeImage}},
			{Name: "grok-video", Platform: "grok", Pricing: &service.ChannelModelPricing{BillingMode: service.BillingModeVideo}},
		},
	}}
	got := buildVcodeCatalog(available, plaza, nil)
	require.Len(t, got, 1)
	require.Equal(t, []vcodeCatalogModel{{ID: "gpt-text", Platform: "openai"}}, got[0].Models)
}
