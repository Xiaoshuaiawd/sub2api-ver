package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type usageMessageRepository struct{ db *sql.DB }

func NewUsageMessageRepository(db *sql.DB) service.MessageStorageRepository {
	return &usageMessageRepository{db: db}
}

func (r *usageMessageRepository) CreatePending(ctx context.Context, m service.MessageCaptureMetadata) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO usage_message_captures
        (usage_log_id, request_id, request_state, response_state, request_raw_bytes,
         response_raw_bytes, request_sha256, response_sha256, expires_at)
        VALUES ($1,$2,$3,$4,$5,$6,NULLIF($7,''),NULLIF($8,''),$9)
        ON CONFLICT (usage_log_id) DO NOTHING`,
		m.UsageLogID, m.RequestID, m.RequestState, m.ResponseState, m.RequestRawBytes,
		m.ResponseRawBytes, m.RequestSHA256, m.ResponseSHA256, m.ExpiresAt)
	return err
}

func (r *usageMessageRepository) StoreBody(ctx context.Context, b service.StoredMessageBody) error {
	result, err := r.db.ExecContext(ctx, `INSERT INTO usage_message_bodies
        (created_at, usage_log_id, body_type, payload_zstd, raw_bytes, stored_bytes)
        SELECT c.created_at, $1, $2, $3, $4, $5
        FROM usage_message_captures c WHERE c.usage_log_id=$1
        ON CONFLICT (created_at, usage_log_id, body_type) DO UPDATE SET
          payload_zstd = EXCLUDED.payload_zstd,
          raw_bytes = EXCLUDED.raw_bytes,
		  stored_bytes = EXCLUDED.stored_bytes`,
		b.UsageLogID, b.BodyType, b.Payload, b.RawBytes, b.StoredBytes)
	if err != nil {
		return err
	}
	if n, rowsErr := result.RowsAffected(); rowsErr != nil {
		return rowsErr
	} else if n == 0 {
		return fmt.Errorf("message capture %d does not exist", b.UsageLogID)
	}
	return nil
}

func (r *usageMessageRepository) UpdateBodyState(ctx context.Context, usageLogID int64, bodyType, state string, storedBytes int64, errorCode, errorMessage string) error {
	var query string
	switch bodyType {
	case service.MessageBodyTypeRequest:
		query = `UPDATE usage_message_captures SET request_state=$2, request_stored_bytes=$3,
                 error_code=NULLIF($4,''), error_message=NULLIF($5,''), updated_at=NOW()
                 WHERE usage_log_id=$1`
	case service.MessageBodyTypeResponse:
		query = `UPDATE usage_message_captures SET response_state=$2, response_stored_bytes=$3,
                 error_code=NULLIF($4,''), error_message=NULLIF($5,''), updated_at=NOW()
                 WHERE usage_log_id=$1`
	default:
		return fmt.Errorf("invalid body type %q", bodyType)
	}
	_, err := r.db.ExecContext(ctx, query, usageLogID, state, storedBytes, errorCode, errorMessage)
	return err
}

func (r *usageMessageRepository) GetDetail(ctx context.Context, usageLogID int64, includeBodies bool) (*service.MessageCaptureDetail, error) {
	d := &service.MessageCaptureDetail{UsageLogID: usageLogID}
	err := r.db.QueryRowContext(ctx, `SELECT request_id, request_state, response_state,
        request_raw_bytes, response_raw_bytes, request_stored_bytes, response_stored_bytes,
        COALESCE(request_sha256,''), COALESCE(response_sha256,''), compression,
        COALESCE(error_code,''), COALESCE(error_message,''), expires_at
        FROM usage_message_captures WHERE usage_log_id=$1`, usageLogID).Scan(
		&d.RequestID, &d.Request.State, &d.Response.State,
		&d.Request.RawBytes, &d.Response.RawBytes, &d.Request.StoredBytes, &d.Response.StoredBytes,
		&d.Request.SHA256, &d.Response.SHA256, &d.Compression,
		&d.ErrorCode, &d.ErrorMessage, &d.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrMessageCaptureNotFound
	}
	if err != nil || !includeBodies {
		return d, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT body_type, payload_zstd
        FROM usage_message_bodies WHERE usage_log_id=$1 ORDER BY body_type`, usageLogID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var bodyType string
		var payload []byte
		if err := rows.Scan(&bodyType, &payload); err != nil {
			return nil, err
		}
		switch bodyType {
		case service.MessageBodyTypeRequest:
			d.Request.Payload = payload
		case service.MessageBodyTypeResponse:
			d.Response.Payload = payload
		}
	}
	return d, rows.Err()
}

