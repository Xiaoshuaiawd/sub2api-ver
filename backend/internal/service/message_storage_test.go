package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

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
