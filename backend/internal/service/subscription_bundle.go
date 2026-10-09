package service

import (
	"fmt"
)

// normalizePlanGroupIDs accepts the old single-group request and the new
// ordered group list. The first ID is the legacy primary group ID.
func normalizePlanGroupIDs(groupID int64, ids []int64) ([]int64, error) {
	if ids == nil && groupID > 0 {
		ids = []int64{groupID}
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("at least one subscription group is required")
	}
	seen := make(map[int64]bool, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			return nil, fmt.Errorf("subscription group IDs must be positive")
		}
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out, nil
}

func (s *UserSubscription) CanUseGroup(groupID int64) bool {
	if s == nil || groupID <= 0 {
		return false
	}
	if s.GroupID == groupID {
		return true
	}
	for _, id := range s.GroupIDs {
		if id == groupID {
			return true
		}
	}
	return false
}

func (s *UserSubscription) AccessibleGroupIDs() []int64 {
	if s == nil {
		return nil
	}
	if len(s.GroupIDs) == 0 {
		return []int64{s.GroupID}
	}
	return s.GroupIDs
}

func (s *UserSubscription) DailyLimit(group *Group) *float64 {
	if s != nil && s.PlanID != nil {
		return s.DailyLimitUSD
	}
	if group != nil {
		return group.DailyLimitUSD
	}
	return nil
}

func (s *UserSubscription) WeeklyLimit(group *Group) *float64 {
	if s != nil && s.PlanID != nil {
		return s.WeeklyLimitUSD
	}
	if group != nil {
		return group.WeeklyLimitUSD
	}
	return nil
}

func (s *UserSubscription) MonthlyLimit(group *Group) *float64 {
	if s != nil && s.PlanID != nil {
		return s.MonthlyLimitUSD
	}
	if group != nil {
		return group.MonthlyLimitUSD
	}
	return nil
}

func applyPurchasedBundle(sub, previous *UserSubscription, input *AssignSubscriptionInput, previousActive bool) {
	if sub == nil || input == nil || input.PlanID == nil {
		return
	}
	groupIDs := input.GroupIDs
	daily, weekly, monthly := input.DailyLimitUSD, input.WeeklyLimitUSD, input.MonthlyLimitUSD
	if previousActive && previous != nil {
		groupIDs, _ = normalizePlanGroupIDs(previous.GroupID, append(append([]int64{}, previous.AccessibleGroupIDs()...), input.GroupIDs...))
		daily = lessRestrictiveSubscriptionLimit(previous.DailyLimit(previous.Group), daily)
		weekly = lessRestrictiveSubscriptionLimit(previous.WeeklyLimit(previous.Group), weekly)
		monthly = lessRestrictiveSubscriptionLimit(previous.MonthlyLimit(previous.Group), monthly)
	}
	sub.GroupIDs = groupIDs
	sub.PlanID = input.PlanID
	sub.DailyLimitUSD = daily
	sub.WeeklyLimitUSD = weekly
	sub.MonthlyLimitUSD = monthly
}

func lessRestrictiveSubscriptionLimit(previous, purchased *float64) *float64 {
	if previous == nil || purchased == nil {
		return nil
	}
	if *previous >= *purchased {
		return previous
	}
	return purchased
}
