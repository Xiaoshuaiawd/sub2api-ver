package service

import (
	"context"
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type proxyGroupMemberSourceStub struct {
	AccountRepository
	group   string
	members []Proxy
}

func (s *proxyGroupMemberSourceStub) ListActiveProxyGroupMembers(_ context.Context, group string) ([]Proxy, error) {
	s.group = group
	return s.members, nil
}

func TestSelectProxyGroupMemberOnlyReturnsActiveUnexpiredMembers(t *testing.T) {
	now := time.Date(2026, time.August, 10, 0, 0, 0, 0, time.UTC)
	expired := now.Add(-time.Minute)
	eligibleIDs := map[int64]struct{}{1: {}, 4: {}}
	members := []Proxy{
		{ID: 1, Name: "active-one", Status: StatusActive},
		{ID: 2, Name: "inactive", Status: StatusDisabled},
		{ID: 3, Name: "expired", Status: StatusActive, ExpiresAt: &expired},
		{ID: 4, Name: "active-two", Status: StatusActive},
	}

	for seed := int64(0); seed < 20; seed++ {
		selected, err := SelectProxyGroupMember(members, now, rand.New(rand.NewSource(seed)))
		require.NoError(t, err)
		_, ok := eligibleIDs[selected.ID]
		require.Truef(t, ok, "selected ineligible proxy %d", selected.ID)
	}
}

func TestSelectProxyGroupMemberReturnsUnavailableForEmptyEligibleSet(t *testing.T) {
	now := time.Date(2026, time.August, 10, 0, 0, 0, 0, time.UTC)
	expired := now.Add(-time.Minute)

	_, err := SelectProxyGroupMember([]Proxy{
		{ID: 1, Status: StatusDisabled},
		{ID: 2, Status: StatusActive, ExpiresAt: &expired},
	}, now, rand.New(rand.NewSource(1)))

	require.ErrorIs(t, err, ErrProxyGroupNoAvailableProxy)
}

func TestResolveAccountProxyGroupUsesEligibleGroupMemberForThisRequest(t *testing.T) {
	group := "residential-us"
	repository := &proxyGroupMemberSourceStub{members: []Proxy{
		{ID: 1, Status: StatusDisabled},
		{ID: 2, Status: StatusActive},
	}}
	account := &Account{ProxyGroup: &group}

	err := resolveAccountProxyGroup(context.Background(), account, repository)

	require.NoError(t, err)
	require.Equal(t, group, repository.group)
	require.NotNil(t, account.ProxyID)
	require.Equal(t, int64(2), *account.ProxyID)
	require.NotNil(t, account.Proxy)
	require.Equal(t, int64(2), account.Proxy.ID)
}
