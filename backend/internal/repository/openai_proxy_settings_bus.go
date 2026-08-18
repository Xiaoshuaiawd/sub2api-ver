package repository

import (
	"context"
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

const openAIProxySettingsChannel = "openai_proxy_settings_updated"

type openAIProxySettingsBus struct {
	rdb *redis.Client
}

func NewOpenAIProxySettingsBus(rdb *redis.Client) service.OpenAIProxySettingsBus {
	return &openAIProxySettingsBus{rdb: rdb}
}

func (b *openAIProxySettingsBus) Publish(ctx context.Context) error {
	if b == nil || b.rdb == nil {
		return fmt.Errorf("Redis client is not configured")
	}
	return b.rdb.Publish(ctx, openAIProxySettingsChannel, "refresh").Err()
}

func (b *openAIProxySettingsBus) Subscribe(ctx context.Context, handler func()) {
	if b == nil || b.rdb == nil || handler == nil {
		return
	}
	go func() {
		subscription := b.rdb.Subscribe(ctx, openAIProxySettingsChannel)
		defer func() { _ = subscription.Close() }()
		messages := subscription.Channel()
		for {
			select {
			case <-ctx.Done():
				return
			case _, ok := <-messages:
				if !ok {
					return
				}
				handler()
			}
		}
	}()
}
