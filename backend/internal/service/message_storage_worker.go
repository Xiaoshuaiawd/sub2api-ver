package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/klauspost/compress/zstd"
)

const messageStorageSettingsRefreshInterval = 30 * time.Second

type messageStorageJob struct {
	usageLogID int64
	requestID  string
	createdAt  time.Time
	artifact   MessageCaptureArtifact
}

type MessageStorageMetrics struct {
	Submitted atomic.Int64
	Completed atomic.Int64
	Failed    atomic.Int64
	QueueFull atomic.Int64
}

type MessageStorageService struct {
	repo           MessageStorageRepository
	cfg            config.GatewayMessageStorageConfig
	jobs           chan messageStorageJob
	dbSlots        chan struct{}
	metrics        MessageStorageMetrics
	enabled        atomic.Bool
	retentionDays  atomic.Int64
	settingService *SettingService
	mu             sync.Mutex
	started        bool
	stopped        bool
	wg             sync.WaitGroup
	stopCleanup    chan struct{}
}

func NewMessageStorageService(repo MessageStorageRepository, cfg *config.Config) *MessageStorageService {
	storageCfg := config.GatewayMessageStorageConfig{}
	if cfg != nil {
		storageCfg = cfg.Gateway.MessageStorage
	}
	if storageCfg.WorkerCount <= 0 {
		storageCfg.WorkerCount = 1
	}
	if storageCfg.QueueSize <= 0 {
		storageCfg.QueueSize = storageCfg.WorkerCount
	}
	if storageCfg.DBMaxOpenConns <= 0 {
		storageCfg.DBMaxOpenConns = 1
	}
	if storageCfg.RetentionDays <= 0 {
		storageCfg.RetentionDays = 7
	}
	svc := &MessageStorageService{
		repo: repo, cfg: storageCfg, jobs: make(chan messageStorageJob, storageCfg.QueueSize),
		dbSlots: make(chan struct{}, storageCfg.DBMaxOpenConns), stopCleanup: make(chan struct{}),
	}
	svc.retentionDays.Store(int64(storageCfg.RetentionDays))
	svc.enabled.Store(storageCfg.Enabled)
	return svc
}

func (s *MessageStorageService) SetSettingService(ctx context.Context, settingService *SettingService) {
	s.settingService = settingService
	s.refreshSettings(ctx)
}

func (s *MessageStorageService) Enabled() bool { return s != nil && s.enabled.Load() }

func (s *MessageStorageService) RetentionDays() int { return int(s.retentionDays.Load()) }

func (s *MessageStorageService) refreshSettings(ctx context.Context) {
	if s == nil || s.settingService == nil {
		return
	}
	enabled, days := s.settingService.GetMessageStorageSettings(ctx, s.Enabled(), s.RetentionDays())
	s.enabled.Store(enabled)
	s.retentionDays.Store(int64(days))
}

func (s *MessageStorageService) UpdateSettings(ctx context.Context, enabled bool, days int) error {
	if days < 1 || days > 30 {
		return fmt.Errorf("message storage retention days must be between 1-30")
	}
	if s.settingService != nil {
		if err := s.settingService.SetMessageStorageSettings(ctx, enabled, days); err != nil {
			return err
		}
	}
	s.enabled.Store(enabled)
	s.retentionDays.Store(int64(days))
	return nil
}

func (s *MessageStorageService) UpdateRetentionDays(ctx context.Context, days int) error {
	return s.UpdateSettings(ctx, s.Enabled(), days)
}

func (s *MessageStorageService) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started || s.stopped || s.repo == nil {
		return
	}
	s.started = true
	partitionCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	_ = s.repo.EnsurePartitions(partitionCtx, time.Now())
	cancel()
	for i := 0; i < s.cfg.WorkerCount; i++ {
		s.wg.Add(1)
		go s.worker()
	}
	s.wg.Add(1)
	go s.cleanupLoop()
	if s.settingService != nil {
		s.wg.Add(1)
		go s.settingsRefreshLoop()
	}
}

