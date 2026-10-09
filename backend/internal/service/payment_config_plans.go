package service

import (
	"context"
	"fmt"
	"math"
	"strings"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/group"
	"github.com/Wei-Shaw/sub2api/ent/subscriptionplan"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// normalizePlanCurrency validates and normalizes the display-only currency label.
// Empty means "no label" and is kept as-is so existing plans stay unchanged.
func normalizePlanCurrency(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", nil
	}
	currency, err := payment.NormalizePaymentCurrency(raw)
	if err != nil {
		return "", infraerrors.BadRequest("PLAN_CURRENCY_INVALID", "currency must be a 3-letter ISO currency code")
	}
	return currency, nil
}

// validatePlanRequired checks that all required fields for a plan are provided.
func validatePlanRequired(name string, groupID int64, price float64, validityDays int, validityUnit string, originalPrice *float64) error {
	if strings.TrimSpace(name) == "" {
		return infraerrors.BadRequest("PLAN_NAME_REQUIRED", "plan name is required")
	}
	if groupID <= 0 {
		return infraerrors.BadRequest("PLAN_GROUP_REQUIRED", "group is required")
	}
	if price <= 0 {
		return infraerrors.BadRequest("PLAN_PRICE_INVALID", "price must be > 0")
	}
	if validityDays <= 0 {
		return infraerrors.BadRequest("PLAN_VALIDITY_REQUIRED", "validity days must be > 0")
	}
	if strings.TrimSpace(validityUnit) == "" {
		return infraerrors.BadRequest("PLAN_VALIDITY_UNIT_REQUIRED", "validity unit is required")
	}
	if originalPrice != nil && *originalPrice < 0 {
		return infraerrors.BadRequest("PLAN_ORIGINAL_PRICE_INVALID", "original price must be >= 0")
	}
	return nil
}

// validatePlanPatch validates only the non-nil fields in a patch update.
func validatePlanPatch(req UpdatePlanRequest) error {
	if req.Name != nil && strings.TrimSpace(*req.Name) == "" {
		return infraerrors.BadRequest("PLAN_NAME_REQUIRED", "plan name is required")
	}
	if req.GroupID != nil && *req.GroupID <= 0 {
		return infraerrors.BadRequest("PLAN_GROUP_REQUIRED", "group is required")
	}
	if req.Price != nil && *req.Price <= 0 {
		return infraerrors.BadRequest("PLAN_PRICE_INVALID", "price must be > 0")
	}
	if req.ValidityDays != nil && *req.ValidityDays <= 0 {
		return infraerrors.BadRequest("PLAN_VALIDITY_REQUIRED", "validity days must be > 0")
	}
	if req.ValidityUnit != nil && strings.TrimSpace(*req.ValidityUnit) == "" {
		return infraerrors.BadRequest("PLAN_VALIDITY_UNIT_REQUIRED", "validity unit is required")
	}
	if req.OriginalPrice != nil && *req.OriginalPrice < 0 {
		return infraerrors.BadRequest("PLAN_ORIGINAL_PRICE_INVALID", "original price must be >= 0")
	}
	return nil
}

func normalizeSharedPlanLimit(limit *float64) (*float64, error) {
	if limit == nil || *limit == 0 {
		return nil, nil
	}
	if math.IsNaN(*limit) || math.IsInf(*limit, 0) || *limit < 0 {
		return nil, infraerrors.BadRequest("PLAN_LIMIT_INVALID", "shared limit must be a finite non-negative amount")
	}
	return limit, nil
}

func (s *PaymentConfigService) validatePlanGroups(ctx context.Context, ids []int64) error {
	groups, err := s.entClient.Group.Query().Where(group.IDIn(ids...)).All(ctx)
	if err != nil {
		return err
	}
	if len(groups) != len(ids) {
		return infraerrors.BadRequest("PLAN_GROUP_INVALID", "one or more subscription groups do not exist")
	}
	for _, g := range groups {
		if !isSubscriptionBillingType(g.SubscriptionType) || g.Status != StatusActive {
			return infraerrors.BadRequest("PLAN_GROUP_INVALID", "all selected groups must be active subscription groups")
		}
	}
	return nil
}

// --- Plan CRUD ---

