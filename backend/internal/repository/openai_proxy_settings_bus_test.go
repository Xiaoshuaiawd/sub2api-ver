//go:build unit

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestOpenAIProxySettingsBusPublishesInvalidation(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	bus := NewOpenAIProxySettingsBus(client)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	received := make(chan struct{}, 1)

	bus.Subscribe(ctx, func() { received <- struct{}{} })

	require.Eventually(t, func() bool {
		require.NoError(t, bus.Publish(context.Background()))
		select {
		case <-received:
			return true
		default:
			return false
		}
	}, time.Second, 10*time.Millisecond)
}
