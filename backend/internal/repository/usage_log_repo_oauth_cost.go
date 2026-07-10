package repository

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// GetOAuthCostSummary returns all-time account-side consumption for non-deleted OAuth accounts.
func (r *usageLogRepository) GetOAuthCostSummary(ctx context.Context) (*usagestats.OAuthCostSummary, error) {
	const query = `
		SELECT
			COUNT(DISTINCT a.id) AS account_count,
			COALESCE(SUM(COALESCE(ul.account_stats_cost, ul.total_cost) * COALESCE(ul.account_rate_multiplier, 1)), 0) AS total_consumed_quota,
			COUNT(ul.id) AS total_requests,
			COALESCE(SUM(
				COALESCE(ul.input_tokens, 0) +
				COALESCE(ul.output_tokens, 0) +
				COALESCE(ul.cache_creation_tokens, 0) +
				COALESCE(ul.cache_read_tokens, 0)
			), 0) AS total_tokens
		FROM accounts a
		LEFT JOIN usage_logs ul ON ul.account_id = a.id
		WHERE a.deleted_at IS NULL AND a.type = $1
	`

	summary := &usagestats.OAuthCostSummary{}
	if err := scanSingleRow(
		ctx,
		r.sql,
		query,
		[]any{service.AccountTypeOAuth},
		&summary.AccountCount,
		&summary.TotalConsumedQuota,
		&summary.TotalRequests,
		&summary.TotalTokens,
	); err != nil {
		return nil, err
	}
	return summary, nil
}
