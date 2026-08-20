package repository

import (
	"context"
	"strconv"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestCleanupExpiredAccountSlotKeysMixedAgeLeaseRunsAtEarliestExpiry(t *testing.T) {
	redisServer := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	cache := NewConcurrencyCache(client, 1, 60).(*concurrencyCache)
	ctx := context.Background()
	accountID := int64(7301)

	acquired, err := cache.AcquireAccountSlot(ctx, accountID, 2, "old-lease")
	require.NoError(t, err)
	require.True(t, acquired)

	now, err := cache.redisUnixSeconds(ctx)
	require.NoError(t, err)
	expiredScore := now - int64(cache.slotTTLSeconds) - 1
	require.NoError(t, client.ZAdd(ctx, accountSlotKey(accountID), redis.Z{
		Score:  float64(expiredScore),
		Member: "old-lease",
	}).Err())
	require.NoError(t, client.ZAdd(ctx, accountActiveIndexKey, redis.Z{
		Score:  float64(expiredScore + int64(cache.slotTTLSeconds)),
		Member: strconv.FormatInt(accountID, 10),
	}).Err())

	acquired, err = cache.AcquireAccountSlot(ctx, accountID, 2, "new-lease")
	require.NoError(t, err)
	require.True(t, acquired)

	require.NoError(t, cache.CleanupExpiredAccountSlotKeys(ctx))

	members, err := client.ZRange(ctx, accountSlotKey(accountID), 0, -1).Result()
	require.NoError(t, err)
	require.Equal(t, []string{"new-lease"}, members)

	now, err = cache.redisUnixSeconds(ctx)
	require.NoError(t, err)
	indexScore, err := client.ZScore(ctx, accountActiveIndexKey, strconv.FormatInt(accountID, 10)).Result()
	require.NoError(t, err)
	require.Greater(t, int64(indexScore), now, "live lease must keep the account indexed for its next expiry")
}