// PlanGroupInfo holds the group details needed for subscription plan display.
type PlanGroupInfo struct {
	Platform           string   `json:"platform"`
	Name               string   `json:"name"`
	RateMultiplier     float64  `json:"rate_multiplier"`
	PeakRateEnabled    bool     `json:"peak_rate_enabled"`
	PeakStart          string   `json:"peak_start"`
	PeakEnd            string   `json:"peak_end"`
	PeakRateMultiplier float64  `json:"peak_rate_multiplier"`
	DailyLimitUSD      *float64 `json:"daily_limit_usd"`
	WeeklyLimitUSD     *float64 `json:"weekly_limit_usd"`
	MonthlyLimitUSD    *float64 `json:"monthly_limit_usd"`
	ModelScopes        []string `json:"supported_model_scopes"`
}

type PlanGroupSummary struct {
	ID             int64   `json:"id"`
	Name           string  `json:"name"`
	Platform       string  `json:"platform"`
	RateMultiplier float64 `json:"rate_multiplier"`
}

func PlanGroupsForResponse(plan *dbent.SubscriptionPlan, groupInfo map[int64]PlanGroupInfo) []PlanGroupSummary {
	if plan == nil {
		return nil
	}
	ids := plan.GroupIds
	if len(ids) == 0 {
		ids = []int64{plan.GroupID}
	}
	result := make([]PlanGroupSummary, 0, len(ids))
	for _, id := range ids {
		info := groupInfo[id]
		if info.Name == "" {
			info.Name = fmt.Sprintf("#%d", id)
		}
		result = append(result, PlanGroupSummary{ID: id, Name: info.Name, Platform: info.Platform, RateMultiplier: info.RateMultiplier})
	}
	return result
}

func PlanLimitsForResponse(plan *dbent.SubscriptionPlan, primary PlanGroupInfo) (daily, weekly, monthly *float64) {
	if plan == nil {
		return nil, nil, nil
	}
	if len(plan.GroupIds) == 0 {
		return primary.DailyLimitUSD, primary.WeeklyLimitUSD, primary.MonthlyLimitUSD
	}
	return plan.DailyLimitUsd, plan.WeeklyLimitUsd, plan.MonthlyLimitUsd
}

