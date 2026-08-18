package service

import (
	"context"
	"errors"
	"time"
)

const (
	MessageBodyTypeRequest  = "request"
	MessageBodyTypeResponse = "response"
)

var ErrMessageCaptureNotFound = errors.New("message capture not found")

type MessageCaptureMetadata struct {
	UsageLogID          int64
	RequestID           string
	RequestState        string
	ResponseState       string
	RequestRawBytes     int64
	ResponseRawBytes    int64
	RequestStoredBytes  int64
	ResponseStoredBytes int64
	RequestSHA256       string
	ResponseSHA256      string
	Compression         string
	ErrorCode           string
	ErrorMessage        string
	ExpiresAt           time.Time
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type StoredMessageBody struct {
	CreatedAt   time.Time
	UsageLogID  int64
	BodyType    string
	Payload     []byte
	RawBytes    int64
	StoredBytes int64
}

type MessageBodyDetail struct {
	State       string `json:"state"`
	RawBytes    int64  `json:"raw_bytes"`
	StoredBytes int64  `json:"stored_bytes"`
	SHA256      string `json:"sha256,omitempty"`
	Body        string `json:"body,omitempty"`
	Payload     []byte `json:"-"`
}

type MessageCaptureDetail struct {
	UsageLogID   int64             `json:"usage_log_id"`
	RequestID    string            `json:"request_id"`
	Request      MessageBodyDetail `json:"request"`
	Response     MessageBodyDetail `json:"response"`
	Compression  string            `json:"compression"`
	ErrorCode    string            `json:"error_code,omitempty"`
	ErrorMessage string            `json:"error_message,omitempty"`
	ExpiresAt    time.Time         `json:"expires_at"`
}

type MessageCaptureSummary struct {
	UsageLogID       int64
	RequestState     string
	ResponseState    string
	RequestRawBytes  int64
	ResponseRawBytes int64
}

type MessageStorageRepository interface {
	CreatePending(context.Context, MessageCaptureMetadata) error
	StoreBody(context.Context, StoredMessageBody) error
	UpdateBodyState(context.Context, int64, string, string, int64, string, string) error
	GetDetail(context.Context, int64, bool) (*MessageCaptureDetail, error)
	GetSummaries(context.Context, []int64) (map[int64]MessageCaptureSummary, error)
	EnsurePartitions(context.Context, time.Time) error
	DropPartitionsBefore(context.Context, time.Time) (int64, error)
	MarkStalePendingFailed(context.Context, time.Time) (int64, error)
	ExpireBefore(context.Context, time.Time, int) (int64, error)
}

type MessageEnqueueResult struct {
	Accepted bool
	Code     string
}
