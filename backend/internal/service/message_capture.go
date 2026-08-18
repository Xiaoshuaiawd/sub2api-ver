package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"hash"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

const (
	BodyStatePending   = "pending"
	BodyStateAvailable = "available"
	BodyStateFailed    = "failed"
	BodyStatePartial   = "partial"
	BodyStateTooLarge  = "too_large"
	BodyStateExpired   = "expired"
	BodyStateDisabled  = "disabled"
)

// MessageCaptureConfig is intentionally independent from the config package so
// the capture service remains straightforward to unit test.
type MessageCaptureConfig struct {
	MaxBodyBytes      int64
	MemoryBudgetBytes int64
	SpoolDirectory    string
	SpoolBudgetBytes  int64
}

type BodyArtifact struct {
	State    string
	RawBytes int64
	SHA256   string
	Bytes    []byte
	FilePath string
	cleanup  func() error
}

type MessageCaptureArtifact struct {
	Request  BodyArtifact
	Response BodyArtifact
	Status   int
	Cleanup  func() error
}

type MessageCaptureSession struct {
	request      *captureBody
	response     *captureBody
	ctx          context.Context
	mu           sync.Mutex
	status       int
	finalized    bool
	claimed      bool
	cleanupTimer *time.Timer
}

type MessageCaptureFactory struct {
	config MessageCaptureConfig
	budget *captureBudget
}

type captureBudget struct {
	memory atomic.Int64
	spool  atomic.Int64
	config MessageCaptureConfig
}

func (b *captureBudget) reserveMemory(n int64) bool {
	if n <= 0 {
		return true
	}
	for {
		cur := b.memory.Load()
		if cur+n > b.config.MemoryBudgetBytes {
			return false
		}
		if b.memory.CompareAndSwap(cur, cur+n) {
			return true
		}
	}
}
func (b *captureBudget) releaseMemory(n int64) {
	if n > 0 {
		b.memory.Add(-n)
	}
}
func (b *captureBudget) reserveSpool(n int64) bool {
	if n <= 0 {
		return true
	}
	for {
		cur := b.spool.Load()
		if cur+n > b.config.SpoolBudgetBytes {
			return false
		}
		if b.spool.CompareAndSwap(cur, cur+n) {
			return true
		}
	}
}
func (b *captureBudget) releaseSpool(n int64) {
	if n > 0 {
		b.spool.Add(-n)
	}
}

type captureBody struct {
	mu         sync.Mutex
	state      string
	rawBytes   int64
	hash       hash.Hash
	memory     []byte
	file       *os.File
	filePath   string
	memBytes   int64
	spoolBytes int64
	limit      int64
	budget     *captureBudget
	dir        string
}

func NewMessageCaptureSession(cfg MessageCaptureConfig) *MessageCaptureSession {
	return NewMessageCaptureFactory(cfg).NewSession()
}

func NewMessageCaptureFactory(cfg MessageCaptureConfig) *MessageCaptureFactory {
	if cfg.MaxBodyBytes <= 0 {
		cfg.MaxBodyBytes = 16 * 1024 * 1024
	}
	if cfg.MemoryBudgetBytes <= 0 {
		cfg.MemoryBudgetBytes = 512 * 1024 * 1024
	}
	if cfg.SpoolBudgetBytes <= 0 {
		cfg.SpoolBudgetBytes = 20 * 1024 * 1024 * 1024
	}
	if cfg.SpoolDirectory == "" {
		cfg.SpoolDirectory = filepath.Join(os.TempDir(), "sub2api-message-storage")
	}
	return &MessageCaptureFactory{config: cfg, budget: &captureBudget{config: cfg}}
}

func (f *MessageCaptureFactory) NewSession() *MessageCaptureSession {
	b := f.budget
	cfg := f.config
	return &MessageCaptureSession{request: newCaptureBody(cfg, b), response: newCaptureBody(cfg, b), ctx: context.Background()}
}

func newCaptureBody(cfg MessageCaptureConfig, b *captureBudget) *captureBody {
	return &captureBody{state: BodyStatePending, limit: cfg.MaxBodyBytes, budget: b, dir: cfg.SpoolDirectory, hash: sha256.New()}
}

func (s *MessageCaptureSession) SetContext(ctx context.Context) { s.ctx = ctx }
func (s *MessageCaptureSession) RequestWriter() io.Writer       { return s.request }
func (s *MessageCaptureSession) ResponseWriter() io.Writer      { return s.response }
func (s *MessageCaptureSession) WrapRequestBody(body io.ReadCloser) io.ReadCloser {
	return &captureReadCloser{ReadCloser: body, capture: s.request}
}
func (s *MessageCaptureSession) Request() io.Writer  { return s.request }
func (s *MessageCaptureSession) Response() io.Writer { return s.response }

type captureReadCloser struct {
	io.ReadCloser
	capture io.Writer
}

func (r *captureReadCloser) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	if n > 0 {
		_, _ = r.capture.Write(p[:n])
	}
	return n, err
}

func (s *MessageCaptureSession) Finalize(ctx context.Context) {
	s.mu.Lock()
	if s.finalized {
		s.mu.Unlock()
		return
	}
	s.finalized = true
	s.mu.Unlock()
	if ctx != nil && ctx.Err() != nil && s.response.raw() > 0 {
		s.response.markPartial()
	}
}

func (s *MessageCaptureSession) SetStatus(status int) { s.mu.Lock(); s.status = status; s.mu.Unlock() }

