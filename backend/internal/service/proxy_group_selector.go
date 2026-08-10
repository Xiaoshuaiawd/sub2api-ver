package service

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"time"
)

var ErrProxyGroupNoAvailableProxy = errors.New("proxy group has no available proxy")

type proxyGroupMemberSource interface {
	ListActiveProxyGroupMembers(ctx context.Context, group string) ([]Proxy, error)
}

func resolveAccountProxyGroup(ctx context.Context, account *Account, repository AccountRepository) error {
	return resolveAccountProxyGroupExcept(ctx, account, repository, nil)
}

func resolveAccountProxyGroupExcept(ctx context.Context, account *Account, repository AccountRepository, excludedID *int64) error {
	if account == nil || account.ProxyGroup == nil || strings.TrimSpace(*account.ProxyGroup) == "" {
		return nil
	}
	source, ok := repository.(proxyGroupMemberSource)
	if !ok {
		return fmt.Errorf("resolve proxy group %q: account repository does not support proxy groups", *account.ProxyGroup)
	}
	members, err := source.ListActiveProxyGroupMembers(ctx, *account.ProxyGroup)
	if err != nil {
		return fmt.Errorf("load proxy group %q: %w", *account.ProxyGroup, err)
	}
	selected, err := SelectProxyGroupMemberExcept(members, time.Now(), nil, excludedID)
	if err != nil {
		return fmt.Errorf("resolve proxy group %q: %w", *account.ProxyGroup, err)
	}
	account.ProxyID = &selected.ID
	account.Proxy = selected
	return nil
}

// rotateAccountProxyGroupForRetry selects another group member after an
// upstream attempt failed. Fixed and direct connections are left unchanged.
func rotateAccountProxyGroupForRetry(ctx context.Context, account *Account, repository AccountRepository) error {
	if account == nil || account.ProxyID == nil {
		return resolveAccountProxyGroupExcept(ctx, account, repository, nil)
	}
	previousID := *account.ProxyID
	return resolveAccountProxyGroupExcept(ctx, account, repository, &previousID)
}

func accountProxyURL(account *Account) string {
	if account == nil || account.Proxy == nil {
		return ""
	}
	return account.Proxy.URL()
}

// SelectProxyGroupMember returns one active, non-expired proxy from members.
// The caller supplies the random source so request routing can be tested without
// relying on global randomness.
func SelectProxyGroupMember(members []Proxy, now time.Time, random *rand.Rand) (*Proxy, error) {
	return SelectProxyGroupMemberExcept(members, now, random, nil)
}

// SelectProxyGroupMemberExcept chooses a different eligible member when one is
// available. It falls back to the excluded member for single-member groups.
func SelectProxyGroupMemberExcept(members []Proxy, now time.Time, random *rand.Rand, excludedID *int64) (*Proxy, error) {
	eligible := make([]Proxy, 0, len(members))
	alternatives := make([]Proxy, 0, len(members))
	for i := range members {
		member := members[i]
		if !member.IsActive() || member.IsExpired(now) {
			continue
		}
		eligible = append(eligible, member)
		if excludedID == nil || member.ID != *excludedID {
			alternatives = append(alternatives, member)
		}
	}
	if len(eligible) == 0 {
		return nil, ErrProxyGroupNoAvailableProxy
	}
	if len(alternatives) > 0 {
		eligible = alternatives
	}
	if random == nil {
		random = rand.New(rand.NewSource(now.UnixNano()))
	}
	selected := eligible[random.Intn(len(eligible))]
	return &selected, nil
}
