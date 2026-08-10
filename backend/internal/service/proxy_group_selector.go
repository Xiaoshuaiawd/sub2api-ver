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
	selected, err := SelectProxyGroupMember(members, time.Now(), nil)
	if err != nil {
		return fmt.Errorf("resolve proxy group %q: %w", *account.ProxyGroup, err)
	}
	account.ProxyID = &selected.ID
	account.Proxy = selected
	return nil
}

// SelectProxyGroupMember returns one active, non-expired proxy from members.
// The caller supplies the random source so request routing can be tested without
// relying on global randomness.
func SelectProxyGroupMember(members []Proxy, now time.Time, random *rand.Rand) (*Proxy, error) {
	eligible := make([]Proxy, 0, len(members))
	for i := range members {
		member := members[i]
		if !member.IsActive() || member.IsExpired(now) {
			continue
		}
		eligible = append(eligible, member)
	}
	if len(eligible) == 0 {
		return nil, ErrProxyGroupNoAvailableProxy
	}
	if random == nil {
		random = rand.New(rand.NewSource(now.UnixNano()))
	}
	selected := eligible[random.Intn(len(eligible))]
	return &selected, nil
}
