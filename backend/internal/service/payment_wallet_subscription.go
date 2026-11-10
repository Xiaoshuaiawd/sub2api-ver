package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentorder"
	dbuser "github.com/Wei-Shaw/sub2api/ent/user"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/google/uuid"
)

func walletSubscriptionTradeNo(userID int64, key string) (string, error) {
	parsed, err := uuid.Parse(strings.TrimSpace(key))
	if err != nil || parsed == uuid.Nil {
		return "", infraerrors.BadRequest("INVALID_IDEMPOTENCY_KEY", "wallet purchase requires a UUID idempotency key")
	}
	return "wallet-" + strconv.FormatInt(userID, 10) + "-" + strings.ReplaceAll(parsed.String(), "-", ""), nil
}

func (s *PaymentService) existingWalletSubscriptionOrder(ctx context.Context, client *dbent.Client, tradeNo string, userID, planID int64) (*CreateOrderResponse, error) {
	order, err := client.PaymentOrder.Query().Where(paymentorder.OutTradeNo(tradeNo)).Only(ctx)
	if dbent.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if order.UserID != userID || order.OrderType != payment.OrderTypeSubscription ||
		order.PaymentType != payment.TypeWallet || order.PlanID == nil || *order.PlanID != planID {
		return nil, infraerrors.Conflict("IDEMPOTENCY_KEY_REUSED", "idempotency key belongs to another purchase")
	}
	if order.Status != OrderStatusCompleted {
		return nil, infraerrors.Conflict("WALLET_PURCHASE_IN_PROGRESS", "wallet purchase is being processed")
	}
	return walletSubscriptionResponse(order), nil
}

func walletSubscriptionResponse(order *dbent.PaymentOrder) *CreateOrderResponse {
	return &CreateOrderResponse{
		OrderID: order.ID, Amount: order.Amount, PayAmount: order.PayAmount,
		Status: order.Status, PaymentType: payment.TypeWallet,
		OutTradeNo: order.OutTradeNo, Currency: "USD", ExpiresAt: order.ExpiresAt,
	}
}

