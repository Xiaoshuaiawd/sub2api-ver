//go:build unit

package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type oauthCostSettingRepoStub struct {
	values map[string]string
}

func (s *oauthCostSettingRepoStub) Get(ctx context.Context, key string) (*service.Setting, error) {
	if value, ok := s.values[key]; ok {
		return &service.Setting{Key: key, Value: value}, nil
	}
	return nil, service.ErrSettingNotFound
}

func (s *oauthCostSettingRepoStub) GetValue(ctx context.Context, key string) (string, error) {
	if value, ok := s.values[key]; ok {
		return value, nil
	}
	return "", nil
}

func (s *oauthCostSettingRepoStub) Set(ctx context.Context, key, value string) error {
	if s.values == nil {
		s.values = map[string]string{}
	}
	s.values[key] = value
	return nil
}

func (s *oauthCostSettingRepoStub) GetMultiple(ctx context.Context, keys []string) (map[string]string, error) {
	out := make(map[string]string, len(keys))
	for _, key := range keys {
		if value, ok := s.values[key]; ok {
			out[key] = value
		}
	}
	return out, nil
}

func (s *oauthCostSettingRepoStub) SetMultiple(ctx context.Context, settings map[string]string) error {
	for key, value := range settings {
		if err := s.Set(ctx, key, value); err != nil {
			return err
		}
	}
	return nil
}

func (s *oauthCostSettingRepoStub) GetAll(ctx context.Context) (map[string]string, error) {
	out := make(map[string]string, len(s.values))
	for key, value := range s.values {
		out[key] = value
	}
	return out, nil
}

func (s *oauthCostSettingRepoStub) Delete(ctx context.Context, key string) error {
	delete(s.values, key)
	return nil
}

func TestSettingHandler_OAuthCost_GetDefaultAndPutRoundTrip(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &oauthCostSettingRepoStub{values: map[string]string{}}
	settingService := service.NewSettingService(repo, &config.Config{})
	handler := NewSettingHandler(settingService, nil, nil, nil, nil, nil, nil)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/settings/oauth-cost", nil)
	handler.GetOAuthCost(c)
	require.Equal(t, http.StatusOK, rec.Code)
	requireOAuthPurchaseCostResponse(t, rec.Body.Bytes(), 0)

	rec = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/v1/admin/settings/oauth-cost", bytes.NewReader([]byte(`{"purchase_cost_cny":88.5}`)))
	c.Request.Header.Set("Content-Type", "application/json")
	handler.UpdateOAuthCost(c)
	require.Equal(t, http.StatusOK, rec.Code)
	requireOAuthPurchaseCostResponse(t, rec.Body.Bytes(), 88.5)

	rec = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/settings/oauth-cost", nil)
	handler.GetOAuthCost(c)
	require.Equal(t, http.StatusOK, rec.Code)
	requireOAuthPurchaseCostResponse(t, rec.Body.Bytes(), 88.5)
}

func TestSettingHandler_OAuthCost_RejectsInvalidValues(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewSettingHandler(service.NewSettingService(&oauthCostSettingRepoStub{values: map[string]string{}}, &config.Config{}), nil, nil, nil, nil, nil, nil)

	for _, body := range []string{`{"purchase_cost_cny":-1}`, `{"purchase_cost_cny":"bad"}`} {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodPut, "/api/v1/admin/settings/oauth-cost", bytes.NewReader([]byte(body)))
		c.Request.Header.Set("Content-Type", "application/json")
		handler.UpdateOAuthCost(c)
		require.Equal(t, http.StatusBadRequest, rec.Code, "body=%s", body)
	}
}

func TestUsageHandler_OAuthCostSummary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &adminUsageRepoCapture{
		oauthCostSummary: &usagestats.OAuthCostSummary{
			AccountCount:       3,
			TotalConsumedQuota: 12.5,
			TotalRequests:      7,
			TotalTokens:        900,
		},
	}
	handler := NewUsageHandler(service.NewUsageService(repo, nil, nil, nil), nil, nil, nil)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/usage/oauth-cost-summary", nil)
	handler.OAuthCostSummary(c)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp response.Response
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	data, ok := resp.Data.(map[string]any)
	require.True(t, ok)
	require.Equal(t, float64(3), data["account_count"])
	require.Equal(t, 12.5, data["total_consumed_quota"])
	require.Equal(t, float64(7), data["total_requests"])
	require.Equal(t, float64(900), data["total_tokens"])
	require.True(t, repo.oauthCostSummaryCalled)
}

func requireOAuthPurchaseCostResponse(t *testing.T, body []byte, want float64) {
	t.Helper()
	var resp response.Response
	require.NoError(t, json.Unmarshal(body, &resp))
	data, ok := resp.Data.(map[string]any)
	require.True(t, ok)
	require.Equal(t, want, data["purchase_cost_cny"])
}