func (r *usageMessageRepository) GetSummaries(ctx context.Context, usageLogIDs []int64) (map[int64]service.MessageCaptureSummary, error) {
	out := make(map[int64]service.MessageCaptureSummary, len(usageLogIDs))
	if len(usageLogIDs) == 0 {
		return out, nil
	}
	placeholders := make([]string, len(usageLogIDs))
	args := make([]any, len(usageLogIDs))
	for i, id := range usageLogIDs {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}
	rows, err := r.db.QueryContext(ctx, `SELECT usage_log_id, request_state, response_state,
        request_raw_bytes, response_raw_bytes FROM usage_message_captures WHERE usage_log_id IN (`+strings.Join(placeholders, ",")+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var summary service.MessageCaptureSummary
		if err := rows.Scan(&summary.UsageLogID, &summary.RequestState, &summary.ResponseState, &summary.RequestRawBytes, &summary.ResponseRawBytes); err != nil {
			return nil, err
		}
		out[summary.UsageLogID] = summary
	}
	return out, rows.Err()
}

func (r *usageMessageRepository) EnsurePartitions(ctx context.Context, now time.Time) error {
	day := now.UTC().Truncate(24 * time.Hour)
	for offset := -1; offset <= 2; offset++ {
		start := day.AddDate(0, 0, offset)
		end := start.AddDate(0, 0, 1)
		name := "usage_message_bodies_" + start.Format("20060102")
		query := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s PARTITION OF usage_message_bodies FOR VALUES FROM ('%s') TO ('%s')", name, start.Format(time.RFC3339), end.Format(time.RFC3339))
		if _, err := r.db.ExecContext(ctx, query); err != nil {
			return err
		}
	}
	return nil
}

var usageMessagePartitionName = regexp.MustCompile(`^usage_message_bodies_(\d{8})$`)

func (r *usageMessageRepository) DropPartitionsBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT child.relname
        FROM pg_inherits
        JOIN pg_class parent ON pg_inherits.inhparent = parent.oid
        JOIN pg_class child ON pg_inherits.inhrelid = child.oid
        WHERE parent.relname = 'usage_message_bodies'`)
	if err != nil {
		return 0, err
	}
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			_ = rows.Close()
			return 0, err
		}
		match := usageMessagePartitionName.FindStringSubmatch(name)
		if len(match) != 2 {
			continue
		}
		day, err := time.Parse("20060102", match[1])
		if err == nil && day.Before(cutoff.UTC().Truncate(24*time.Hour)) {
			names = append(names, name)
		}
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	var dropped int64
	for _, name := range names {
		if _, err := r.db.ExecContext(ctx, "DROP TABLE IF EXISTS "+name); err != nil {
			return dropped, err
		}
		dropped++
	}
	return dropped, nil
}

func (r *usageMessageRepository) MarkStalePendingFailed(ctx context.Context, before time.Time) (int64, error) {
	result, err := r.db.ExecContext(ctx, `UPDATE usage_message_captures SET
        request_state=CASE WHEN request_state='pending' THEN 'failed' ELSE request_state END,
        response_state=CASE WHEN response_state='pending' THEN 'failed' ELSE response_state END,
        error_code='stale_pending', error_message='message storage worker did not finish', updated_at=NOW()
        WHERE updated_at < $1 AND (request_state='pending' OR response_state='pending')`, before)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (r *usageMessageRepository) ExpireBefore(ctx context.Context, cutoff time.Time, batchSize int) (int64, error) {
	if batchSize <= 0 {
		batchSize = 1000
	}
	ids := `SELECT usage_log_id FROM usage_message_captures
        WHERE expires_at < $1 AND (request_state <> 'expired' OR response_state <> 'expired')
        ORDER BY expires_at LIMIT $2`
	if _, err := r.db.ExecContext(ctx, `DELETE FROM usage_message_bodies WHERE usage_log_id IN (`+ids+`)`, cutoff, batchSize); err != nil {
		return 0, err
	}
	result, err := r.db.ExecContext(ctx, `UPDATE usage_message_captures SET request_state='expired', response_state='expired',
        request_stored_bytes=0, response_stored_bytes=0, updated_at=NOW()
        WHERE usage_log_id IN (`+ids+`)`, cutoff, batchSize)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