// A wallet purchase has no external provider callback. Balance, receipt, and
// subscription entitlement therefore commit together in one transaction.
func (s *PaymentService) createWalletSubscriptionOrder(ctx context.Context, req CreateOrderRequest, cfg *PaymentConfig) (*CreateOrderResponse, error) {
	if req.OrderType != payment.OrderTypeSubscription {
		return nil, infraerrors.BadRequest("INVALID_ORDER_TYPE", "wallet can only purchase a subscription")
	}
	tradeNo, err := walletSubscriptionTradeNo(req.UserID, req.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	if previous, err := s.existingWalletSubscriptionOrder(ctx, s.entClient, tradeNo, req.UserID, req.PlanID); previous != nil || err != nil {
		return previous, err
	}
	if !cfg.Enabled {
		return nil, infraerrors.Forbidden("PAYMENT_DISABLED", "payment system is disabled")
	}
	plan, err := s.validateSubOrder(ctx, req)
	if err != nil {
		return nil, err
	}
	if math.IsNaN(plan.Price) || math.IsInf(plan.Price, 0) || plan.Price <= 0 {
		return nil, infraerrors.BadRequest("PLAN_PRICE_INVALID", "subscription plan price must be positive")
	}
	if s.subscriptionSvc == nil {
		return nil, infraerrors.ServiceUnavailable("SUBSCRIPTION_UNAVAILABLE", "subscription service is unavailable")
	}
	user, err := s.userRepo.GetByID(ctx, req.UserID)
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}
	if user.Status != payment.EntityStatusActive {
		return nil, infraerrors.Forbidden("USER_INACTIVE", "user account is disabled")
	}

	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin wallet purchase: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	txCtx := dbent.NewTxContext(ctx, tx)
	txClient := tx.Client()
	// Serialize purchases for this user before checking the idempotency key.
	// A retry waiting on the lock sees the first committed order and cannot debit twice.
	locked, err := txClient.User.Update().Where(dbuser.IDEQ(req.UserID), dbuser.StatusEQ(payment.EntityStatusActive)).AddBalance(0).Save(txCtx)
	if err != nil {
		return nil, fmt.Errorf("lock wallet balance: %w", err)
	}
	if locked != 1 {
		return nil, infraerrors.Forbidden("USER_INACTIVE", "user account is disabled")
	}
	if previous, err := s.existingWalletSubscriptionOrder(txCtx, txClient, tradeNo, req.UserID, req.PlanID); previous != nil || err != nil {
		return previous, err
	}
	if err := s.checkDailyLimit(txCtx, tx, req.UserID, plan.Price, cfg.DailyLimit); err != nil {
		return nil, err
	}
	deducted, err := txClient.User.Update().Where(dbuser.IDEQ(req.UserID), dbuser.BalanceGTE(plan.Price)).AddBalance(-plan.Price).Save(txCtx)
	if err != nil {
		return nil, fmt.Errorf("deduct wallet balance: %w", err)
	}
	if deducted != 1 {
		return nil, ErrInsufficientBalance
	}

	groupIDs := plan.GroupIds
	if len(groupIDs) == 0 {
		groupIDs = []int64{plan.GroupID}
	}
	now := time.Now()
	days := psComputeValidityDays(plan.ValidityDays, plan.ValidityUnit)
	orderTimeoutMin := cfg.OrderTimeoutMin
	if orderTimeoutMin <= 0 {
		orderTimeoutMin = defaultOrderTimeoutMin
	}
	order, err := txClient.PaymentOrder.Create().
		SetUserID(req.UserID).SetUserEmail(user.Email).SetUserName(user.Username).
		SetAmount(plan.Price).SetPayAmount(plan.Price).SetFeeRate(0).SetBonusAmount(0).
		SetRechargeCode("").SetOutTradeNo(tradeNo).SetPaymentType(payment.TypeWallet).
		SetPaymentTradeNo("").SetOrderType(payment.OrderTypeSubscription).
		SetStatus(OrderStatusCompleted).SetPaidAt(now).SetCompletedAt(now).SetExpiresAt(now.Add(time.Duration(orderTimeoutMin) * time.Minute)).
		SetClientIP(req.ClientIP).SetSrcHost(req.SrcHost).
		SetProviderSnapshot(map[string]any{"schema_version": 2, "provider_key": payment.TypeWallet, "currency": "USD"}).
		SetPlanID(plan.ID).SetSubscriptionGroupID(groupIDs[0]).SetSubscriptionGroupIds(groupIDs).
		SetSubscriptionLimitsSnapshot(true).
		SetNillableSubscriptionDailyLimitUsd(plan.DailyLimitUsd).
		SetNillableSubscriptionWeeklyLimitUsd(plan.WeeklyLimitUsd).
		SetNillableSubscriptionMonthlyLimitUsd(plan.MonthlyLimitUsd).
		SetSubscriptionDays(days).Save(txCtx)
	if err != nil {
		return nil, fmt.Errorf("create wallet purchase order: %w", err)
	}
	assigned, _, err := s.subscriptionSvc.assignOrExtendSubscription(txCtx, &AssignSubscriptionInput{
		UserID: req.UserID, GroupID: groupIDs[0], GroupIDs: groupIDs,
		PlanID: &plan.ID, DailyLimitUSD: plan.DailyLimitUsd,
		WeeklyLimitUSD: plan.WeeklyLimitUsd, MonthlyLimitUSD: plan.MonthlyLimitUsd,
		ValidityDays: days, AssignedBy: 0, Notes: paymentSubscriptionOrderNote(order.ID),
	}, true)
	if err != nil {
		return nil, fmt.Errorf("assign wallet subscription: %w", err)
	}
	order, err = txClient.PaymentOrder.UpdateOneID(order.ID).SetSubscriptionID(assigned.ID).Save(txCtx)
	if err != nil {
		return nil, fmt.Errorf("link wallet subscription: %w", err)
	}
	detail, _ := json.Marshal(map[string]any{"plan_id": plan.ID, "amount_usd": plan.Price, "subscription_id": assigned.ID})
	if _, err := txClient.PaymentAuditLog.Create().SetOrderID(strconv.FormatInt(order.ID, 10)).
		SetAction("WALLET_SUBSCRIPTION_PURCHASE").SetDetail(string(detail)).SetOperator("user").Save(txCtx); err != nil {
		return nil, fmt.Errorf("audit wallet purchase: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit wallet purchase: %w", err)
	}
	if err := s.invalidatePaymentSubscriptionGroupCaches(req.UserID, groupIDs); err != nil {
		slog.Warn("wallet purchase subscription cache invalidation failed", "order_id", order.ID, "error", err)
	}
	if s.subscriptionSvc.billingCacheService != nil {
		cacheCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := s.subscriptionSvc.billingCacheService.InvalidateUserBalance(cacheCtx, req.UserID); err != nil {
			slog.Warn("wallet purchase balance cache invalidation failed", "order_id", order.ID, "error", err)
		}
		cancel()
	}
	s.dispatchPaymentFulfillmentNotification(order, "SUBSCRIPTION_SUCCESS")
	return walletSubscriptionResponse(order), nil
}