func (s *MessageStorageService) Enqueue(ctx context.Context, usageLogID int64, requestID string, artifact MessageCaptureArtifact) MessageEnqueueResult {
	if !s.Enabled() || s.repo == nil || usageLogID <= 0 {
		_ = artifact.Cleanup()
		return MessageEnqueueResult{Code: "disabled"}
	}
	now := time.Now().UTC()
	metadata := MessageCaptureMetadata{
		UsageLogID: usageLogID, RequestID: requestID,
		RequestState: pendingState(artifact.Request.State), ResponseState: pendingState(artifact.Response.State),
		RequestRawBytes: artifact.Request.RawBytes, ResponseRawBytes: artifact.Response.RawBytes,
		RequestSHA256: artifact.Request.SHA256, ResponseSHA256: artifact.Response.SHA256,
		ExpiresAt: now.AddDate(0, 0, s.RetentionDays()),
	}
	metaCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	err := s.repo.CreatePending(metaCtx, metadata)
	cancel()
	if err != nil {
		_ = artifact.Cleanup()
		s.metrics.Failed.Add(1)
		return MessageEnqueueResult{Code: "metadata_failed"}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		_ = artifact.Cleanup()
		return MessageEnqueueResult{Code: "stopped"}
	}
	job := messageStorageJob{usageLogID: usageLogID, requestID: requestID, createdAt: now, artifact: artifact}
	select {
	case s.jobs <- job:
		s.metrics.Submitted.Add(1)
		return MessageEnqueueResult{Accepted: true}
	default:
		s.metrics.QueueFull.Add(1)
		_ = artifact.Cleanup()
		failCtx, failCancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
		_ = s.repo.UpdateBodyState(failCtx, usageLogID, MessageBodyTypeRequest, BodyStateFailed, 0, "queue_full", "message storage queue is full")
		_ = s.repo.UpdateBodyState(failCtx, usageLogID, MessageBodyTypeResponse, BodyStateFailed, 0, "queue_full", "message storage queue is full")
		failCancel()
		return MessageEnqueueResult{Code: "queue_full"}
	}
}

func (s *MessageStorageService) settingsRefreshLoop() {
	defer s.wg.Done()
	ticker := time.NewTicker(messageStorageSettingsRefreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			s.refreshSettings(ctx)
			cancel()
		case <-s.stopCleanup:
			return
		}
	}
}

func pendingState(state string) string {
	switch state {
	case BodyStateAvailable, BodyStatePartial, BodyStatePending, "":
		return BodyStatePending
	default:
		return state
	}
}

func (s *MessageStorageService) worker() {
	defer s.wg.Done()
	encoder, err := zstd.NewWriter(nil, zstd.WithEncoderConcurrency(1), zstd.WithEncoderLevel(zstd.SpeedFastest))
	if err != nil {
		for job := range s.jobs {
			s.failJob(job, "compression_init", err)
			_ = job.artifact.Cleanup()
		}
		return
	}
	defer encoder.Close()
	for job := range s.jobs {
		s.processJob(job, encoder)
	}
}

func (s *MessageStorageService) processJob(job messageStorageJob, encoder *zstd.Encoder) {
	defer func() { _ = job.artifact.Cleanup() }()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	failed := false
	for _, body := range []struct {
		kind     string
		artifact BodyArtifact
	}{{MessageBodyTypeRequest, job.artifact.Request}, {MessageBodyTypeResponse, job.artifact.Response}} {
		if body.artifact.State == BodyStateTooLarge || body.artifact.State == BodyStateFailed || body.artifact.State == BodyStateDisabled {
			_ = s.withDBSlot(ctx, func() error {
				return s.repo.UpdateBodyState(ctx, job.usageLogID, body.kind, body.artifact.State, 0, body.artifact.State, "body capture unavailable")
			})
			continue
		}
		compressed, err := compressArtifact(body.artifact, encoder)
		if err != nil {
			failed = true
			_ = s.withDBSlot(ctx, func() error {
				return s.repo.UpdateBodyState(ctx, job.usageLogID, body.kind, BodyStateFailed, 0, "compression_failed", err.Error())
			})
			continue
		}
		err = s.withDBSlot(ctx, func() error {
			return s.repo.StoreBody(ctx, StoredMessageBody{CreatedAt: job.createdAt, UsageLogID: job.usageLogID, BodyType: body.kind, Payload: compressed, RawBytes: body.artifact.RawBytes, StoredBytes: int64(len(compressed))})
		})
		if err != nil {
			failed = true
			_ = s.withDBSlot(ctx, func() error {
				return s.repo.UpdateBodyState(ctx, job.usageLogID, body.kind, BodyStateFailed, 0, "database_failed", err.Error())
			})
			continue
		}
		finalState := BodyStateAvailable
		if body.artifact.State == BodyStatePartial {
			finalState = BodyStatePartial
		}
		if err := s.withDBSlot(ctx, func() error {
			return s.repo.UpdateBodyState(ctx, job.usageLogID, body.kind, finalState, int64(len(compressed)), "", "")
		}); err != nil {
			failed = true
		}
	}
	if failed {
		s.metrics.Failed.Add(1)
	} else {
		s.metrics.Completed.Add(1)
	}
}

