package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type messageStorageSettingRepo struct {
	SettingRepository
	mu     sync.Mutex
	values map[string]string
	setErr error
}

func (r *messageStorageSettingRepo) GetValue(_ context.Context, key string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	value, ok := r.values[key]
	if !ok {
		return "", ErrSettingNotFound
	}
	return value, nil
}

func (r *messageStorageSettingRepo) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	values := make(map[string]string, len(keys))
	for _, key := range keys {
		if value, ok := r.values[key]; ok {
			values[key] = value
		}
	}
	return values, nil
}

func (r *messageStorageSettingRepo) SetMultiple(_ context.Context, values map[string]string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.setErr != nil {
		return r.setErr
	}
	for key, value := range values {
		r.values[key] = value
	}
	return nil
}

type fakeMessageStorageRepository struct {
	mu      sync.Mutex
	pending []MessageCaptureMetadata
	bodies  []StoredMessageBody
	states  []string
	block   chan struct{}
	entered chan struct{}
}

func (f *fakeMessageStorageRepository) CreatePending(_ context.Context, m MessageCaptureMetadata) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pending = append(f.pending, m)
	return nil
}
func (f *fakeMessageStorageRepository) StoreBody(_ context.Context, b StoredMessageBody) error {
	if f.block != nil {
		if f.entered != nil {
			select {
			case f.entered <- struct{}{}:
			default:
			}
		}
		<-f.block
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.bodies = append(f.bodies, b)
	return nil
}
func (f *fakeMessageStorageRepository) UpdateBodyState(_ context.Context, _ int64, _, state string, _ int64, _, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.states = append(f.states, state)
	return nil
}
func (f *fakeMessageStorageRepository) GetDetail(context.Context, int64, bool) (*MessageCaptureDetail, error) {
	return nil, ErrMessageCaptureNotFound
}
func (f *fakeMessageStorageRepository) GetSummaries(context.Context, []int64) (map[int64]MessageCaptureSummary, error) {
	return map[int64]MessageCaptureSummary{}, nil
}
func (f *fakeMessageStorageRepository) EnsurePartitions(context.Context, time.Time) error { return nil }
func (f *fakeMessageStorageRepository) DropPartitionsBefore(context.Context, time.Time) (int64, error) {
	return 0, nil
}
func (f *fakeMessageStorageRepository) MarkStalePendingFailed(context.Context, time.Time) (int64, error) {
	return 0, nil
}
func (f *fakeMessageStorageRepository) ExpireBefore(context.Context, time.Time, int) (int64, error) {
	return 0, nil
}

func messageStorageTestConfig() *config.Config {
	return &config.Config{Gateway: config.GatewayConfig{MessageStorage: config.GatewayMessageStorageConfig{
		Enabled: true, RetentionDays: 7, WorkerCount: 1, QueueSize: 1, DBMaxOpenConns: 1, MaxBodyBytes: 1024,
	}}}
}

func TestMessageStorageRuntimeSettingsUseConfigFallbackAndPersistedOverride(t *testing.T) {
	cfg := messageStorageTestConfig()
	cfg.Gateway.MessageStorage.Enabled = false
	svc := NewMessageStorageService(&fakeMessageStorageRepository{}, cfg)
	require.False(t, svc.Enabled())
	require.Equal(t, 7, svc.RetentionDays())

	settingsRepo := &messageStorageSettingRepo{values: map[string]string{
		SettingKeyMessageStorageEnabled:       "true",
		SettingKeyMessageStorageRetentionDays: "3",
	}}
	svc.SetSettingService(context.Background(), NewSettingService(settingsRepo, cfg))

	require.True(t, svc.Enabled())
	require.Equal(t, 3, svc.RetentionDays())
}

func TestMessageStorageUpdateSettingsPersistsBeforeChangingRuntimeState(t *testing.T) {
	cfg := messageStorageTestConfig()
	settingsRepo := &messageStorageSettingRepo{
		values: map[string]string{},
		setErr: errors.New("database unavailable"),
	}
	svc := NewMessageStorageService(&fakeMessageStorageRepository{}, cfg)
	svc.SetSettingService(context.Background(), NewSettingService(settingsRepo, cfg))

	err := svc.UpdateSettings(context.Background(), false, 2)
	require.ErrorContains(t, err, "database unavailable")
	require.True(t, svc.Enabled())
	require.Equal(t, 7, svc.RetentionDays())

	settingsRepo.mu.Lock()
	settingsRepo.setErr = nil
	settingsRepo.mu.Unlock()
	require.NoError(t, svc.UpdateSettings(context.Background(), false, 2))
	require.False(t, svc.Enabled())
	require.Equal(t, 2, svc.RetentionDays())
}

func TestMessageStorageRefreshSettingsObservesAnotherInstance(t *testing.T) {
	cfg := messageStorageTestConfig()
	settingsRepo := &messageStorageSettingRepo{values: map[string]string{
		SettingKeyMessageStorageEnabled:       "true",
		SettingKeyMessageStorageRetentionDays: "7",
	}}
	svc := NewMessageStorageService(&fakeMessageStorageRepository{}, cfg)
	svc.SetSettingService(context.Background(), NewSettingService(settingsRepo, cfg))

	settingsRepo.mu.Lock()
	settingsRepo.values[SettingKeyMessageStorageEnabled] = "false"
	settingsRepo.values[SettingKeyMessageStorageRetentionDays] = "4"
	settingsRepo.mu.Unlock()
	svc.refreshSettings(context.Background())

	require.False(t, svc.Enabled())
	require.Equal(t, 4, svc.RetentionDays())
}

func TestMessageStorageStartsCleanupWhenRuntimeStorageIsDisabled(t *testing.T) {
	cfg := messageStorageTestConfig()
	cfg.Gateway.MessageStorage.Enabled = false
	svc := NewMessageStorageService(&fakeMessageStorageRepository{}, cfg)
	svc.Start()
	require.True(t, svc.started)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, svc.Stop(ctx))
}

