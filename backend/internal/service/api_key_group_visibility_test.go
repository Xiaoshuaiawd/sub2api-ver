//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type visibilityUserRepo struct {
	UserRepository
	user *User
	err  error
}

func (r *visibilityUserRepo) GetByID(context.Context, int64) (*User, error) { return r.user, r.err }

type visibilitySubRepo struct {
	UserSubscriptionRepository
	subscriptions []UserSubscription
	err           error
	calls         int
}

func (r *visibilitySubRepo) ListActiveByUserID(_ context.Context, userID int64) ([]UserSubscription, error) {
	r.calls++
	// Match the repository's active-status and expiry predicates.
	active := make([]UserSubscription, 0)
	for _, sub := range r.subscriptions {
		if sub.UserID == userID && sub.IsActive() {
			active = append(active, sub)
		}
	}
	return active, r.err
}

type visibilityGroupRepo struct {
	GroupRepository
	groups []Group
}

type hybridBindingSubRepo struct {
	UserSubscriptionRepository
	subscribed map[int64]bool
	err        error
}

func (r *hybridBindingSubRepo) GetActiveByUserIDAndGroupID(_ context.Context, _, groupID int64) (*UserSubscription, error) {
	if r.err != nil {
		return nil, r.err
	}
	if r.subscribed[groupID] {
		return &UserSubscription{GroupID: groupID}, nil
	}
	return nil, ErrSubscriptionNotFound
}

func TestHybridGroupBindingUsesSubscriptionOrBalanceEntitlement(t *testing.T) {
	user := &User{ID: 1, AllowedGroups: []int64{20}}
	svc := &APIKeyService{userSubRepo: &hybridBindingSubRepo{subscribed: map[int64]bool{30: true}}}
	for _, tt := range []struct {
		name         string
		group        Group
		wantBindable bool
	}{
		{"public hybrid without subscription", Group{ID: 10, SubscriptionType: SubscriptionTypeSubscriptionBalance}, true},
		{"granted exclusive hybrid without subscription", Group{ID: 20, IsExclusive: true, SubscriptionType: SubscriptionTypeSubscriptionBalance}, true},
		{"exclusive hybrid without grant", Group{ID: 21, IsExclusive: true, SubscriptionType: SubscriptionTypeSubscriptionBalance}, false},
		{"exclusive hybrid with subscription", Group{ID: 30, IsExclusive: true, SubscriptionType: SubscriptionTypeSubscriptionBalance}, true},
		{"strict subscription without subscription", Group{ID: 40, SubscriptionType: SubscriptionTypeSubscription}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.wantBindable, svc.canUserBindGroup(context.Background(), user, &tt.group))
		})
	}
	svc.userSubRepo = &hybridBindingSubRepo{err: errors.New("database unavailable")}
	require.False(t, svc.canUserBindGroup(context.Background(), user, &Group{ID: 10, SubscriptionType: SubscriptionTypeSubscriptionBalance}),
		"subscription repository failures must not silently grant wallet access")
}

func (r *visibilityGroupRepo) ListActive(context.Context) ([]Group, error) { return r.groups, nil }

func TestGetUserGroupVisibilityIncludesActiveSubscriptions(t *testing.T) {
	for _, restricted := range []bool{false, true} {
		t.Run(map[bool]string{false: "unrestricted", true: "restricted"}[restricted], func(t *testing.T) {
			now := time.Now()
			subs := &visibilitySubRepo{subscriptions: []UserSubscription{
				{UserID: 1, GroupID: 42, Status: SubscriptionStatusActive, ExpiresAt: now.Add(time.Hour)},
				{UserID: 1, GroupID: 43, Status: SubscriptionStatusActive, ExpiresAt: now.Add(-time.Hour)},
				{UserID: 1, GroupID: 44, Status: "expired", ExpiresAt: now.Add(time.Hour)},
				{UserID: 2, GroupID: 45, Status: SubscriptionStatusActive, ExpiresAt: now.Add(time.Hour)},
			}}
			svc := &APIKeyService{
				userRepo:    &visibilityUserRepo{user: &User{ID: 1, AllowedGroups: []int64{7}, RestrictPublicGroups: restricted}},
				userSubRepo: subs,
				groupRepo:   &visibilityGroupRepo{groups: []Group{{ID: 42, IsExclusive: true, SubscriptionType: "subscription"}}},
			}
			available, err := svc.GetAvailableGroups(context.Background(), 1)
			require.NoError(t, err)
			require.Len(t, available, 1)
			visible, restrict, err := svc.GetUserGroupVisibility(context.Background(), 1)
			require.NoError(t, err)
			require.Equal(t, restricted, restrict)
			require.Equal(t, map[int64]struct{}{7: {}, 42: {}}, visible)
			require.Contains(t, visible, available[0].ID, "a subscribed group that can be bound must be visible")
		})
	}
}

func TestGetUserGroupVisibilityEmptyAndErrors(t *testing.T) {
	failure := errors.New("repository unavailable")
	for _, tc := range []struct {
		name            string
		userErr, subErr error
	}{
		{name: "empty"}, {name: "user failure", userErr: failure}, {name: "subscription failure", subErr: failure},
	} {
		t.Run(tc.name, func(t *testing.T) {
			subs := &visibilitySubRepo{err: tc.subErr}
			svc := &APIKeyService{userRepo: &visibilityUserRepo{user: &User{ID: 1}, err: tc.userErr}, userSubRepo: subs}
			got, _, err := svc.GetUserGroupVisibility(context.Background(), 1)
			if tc.userErr != nil || tc.subErr != nil {
				require.ErrorIs(t, err, failure)
				require.Nil(t, got, "repository failures must not become anonymous visibility")
			} else {
				require.NoError(t, err)
				require.NotNil(t, got, "an empty logged-in user must not become anonymous")
				require.Empty(t, got)
			}
			if tc.userErr != nil {
				require.Zero(t, subs.calls)
			}
		})
	}
}