func (s *MessageCaptureSession) Artifact() MessageCaptureArtifact {
	s.mu.Lock()
	status := s.status
	s.mu.Unlock()
	req := s.request.artifact()
	resp := s.response.artifact()
	return MessageCaptureArtifact{Request: req, Response: resp, Status: status, Cleanup: func() error {
		var errs []error
		if err := req.cleanup(); err != nil {
			errs = append(errs, err)
		}
		if err := resp.cleanup(); err != nil {
			errs = append(errs, err)
		}
		return errors.Join(errs...)
	}}
}

// ClaimArtifact transfers cleanup ownership to the storage worker. It returns
// false when another terminal usage record already claimed this HTTP exchange.
func (s *MessageCaptureSession) ClaimArtifact() (MessageCaptureArtifact, bool) {
	s.mu.Lock()
	if s.claimed {
		s.mu.Unlock()
		return MessageCaptureArtifact{}, false
	}
	s.claimed = true
	if s.cleanupTimer != nil {
		s.cleanupTimer.Stop()
		s.cleanupTimer = nil
	}
	s.mu.Unlock()
	return s.Artifact(), true
}

func (s *MessageCaptureSession) CleanupIfUnclaimed() error {
	s.mu.Lock()
	if s.claimed {
		s.mu.Unlock()
		return nil
	}
	// Claim cleanup ownership under the same lock used by ClaimArtifact so the
	// timer cannot delete files while the usage worker is taking the artifact.
	s.claimed = true
	if s.cleanupTimer != nil {
		s.cleanupTimer.Stop()
		s.cleanupTimer = nil
	}
	s.mu.Unlock()
	return s.Artifact().Cleanup()
}

// ScheduleCleanup bounds the lifetime of captures for requests that do not
// create a usage log (for example authentication or validation failures).
func (s *MessageCaptureSession) ScheduleCleanup(after time.Duration) {
	if after <= 0 {
		after = time.Minute
	}
	s.mu.Lock()
	if s.claimed || s.cleanupTimer != nil {
		s.mu.Unlock()
		return
	}
	s.cleanupTimer = time.AfterFunc(after, func() {
		_ = s.CleanupIfUnclaimed()
	})
	s.mu.Unlock()
}

func (b *captureBody) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state == BodyStateTooLarge || b.state == BodyStateFailed {
		return len(p), nil
	}
	if b.rawBytes+int64(len(p)) > b.limit {
		b.state = BodyStateTooLarge
		return len(p), nil
	}
	if len(p) == 0 {
		return 0, nil
	}
	if b.file == nil && b.budget.reserveMemory(int64(len(p))) {
		b.memory = append(b.memory, p...)
		b.memBytes += int64(len(p))
	} else {
		if b.file == nil {
			if err := b.startSpoolLocked(); err != nil {
				b.state = BodyStateFailed
				return len(p), nil
			}
		}
		if !b.budget.reserveSpool(int64(len(p))) {
			b.state = BodyStateFailed
			return len(p), nil
		}
		if _, err := b.file.Write(p); err != nil {
			b.budget.releaseSpool(int64(len(p)))
			b.state = BodyStateFailed
			return len(p), nil
		}
		b.spoolBytes += int64(len(p))
	}
	_, _ = b.hash.Write(p)
	b.rawBytes += int64(len(p))
	return len(p), nil
}

func (b *captureBody) startSpoolLocked() error {
	if err := os.MkdirAll(b.dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(b.dir, "capture-*")
	if err != nil {
		return err
	}
	if len(b.memory) > 0 {
		if !b.budget.reserveSpool(int64(len(b.memory))) {
			_ = f.Close()
			_ = os.Remove(f.Name())
			return errors.New("spool budget exceeded")
		}
		if _, err := f.Write(b.memory); err != nil {
			b.budget.releaseSpool(int64(len(b.memory)))
			_ = f.Close()
			_ = os.Remove(f.Name())
			return err
		}
		b.spoolBytes += int64(len(b.memory))
		b.budget.releaseMemory(b.memBytes)
		b.memBytes = 0
		b.memory = nil
	}
	b.file, b.filePath = f, f.Name()
	return nil
}

func (b *captureBody) raw() int64 { b.mu.Lock(); defer b.mu.Unlock(); return b.rawBytes }
func (b *captureBody) markPartial() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state == BodyStateAvailable || b.state == BodyStatePending {
		b.state = BodyStatePartial
	}
}
func (b *captureBody) artifact() BodyArtifact {
	b.mu.Lock()
	defer b.mu.Unlock()
	state := b.state
	if state == BodyStatePending {
		state = BodyStateAvailable
	}
	return BodyArtifact{State: state, RawBytes: b.rawBytes, SHA256: hex.EncodeToString(b.hash.Sum(nil)), Bytes: append([]byte(nil), b.memory...), FilePath: b.filePath, cleanup: b.cleanupLocked}
}
func (b *captureBody) cleanupLocked() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.file != nil {
		_ = b.file.Close()
	}
	if b.filePath != "" {
		if err := os.Remove(b.filePath); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	b.budget.releaseMemory(b.memBytes)
	b.budget.releaseSpool(b.spoolBytes)
	b.memory = nil
	b.memBytes = 0
	b.spoolBytes = 0
	return nil
}

type messageCaptureContextKey struct{}

func WithMessageCapture(ctx context.Context, session *MessageCaptureSession) context.Context {
	return context.WithValue(ctx, messageCaptureContextKey{}, session)
}
func MessageCaptureFromContext(ctx context.Context) *MessageCaptureSession {
	if ctx == nil {
		return nil
	}
	session, _ := ctx.Value(messageCaptureContextKey{}).(*MessageCaptureSession)
	return session
}
