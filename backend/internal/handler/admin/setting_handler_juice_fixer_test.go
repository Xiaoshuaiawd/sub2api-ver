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
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type juiceFixerHandlerRepoStub struct {
	values map[string]string
}

func (s *juiceFixerHandlerRepoStub) Get(ctx context.Context, key string) (*service.Setting, error) {
	panic("unexpected Get call")
}

func (s *juiceFixerHandlerRepoStub) GetValue(ctx context.Context, key string) (string, error) {
	if s.values != nil {
		if value, ok := s.values[key]; ok {
			return value, nil
		}
	}
	return "", nil
}

func (s *juiceFixerHandlerRepoStub) Set(ctx context.Context, key, value string) error {
	if s.values == nil {
		s.values = map[string]string{}
	}
	s.values[key] = value
	return nil
}

func (s *juiceFixerHandlerRepoStub) GetMultiple(ctx context.Context, keys []string) (map[string]string, error) {
	out := make(map[string]string, len(keys))
	for _, key := range keys {
		if value, ok := s.values[key]; ok {
			out[key] = value
		}
	}
	return out, nil
}

func (s *juiceFixerHandlerRepoStub) SetMultiple(ctx context.Context, settings map[string]string) error {
	for key, value := range settings {
		if s.values == nil {
			s.values = map[string]string{}
		}
		s.values[key] = value
	}
	return nil
}

func (s *juiceFixerHandlerRepoStub) GetAll(ctx context.Context) (map[string]string, error) {
	out := make(map[string]string, len(s.values))
	for key, value := range s.values {
		out[key] = value
	}
	return out, nil
}

func (s *juiceFixerHandlerRepoStub) Delete(ctx context.Context, key string) error {
	panic("unexpected Delete call")
}

func TestSettingHandler_JuiceFixerConfig_GetAndUpdate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &juiceFixerHandlerRepoStub{}
	svc := service.NewSettingService(repo, &config.Config{})
	handler := NewSettingHandler(svc, nil, nil, nil, nil, nil, nil)

	// 默认配置：禁用、无规则
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/settings/juice-fixer", nil)
	handler.GetJuiceFixerConfig(c)
	require.Equal(t, http.StatusOK, rec.Code)
	var resp response.Response
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	data, ok := resp.Data.(map[string]any)
	require.True(t, ok)
	require.Equal(t, false, data["enabled"])

	// 更新配置
	body := map[string]any{
		"enabled": true,
		"rules": []map[string]any{
			{"model": "gpt-5.6-sol", "reasoning_effort": "low", "value": 8},
		},
	}
	rawBody, err := json.Marshal(body)
	require.NoError(t, err)

	rec = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/v1/admin/settings/juice-fixer", bytes.NewReader(rawBody))
	c.Request.Header.Set("Content-Type", "application/json")
	handler.UpdateJuiceFixerConfig(c)
	require.Equal(t, http.StatusOK, rec.Code)

	require.JSONEq(t, `{"enabled":true,"rules":[{"model":"gpt-5.6-sol","reasoning_effort":"low","value":8}]}`,
		repo.values[service.SettingKeyJuiceFixerSetting])

	// 再读回
	rec = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/settings/juice-fixer", nil)
	handler.GetJuiceFixerConfig(c)
	require.Equal(t, http.StatusOK, rec.Code)
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	data, ok = resp.Data.(map[string]any)
	require.True(t, ok)
	require.Equal(t, true, data["enabled"])
}

func TestSettingHandler_JuiceFixerConfig_RejectsInvalidRule(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &juiceFixerHandlerRepoStub{}
	svc := service.NewSettingService(repo, &config.Config{})
	handler := NewSettingHandler(svc, nil, nil, nil, nil, nil, nil)

	body := `{"enabled":true,"rules":[{"model":"","reasoning_effort":"low","value":8}]}`
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/v1/admin/settings/juice-fixer", bytes.NewReader([]byte(body)))
	c.Request.Header.Set("Content-Type", "application/json")
	handler.UpdateJuiceFixerConfig(c)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	var resp response.Response
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Contains(t, resp.Message, "rules[0].model is required")
}
