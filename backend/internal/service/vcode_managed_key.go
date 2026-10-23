package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
)

// EnsureVcodeManagedKey 为一个真实用户和分组建立内部计费身份。Key 值只在
// sub2api 进程内用于复用现有网关，不返回给 Vcode 控制服务或 Agent。
func (s *APIKeyService) EnsureVcodeManagedKey(ctx context.Context, userID, groupID int64, secret string) (string, error) {
	if s == nil || userID <= 0 || groupID <= 0 || len(secret) < 32 {
		return "", errors.New("Vcode integration is not configured")
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = fmt.Fprintf(mac, "vcode-managed-key:%d:%d", userID, groupID)
	key := "sk-vcode-" + hex.EncodeToString(mac.Sum(nil))
	verify := func(item *APIKey) error {
		if item == nil || item.UserID != userID || item.GroupID == nil || *item.GroupID != groupID || !item.IsActive() {
			return errors.New("Vcode managed key has conflicting ownership or is disabled")
		}
		return nil
	}
	item, err := s.apiKeyRepo.GetByKey(ctx, key)
	if err == nil {
		if err := verify(item); err != nil {
			return "", err
		}
		return key, nil
	}
	if !errors.Is(err, ErrAPIKeyNotFound) {
		return "", err
	}
	_, createErr := s.Create(ctx, userID, CreateAPIKeyRequest{
		Name: "Vcode managed", GroupID: &groupID, CustomKey: &key, Managed: true,
	})
	if createErr == nil {
		return key, nil
	}
	// 多个实例同时首用时唯一约束只允许一个创建；读取已提交记录确认归属。
	item, err = s.apiKeyRepo.GetByKey(ctx, key)
	if err != nil {
		return "", createErr
	}
	if err := verify(item); err != nil {
		return "", err
	}
	return key, nil
}