func compressArtifact(artifact BodyArtifact, encoder *zstd.Encoder) ([]byte, error) {
	var reader io.Reader = bytes.NewReader(artifact.Bytes)
	var file *os.File
	if artifact.FilePath != "" {
		var err error
		file, err = os.Open(artifact.FilePath)
		if err != nil {
			return nil, err
		}
		defer file.Close()
		reader = file
	}
	var dst bytes.Buffer
	encoder.Reset(&dst)
	if _, err := io.Copy(encoder, reader); err != nil {
		return nil, err
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}
	return dst.Bytes(), nil
}

func (s *MessageStorageService) withDBSlot(ctx context.Context, fn func() error) error {
	select {
	case s.dbSlots <- struct{}{}:
		defer func() { <-s.dbSlots }()
	case <-ctx.Done():
		return ctx.Err()
	}
	return fn()
}

func (s *MessageStorageService) failJob(job messageStorageJob, code string, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	message := fmt.Sprintf("%s: %v", code, err)
	_ = s.repo.UpdateBodyState(ctx, job.usageLogID, MessageBodyTypeRequest, BodyStateFailed, 0, code, message)
	_ = s.repo.UpdateBodyState(ctx, job.usageLogID, MessageBodyTypeResponse, BodyStateFailed, 0, code, message)
	s.metrics.Failed.Add(1)
}

func (s *MessageStorageService) Stop(ctx context.Context) error {
	s.mu.Lock()
	if !s.stopped {
		s.stopped = true
		close(s.jobs)
		close(s.stopCleanup)
	}
	s.mu.Unlock()
	done := make(chan struct{})
	go func() { s.wg.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *MessageStorageService) cleanupLoop() {
	defer s.wg.Done()
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case now := <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			_ = s.repo.EnsurePartitions(ctx, now)
			partitionCutoff := now.UTC().AddDate(0, 0, -s.RetentionDays()).Truncate(24 * time.Hour)
			_, _ = s.repo.DropPartitionsBefore(ctx, partitionCutoff)
			_, _ = s.repo.MarkStalePendingFailed(ctx, now.Add(-30*time.Minute))
			for {
				n, err := s.repo.ExpireBefore(ctx, now, 1000)
				if err != nil || n == 0 {
					break
				}
			}
			cancel()
		case <-s.stopCleanup:
			return
		}
	}
}

func (s *MessageStorageService) GetDetail(ctx context.Context, usageLogID int64, includeBodies bool) (*MessageCaptureDetail, error) {
	detail, err := s.repo.GetDetail(ctx, usageLogID, includeBodies)
	if err != nil || !includeBodies {
		return detail, err
	}
	maxBytes := s.cfg.MaxBodyBytes
	if maxBytes <= 0 {
		maxBytes = 16 * 1024 * 1024
	}
	for _, body := range []*MessageBodyDetail{&detail.Request, &detail.Response} {
		if len(body.Payload) == 0 {
			continue
		}
		decoder, err := zstd.NewReader(bytes.NewReader(body.Payload), zstd.WithDecoderConcurrency(1))
		if err != nil {
			return nil, fmt.Errorf("open zstd body: %w", err)
		}
		raw, readErr := io.ReadAll(io.LimitReader(decoder, maxBytes+1))
		decoder.Close()
		if readErr != nil {
			return nil, fmt.Errorf("decompress body: %w", readErr)
		}
		if int64(len(raw)) > maxBytes {
			return nil, fmt.Errorf("decompressed body exceeds limit")
		}
		body.Body = string(raw)
		body.Payload = nil
	}
	return detail, nil
}

func (s *MessageStorageService) GetSummaries(ctx context.Context, usageLogIDs []int64) (map[int64]MessageCaptureSummary, error) {
	return s.repo.GetSummaries(ctx, usageLogIDs)
}
