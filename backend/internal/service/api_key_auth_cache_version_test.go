package service

import (
	"context"
	"testing"
)

func TestAPIKeyAuthSnapshotPreservesHybridSubscriptionRate(t *testing.T) {
	groupID, quotaRate := int64(9), 1.0
	svc := &APIKeyService{}
	key := &APIKey{ID: 1, UserID: 2, GroupID: &groupID, User: &User{ID: 2}, Group: &Group{ID: groupID, SubscriptionType: SubscriptionTypeSubscriptionBalance, RateMultiplier: 0.25, SubscriptionRateMultiplier: &quotaRate}}
	snapshot := svc.snapshotFromAPIKey(context.Background(), key)
	loaded := svc.snapshotToAPIKey("test-key", snapshot)
	if loaded == nil || loaded.Group == nil || loaded.Group.SubscriptionRateMultiplier == nil || *loaded.Group.SubscriptionRateMultiplier != 1 || loaded.Group.RateMultiplier != 0.25 {
		t.Fatalf("hybrid rates lost across auth snapshot: %#v", loaded)
	}
}

func TestAPIKeyService_RejectsV10AuthSnapshotWithoutModelAllowlist(t *testing.T) {
	groupID := int64(9)
	svc := &APIKeyService{}

	apiKey, ok, err := svc.applyAuthCacheEntry("k-legacy-models-list", &APIKeyAuthCacheEntry{
		Snapshot: &APIKeyAuthSnapshot{
			Version:  10,
			APIKeyID: 1,
			UserID:   2,
			GroupID:  &groupID,
			Status:   StatusActive,
			User: APIKeyAuthUserSnapshot{
				ID:          2,
				Status:      StatusActive,
				Role:        RoleUser,
				Balance:     10,
				Concurrency: 3,
			},
			Group: &APIKeyAuthGroupSnapshot{
				ID:               groupID,
				Name:             "openai",
				Platform:         PlatformOpenAI,
				Status:           StatusActive,
				SubscriptionType: SubscriptionTypeStandard,
				RateMultiplier:   1,
			},
		},
	})

	if err != nil {
		t.Fatalf("expected stale snapshot to be ignored without error, got %v", err)
	}
	if ok {
		t.Fatalf("expected v10 auth snapshot to be rejected after model_allowlist was added")
	}
	if apiKey != nil {
		t.Fatalf("expected no API key from stale snapshot, got %#v", apiKey)
	}
}

func TestAPIKeyService_RejectsV15AuthSnapshotWithoutReasoningEffortPolicy(t *testing.T) {
	svc := &APIKeyService{}

	apiKey, ok, err := svc.applyAuthCacheEntry("k-legacy-reasoning-mappings", &APIKeyAuthCacheEntry{
		Snapshot: &APIKeyAuthSnapshot{Version: 15},
	})

	if err != nil {
		t.Fatalf("expected stale snapshot to be ignored without error, got %v", err)
	}
	if ok {
		t.Fatal("expected v15 auth snapshot to be rejected after reasoning effort policy was added")
	}
	if apiKey != nil {
		t.Fatalf("expected no API key from stale snapshot, got %#v", apiKey)
	}
}