// GetGroupInfoMap returns a map of group_id → PlanGroupInfo for the given plans.
func (s *PaymentConfigService) GetGroupInfoMap(ctx context.Context, plans []*dbent.SubscriptionPlan) map[int64]PlanGroupInfo {
	ids := make([]int64, 0, len(plans))
	seen := make(map[int64]bool)
	for _, p := range plans {
		groupIDs := p.GroupIds
		if len(groupIDs) == 0 {
			groupIDs = []int64{p.GroupID}
		}
		for _, id := range groupIDs {
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	if len(ids) == 0 {
		return nil
	}
	groups, err := s.entClient.Group.Query().Where(group.IDIn(ids...)).All(ctx)
	if err != nil {
		return nil
	}
	m := make(map[int64]PlanGroupInfo, len(groups))
	for _, g := range groups {
		subscriptionRate := g.RateMultiplier
		if g.SubscriptionType == SubscriptionTypeSubscriptionBalance && g.SubscriptionRateMultiplier != nil {
			subscriptionRate = *g.SubscriptionRateMultiplier
		}
		m[int64(g.ID)] = PlanGroupInfo{
			Platform:           g.Platform,
			Name:               g.Name,
			RateMultiplier:     subscriptionRate,
			PeakRateEnabled:    g.PeakRateEnabled,
			PeakStart:          g.PeakStart,
			PeakEnd:            g.PeakEnd,
			PeakRateMultiplier: g.PeakRateMultiplier,
			DailyLimitUSD:      g.DailyLimitUsd,
			WeeklyLimitUSD:     g.WeeklyLimitUsd,
			MonthlyLimitUSD:    g.MonthlyLimitUsd,
			ModelScopes:        g.SupportedModelScopes,
		}
	}
	return m
}

func (s *PaymentConfigService) ListPlans(ctx context.Context) ([]*dbent.SubscriptionPlan, error) {
	return s.entClient.SubscriptionPlan.Query().Order(subscriptionplan.BySortOrder()).All(ctx)
}

func (s *PaymentConfigService) ListPlansForSale(ctx context.Context) ([]*dbent.SubscriptionPlan, error) {
	return s.entClient.SubscriptionPlan.Query().Where(subscriptionplan.ForSaleEQ(true)).Order(subscriptionplan.BySortOrder()).All(ctx)
}

func (s *PaymentConfigService) CreatePlan(ctx context.Context, req CreatePlanRequest) (*dbent.SubscriptionPlan, error) {
	groupIDs, err := normalizePlanGroupIDs(req.GroupID, req.GroupIDs)
	if err != nil {
		return nil, infraerrors.BadRequest("PLAN_GROUP_REQUIRED", err.Error())
	}
	if err := validatePlanRequired(req.Name, groupIDs[0], req.Price, req.ValidityDays, req.ValidityUnit, req.OriginalPrice); err != nil {
		return nil, err
	}
	if err := s.validatePlanGroups(ctx, groupIDs); err != nil {
		return nil, err
	}
	// Legacy single-group clients did not send plan caps; preserve their
	// previous group-limit behavior when creating a plan.
	if req.GroupIDs == nil {
		g, err := s.entClient.Group.Get(ctx, groupIDs[0])
		if err != nil {
			return nil, err
		}
		if req.DailyLimitUSD == nil {
			req.DailyLimitUSD = g.DailyLimitUsd
		}
		if req.WeeklyLimitUSD == nil {
			req.WeeklyLimitUSD = g.WeeklyLimitUsd
		}
		if req.MonthlyLimitUSD == nil {
			req.MonthlyLimitUSD = g.MonthlyLimitUsd
		}
	}
	daily, err := normalizeSharedPlanLimit(req.DailyLimitUSD)
	if err != nil {
		return nil, err
	}
	weekly, err := normalizeSharedPlanLimit(req.WeeklyLimitUSD)
	if err != nil {
		return nil, err
	}
	monthly, err := normalizeSharedPlanLimit(req.MonthlyLimitUSD)
	if err != nil {
		return nil, err
	}
	currency, err := normalizePlanCurrency(req.Currency)
	if err != nil {
		return nil, err
	}
	b := s.entClient.SubscriptionPlan.Create().
		SetGroupID(groupIDs[0]).SetGroupIds(groupIDs).
		SetNillableDailyLimitUsd(daily).SetNillableWeeklyLimitUsd(weekly).SetNillableMonthlyLimitUsd(monthly).
		SetName(req.Name).SetDescription(req.Description).
		SetPrice(req.Price).SetCurrency(currency).SetValidityDays(req.ValidityDays).SetValidityUnit(req.ValidityUnit).
		SetFeatures(req.Features).SetProductName(req.ProductName).
		SetForSale(req.ForSale).SetSortOrder(req.SortOrder)
	if req.OriginalPrice != nil {
		b.SetOriginalPrice(*req.OriginalPrice)
	}
	return b.Save(ctx)
}

// UpdatePlan updates a subscription plan by ID (patch semantics).
// NOTE: This function exceeds 30 lines due to per-field nil-check patch update boilerplate
// plus a validation guard for non-nil fields.
func (s *PaymentConfigService) UpdatePlan(ctx context.Context, id int64, req UpdatePlanRequest) (*dbent.SubscriptionPlan, error) {
	if err := validatePlanPatch(req); err != nil {
		return nil, err
	}
	u := s.entClient.SubscriptionPlan.UpdateOneID(id)
	if req.GroupID != nil || req.GroupIDs != nil {
		primary := int64(0)
		if req.GroupID != nil {
			primary = *req.GroupID
		}
		groupIDs, err := normalizePlanGroupIDs(primary, req.GroupIDs)
		if err != nil {
			return nil, infraerrors.BadRequest("PLAN_GROUP_REQUIRED", err.Error())
		}
		if req.GroupIDs != nil {
			current, err := s.entClient.SubscriptionPlan.Get(ctx, id)
			if err != nil {
				return nil, err
			}
			for _, groupID := range groupIDs {
				if groupID == current.GroupID {
					ordered := []int64{groupID}
					for _, candidate := range groupIDs {
						if candidate != groupID {
							ordered = append(ordered, candidate)
						}
					}
					groupIDs = ordered
					break
				}
			}
		}
		if err := s.validatePlanGroups(ctx, groupIDs); err != nil {
			return nil, err
		}
		u.SetGroupID(groupIDs[0]).SetGroupIds(groupIDs)
		if req.GroupIDs == nil {
			g, err := s.entClient.Group.Get(ctx, groupIDs[0])
			if err != nil {
				return nil, err
			}
			if req.DailyLimitUSD == nil {
				if g.DailyLimitUsd == nil {
					u.ClearDailyLimitUsd()
				} else {
					req.DailyLimitUSD = g.DailyLimitUsd
				}
			}
			if req.WeeklyLimitUSD == nil {
				if g.WeeklyLimitUsd == nil {
					u.ClearWeeklyLimitUsd()
				} else {
					req.WeeklyLimitUSD = g.WeeklyLimitUsd
				}
			}
			if req.MonthlyLimitUSD == nil {
				if g.MonthlyLimitUsd == nil {
					u.ClearMonthlyLimitUsd()
				} else {
					req.MonthlyLimitUSD = g.MonthlyLimitUsd
				}
			}
		}
	}
	if req.DailyLimitUSD != nil {
		limit, err := normalizeSharedPlanLimit(req.DailyLimitUSD)
		if err != nil {
			return nil, err
		}
		if limit == nil {
			u.ClearDailyLimitUsd()
		} else {
			u.SetDailyLimitUsd(*limit)
		}
	}
	if req.WeeklyLimitUSD != nil {
		limit, err := normalizeSharedPlanLimit(req.WeeklyLimitUSD)
		if err != nil {
			return nil, err
		}
		if limit == nil {
			u.ClearWeeklyLimitUsd()
		} else {
			u.SetWeeklyLimitUsd(*limit)
		}
	}
	if req.MonthlyLimitUSD != nil {
		limit, err := normalizeSharedPlanLimit(req.MonthlyLimitUSD)
		if err != nil {
			return nil, err
		}
		if limit == nil {
			u.ClearMonthlyLimitUsd()
		} else {
			u.SetMonthlyLimitUsd(*limit)
		}
	}
	if req.Name != nil {
		u.SetName(*req.Name)
	}
	if req.Description != nil {
		u.SetDescription(*req.Description)
	}
	if req.Price != nil {
		u.SetPrice(*req.Price)
	}
	if req.OriginalPrice != nil {
		u.SetOriginalPrice(*req.OriginalPrice)
	}
	if req.Currency != nil {
		currency, err := normalizePlanCurrency(*req.Currency)
		if err != nil {
			return nil, err
		}
		u.SetCurrency(currency)
	}
	if req.ValidityDays != nil {
		u.SetValidityDays(*req.ValidityDays)
	}
	if req.ValidityUnit != nil {
		u.SetValidityUnit(*req.ValidityUnit)
	}
	if req.Features != nil {
		u.SetFeatures(*req.Features)
	}
	if req.ProductName != nil {
		u.SetProductName(*req.ProductName)
	}
	if req.ForSale != nil {
		u.SetForSale(*req.ForSale)
	}
	if req.SortOrder != nil {
		u.SetSortOrder(*req.SortOrder)
	}
	return u.Save(ctx)
}

func (s *PaymentConfigService) DeletePlan(ctx context.Context, id int64) error {
	count, err := s.countPendingOrdersByPlan(ctx, id)
	if err != nil {
		return fmt.Errorf("check pending orders: %w", err)
	}
	if count > 0 {
		return infraerrors.Conflict("PENDING_ORDERS",
			fmt.Sprintf("this plan has %d in-progress orders and cannot be deleted — wait for orders to complete first", count))
	}
	return s.entClient.SubscriptionPlan.DeleteOneID(id).Exec(ctx)
}

// GetPlan returns a subscription plan by ID.
func (s *PaymentConfigService) GetPlan(ctx context.Context, id int64) (*dbent.SubscriptionPlan, error) {
	plan, err := s.entClient.SubscriptionPlan.Get(ctx, id)
	if err != nil {
		return nil, infraerrors.NotFound("PLAN_NOT_FOUND", "subscription plan not found")
	}
	return plan, nil
}