func TestMessageStorageWorkerCompressesAndStoresBothBodies(t *testing.T) {
	repo := &fakeMessageStorageRepository{}
	svc := NewMessageStorageService(repo, messageStorageTestConfig())
	svc.Start()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = svc.Stop(ctx)
	})
	capture := NewMessageCaptureSession(MessageCaptureConfig{MaxBodyBytes: 1024, MemoryBudgetBytes: 4096, SpoolDirectory: t.TempDir()})
	_, _ = capture.RequestWriter().Write([]byte(`{"request":true}`))
	_, _ = capture.ResponseWriter().Write([]byte(`{"response":true}`))
	result := svc.Enqueue(context.Background(), 42, "req-42", capture.Artifact())
	require.True(t, result.Accepted)
	require.Eventually(t, func() bool {
		repo.mu.Lock()
		defer repo.mu.Unlock()
		return len(repo.bodies) == 2 && len(repo.states) == 2
	}, time.Second, 10*time.Millisecond)
	repo.mu.Lock()
	defer repo.mu.Unlock()
	require.NotEqual(t, []byte(`{"request":true}`), repo.bodies[0].Payload)
	require.Equal(t, []string{BodyStateAvailable, BodyStateAvailable}, repo.states)
}

func TestMessageStorageQueueFullDoesNotWaitForWorker(t *testing.T) {
	repo := &fakeMessageStorageRepository{block: make(chan struct{}), entered: make(chan struct{}, 1)}
	svc := NewMessageStorageService(repo, messageStorageTestConfig())
	svc.Start()
	newArtifact := func() MessageCaptureArtifact {
		return NewMessageCaptureSession(MessageCaptureConfig{MaxBodyBytes: 64, MemoryBudgetBytes: 256, SpoolDirectory: t.TempDir()}).Artifact()
	}
	require.True(t, svc.Enqueue(context.Background(), 1, "1", newArtifact()).Accepted)
	select {
	case <-repo.entered:
	case <-time.After(time.Second):
		t.Fatal("worker did not start")
	}
	require.True(t, svc.Enqueue(context.Background(), 2, "2", newArtifact()).Accepted)
	result := svc.Enqueue(context.Background(), 3, "3", newArtifact())
	require.False(t, result.Accepted)
	require.Equal(t, "queue_full", result.Code)
	close(repo.block)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, svc.Stop(ctx))
}
