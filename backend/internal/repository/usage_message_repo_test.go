package repository

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUsageMessageRepositoryCreatePendingUsesParameters(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := NewUsageMessageRepository(db)
	expires := time.Now().Add(7 * 24 * time.Hour)
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO usage_message_captures")).
		WithArgs(int64(9), "req-9", service.BodyStatePending, service.BodyStatePending, int64(3), int64(4), "a", "b", expires).
		WillReturnResult(sqlmock.NewResult(1, 1))
	err = repo.CreatePending(context.Background(), service.MessageCaptureMetadata{UsageLogID: 9, RequestID: "req-9", RequestState: service.BodyStatePending, ResponseState: service.BodyStatePending, RequestRawBytes: 3, ResponseRawBytes: 4, RequestSHA256: "a", ResponseSHA256: "b", ExpiresAt: expires})
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageMessageRepositoryStoresBodyWithUpsert(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := NewUsageMessageRepository(db)
	created := time.Now()
	mock.ExpectExec("INSERT INTO usage_message_bodies").WithArgs(created, int64(9), "request", []byte("zstd"), int64(10), int64(4)).WillReturnResult(sqlmock.NewResult(1, 1))
	require.NoError(t, repo.StoreBody(context.Background(), service.StoredMessageBody{CreatedAt: created, UsageLogID: 9, BodyType: "request", Payload: []byte("zstd"), RawBytes: 10, StoredBytes: 4}))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageMessageRepositoryExpireSkipsAlreadyExpiredRows(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := NewUsageMessageRepository(db)
	cutoff := time.Now()

	queryFragment := regexp.QuoteMeta("WHERE expires_at < $1 AND (request_state <> 'expired' OR response_state <> 'expired')")
	mock.ExpectExec("(?s)DELETE FROM usage_message_bodies.*"+queryFragment).
		WithArgs(cutoff, 1000).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("(?s)UPDATE usage_message_captures.*"+queryFragment).
		WithArgs(cutoff, 1000).
		WillReturnResult(sqlmock.NewResult(0, 1))

	n, err := repo.ExpireBefore(context.Background(), cutoff, 1000)
	require.NoError(t, err)
	require.Equal(t, int64(1), n)
	require.NoError(t, mock.ExpectationsWereMet())
}
