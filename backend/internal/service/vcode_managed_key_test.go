//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type vcodeKeyRepoStub struct {
	APIKeyRepository
	keys map[string]*APIKey
}

func (r *vcodeKeyRepoStub) GetByKey(_ context.Context, key string) (*APIKey, error) {
	if item := r.keys[key]; item != nil {
		return item, nil
	}
	return nil, ErrAPIKeyNotFound
}
func (r *vcodeKeyRepoStub) ExistsByKey(_ context.Context, key string) (bool, error) {
	return r.keys[key] != nil, nil
}
func (r *vcodeKeyRepoStub) Create(_ context.Context, item *APIKey) error {
	item.ID = int64(len(r.keys) + 1)
	r.keys[item.Key] = item
	return nil
}

type vcodeUserRepoStub struct{ UserRepository }

func (vcodeUserRepoStub) GetByID(_ context.Context, id int64) (*User, error) {
	return &User{ID: id, Status: StatusActive}, nil
}

type vcodeGroupRepoStub struct{ GroupRepository }

func (vcodeGroupRepoStub) GetByID(_ context.Context, id int64) (*Group, error) {
	return &Group{ID: id, Status: StatusActive}, nil
}

func TestEnsureVcodeManagedKeyIsStableAndScopedToUserAndGroup(t *testing.T) {
	repo := &vcodeKeyRepoStub{keys: make(map[string]*APIKey)}
	svc := NewAPIKeyService(repo, vcodeUserRepoStub{}, vcodeGroupRepoStub{}, nil, nil, nil, &config.Config{})
	secret := "vcode-secret-at-least-thirty-two-characters"
	first, err := svc.EnsureVcodeManagedKey(context.Background(), 13, 7, secret)
	require.NoError(t, err)
	require.NotEmpty(t, first)
	again, err := svc.EnsureVcodeManagedKey(context.Background(), 13, 7, secret)
	require.NoError(t, err)
	require.Equal(t, first, again)
	other, err := svc.EnsureVcodeManagedKey(context.Background(), 13, 8, secret)
	require.NoError(t, err)
	require.NotEqual(t, first, other)
	require.Len(t, repo.keys, 2)
	require.Equal(t, int64(13), repo.keys[first].UserID)
	require.Equal(t, int64(7), *repo.keys[first].GroupID)
}
