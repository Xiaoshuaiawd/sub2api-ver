package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type usageMessageSettingsRepo struct {
	service.SettingRepository
	values map[string]string
	setErr error
}

func (r *usageMessageSettingsRepo) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	values := make(map[string]string, len(keys))
	for _, key := range keys {
		if value, ok := r.values[key]; ok {
			values[key] = value
		}
	}
	return values, nil
}

func (r *usageMessageSettingsRepo) SetMultiple(_ context.Context, values map[string]string) error {
	if r.setErr != nil {
		return r.setErr
	}
	for key, value := range values {
		r.values[key] = value
	}
	return nil
}

func newUsageMessageSettingsRouter(repo *usageMessageSettingsRepo) (*gin.Engine, *service.MessageStorageService) {
	cfg := &config.Config{Gateway: config.GatewayConfig{MessageStorage: config.GatewayMessageStorageConfig{
		Enabled: true, RetentionDays: 7, WorkerCount: 1, QueueSize: 1, DBMaxOpenConns: 1,
	}}}
	storage := service.NewMessageStorageService(nil, cfg)
	storage.SetSettingService(context.Background(), service.NewSettingService(repo, cfg))
	usage := service.NewUsageService(nil, nil, nil, nil)
	usage.SetMessageStorage(storage)
	handler := NewUsageHandler(usage, nil, nil, nil)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/settings", handler.MessageStorageSettings)
	router.PUT("/settings", handler.UpdateMessageStorageSettings)
	return router, storage
}

func decodeUsageMessageSettingsResponse(t *testing.T, recorder *httptest.ResponseRecorder) struct {
	Data struct {
		Enabled       bool `json:"enabled"`
		RetentionDays int  `json:"retention_days"`
	} `json:"data"`
} {
	t.Helper()
	var payload struct {
		Data struct {
			Enabled       bool `json:"enabled"`
			RetentionDays int  `json:"retention_days"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &payload))
	return payload
}

func TestUsageMessageStorageSettingsReturnsEnabledAndRetention(t *testing.T) {
	router, _ := newUsageMessageSettingsRouter(&usageMessageSettingsRepo{values: map[string]string{
		service.SettingKeyMessageStorageEnabled:       "false",
		service.SettingKeyMessageStorageRetentionDays: "3",
	}})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/settings", nil))

	require.Equal(t, http.StatusOK, recorder.Code)
	payload := decodeUsageMessageSettingsResponse(t, recorder)
	require.False(t, payload.Data.Enabled)
	require.Equal(t, 3, payload.Data.RetentionDays)
	require.Contains(t, recorder.Body.String(), `"enabled":false`)
}

func TestUpdateUsageMessageStorageSettingsUpdatesBothValues(t *testing.T) {
	repo := &usageMessageSettingsRepo{values: map[string]string{}}
	router, storage := newUsageMessageSettingsRouter(repo)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/settings", bytes.NewBufferString(`{"enabled":false,"retention_days":2}`)))

	require.Equal(t, http.StatusOK, recorder.Code)
	payload := decodeUsageMessageSettingsResponse(t, recorder)
	require.False(t, payload.Data.Enabled)
	require.Equal(t, 2, payload.Data.RetentionDays)
	require.False(t, storage.Enabled())
	require.Equal(t, 2, storage.RetentionDays())
	require.Equal(t, "false", repo.values[service.SettingKeyMessageStorageEnabled])
}

func TestUpdateUsageMessageStorageSettingsRequiresEnabled(t *testing.T) {
	router, storage := newUsageMessageSettingsRouter(&usageMessageSettingsRepo{values: map[string]string{}})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/settings", bytes.NewBufferString(`{"retention_days":2}`)))

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.True(t, storage.Enabled())
}

func TestUpdateUsageMessageStorageSettingsKeepsRuntimeStateOnPersistenceFailure(t *testing.T) {
	router, storage := newUsageMessageSettingsRouter(&usageMessageSettingsRepo{
		values: map[string]string{},
		setErr: errors.New("database unavailable"),
	})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/settings", bytes.NewBufferString(`{"enabled":false,"retention_days":2}`)))

	require.Equal(t, http.StatusInternalServerError, recorder.Code)
	require.True(t, storage.Enabled())
	require.Equal(t, 7, storage.RetentionDays())
}
