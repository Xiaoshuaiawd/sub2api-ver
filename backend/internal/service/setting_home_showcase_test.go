package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestIsHomeShowcaseEnabled_OnlyForSelectedTemplate(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		content string
		want    bool
	}{
		{content: HomeShowcaseContentMarker, want: true},
		{content: "  " + HomeShowcaseContentMarker + "  ", want: true},
		{content: "<h1>Custom</h1>"},
		{content: ""},
	} {
		svc := NewSettingService(&codexPolicyMigrationRepoStub{values: map[string]string{
			SettingKeyHomeContent: tc.content,
		}}, &config.Config{})
		require.Equal(t, tc.want, svc.IsHomeShowcaseEnabled(ctx), tc.content)
	}
	missing := NewSettingService(&codexPolicyMigrationRepoStub{values: map[string]string{}}, &config.Config{})
	require.False(t, missing.IsHomeShowcaseEnabled(ctx))
}
