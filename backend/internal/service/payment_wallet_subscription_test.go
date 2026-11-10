//go:build unit

package service_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	"github.com/Wei-Shaw/sub2api/ent/paymentorder"
	"github.com/Wei-Shaw/sub2api/ent/usersubscription"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

type walletPaymentSettings struct{ service.SettingRepository }

type failingWalletSubscriptionRepo struct {
	service.UserSubscriptionRepository
}

// SQLite cannot express FOR UPDATE; the wallet purchase's user-row write still
// serializes purchases and the subscription update stays in the same tx.
type walletSQLiteSubscriptionRepo struct {
	service.UserSubscriptionRepository
}

func (r walletSQLiteSubscriptionRepo) GetByIDForUpdate(ctx context.Context, id int64) (*service.UserSubscription, error) {
	return r.UserSubscriptionRepository.GetByID(ctx, id)
}

func (failingWalletSubscriptionRepo) Create(context.Context, *service.UserSubscription) error {
	return errors.New("subscription write failed")
}

func (walletPaymentSettings) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	values := make(map[string]string, len(keys))
	values[service.SettingPaymentEnabled] = "true"
	return values, nil
}

func walletPaymentFixture(t *testing.T, balance, price float64, failAssignment ...bool) (*service.PaymentService, *dbent.Client, int64, int64) {
	t.Helper()
	name := strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	db, err := sql.Open("sqlite", fmt.Sprintf("file:%s?mode=memory&cache=shared&_fk=1", name))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(entsql.OpenDB(dialect.SQLite, db))))
	t.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()
	user, err := client.User.Create().SetEmail("wallet@example.com").SetUsername("wallet-user").
		SetPasswordHash("hash").SetRole("user").SetStatus("active").SetConcurrency(5).SetBalance(balance).Save(ctx)
	require.NoError(t, err)
	group, err := client.Group.Create().SetName("wallet-subscription").SetPlatform("openai").
		SetStatus("active").SetSubscriptionType("subscription").SetRateMultiplier(1).Save(ctx)
	require.NoError(t, err)
	plan, err := client.SubscriptionPlan.Create().SetGroupID(group.ID).SetGroupIds([]int64{group.ID}).
		SetName("Plan").SetPrice(price).SetValidityDays(30).SetValidityUnit("day").SetForSale(true).Save(ctx)
	require.NoError(t, err)
	groupRepo := repository.NewGroupRepository(client, db)
	var subRepo service.UserSubscriptionRepository = walletSQLiteSubscriptionRepo{repository.NewUserSubscriptionRepository(client)}
	if len(failAssignment) > 0 && failAssignment[0] {
		subRepo = failingWalletSubscriptionRepo{subRepo}
	}
	subSvc := service.NewSubscriptionService(groupRepo, subRepo, nil, client, nil)
	configSvc := service.NewPaymentConfigService(client, walletPaymentSettings{}, nil)
	svc := service.NewPaymentService(client, nil, nil, nil, subSvc, configSvc, repository.NewUserRepository(client, db), groupRepo, nil)
	return svc, client, user.ID, plan.ID
}

func TestWalletSubscriptionPurchaseIsAtomicAndIdempotent(t *testing.T) {
	svc, client, userID, planID := walletPaymentFixture(t, 12, 5)
	request := service.CreateOrderRequest{UserID: userID, OrderType: payment.OrderTypeSubscription, PaymentType: payment.TypeWallet, PlanID: planID, IdempotencyKey: uuid.NewString()}

	first, err := svc.CreateOrder(context.Background(), request)
	require.NoError(t, err)
	require.Equal(t, service.OrderStatusCompleted, first.Status)
	require.Equal(t, 5.0, first.PayAmount)
	require.Equal(t, "USD", first.Currency)
	order, err := client.PaymentOrder.Get(context.Background(), first.OrderID)
	require.NoError(t, err)
	require.Equal(t, "USD", service.PaymentOrderCurrency(order))
	user, err := client.User.Get(context.Background(), userID)
	require.NoError(t, err)
	require.InDelta(t, 7, user.Balance, 1e-8)
	subs, err := client.UserSubscription.Query().Where(usersubscription.UserIDEQ(userID)).All(context.Background())
	require.NoError(t, err)
	require.Len(t, subs, 1)

	second, err := svc.CreateOrder(context.Background(), request)
	require.NoError(t, err)
	require.Equal(t, first.OrderID, second.OrderID)
	user, err = client.User.Get(context.Background(), userID)
	require.NoError(t, err)
	require.InDelta(t, 7, user.Balance, 1e-8)
	require.Equal(t, 1, client.PaymentOrder.Query().Where(paymentorder.UserIDEQ(userID)).CountX(context.Background()))
}

func TestWalletSubscriptionPurchaseRejectsInsufficientBalanceWithoutOrder(t *testing.T) {
	svc, client, userID, planID := walletPaymentFixture(t, 3, 5)
	_, err := svc.CreateOrder(context.Background(), service.CreateOrderRequest{
		UserID: userID, OrderType: payment.OrderTypeSubscription, PaymentType: payment.TypeWallet,
		PlanID: planID, IdempotencyKey: uuid.NewString(),
	})
	require.ErrorIs(t, err, service.ErrInsufficientBalance)
	require.Zero(t, client.PaymentOrder.Query().Where(paymentorder.UserIDEQ(userID)).CountX(context.Background()))
	require.Zero(t, client.UserSubscription.Query().Where(usersubscription.UserIDEQ(userID)).CountX(context.Background()))
	user, err := client.User.Get(context.Background(), userID)
	require.NoError(t, err)
	require.InDelta(t, 3, user.Balance, 1e-8)
}

func TestWalletSubscriptionPurchaseRollsBackWhenAssignmentFails(t *testing.T) {
	svc, client, userID, planID := walletPaymentFixture(t, 12, 5, true)
	_, err := svc.CreateOrder(context.Background(), service.CreateOrderRequest{
		UserID: userID, OrderType: payment.OrderTypeSubscription, PaymentType: payment.TypeWallet,
		PlanID: planID, IdempotencyKey: uuid.NewString(),
	})
	require.ErrorContains(t, err, "subscription write failed")
	user, err := client.User.Get(context.Background(), userID)
	require.NoError(t, err)
	require.InDelta(t, 12, user.Balance, 1e-8)
	require.Zero(t, client.PaymentOrder.Query().Where(paymentorder.UserIDEQ(userID)).CountX(context.Background()))
}

func TestWalletSubscriptionPurchaseRejectsInvalidKeyAndWrongOrderType(t *testing.T) {
	svc, client, userID, planID := walletPaymentFixture(t, 12, 5)
	for _, request := range []service.CreateOrderRequest{
		{UserID: userID, OrderType: payment.OrderTypeSubscription, PaymentType: payment.TypeWallet, PlanID: planID, IdempotencyKey: "not-a-uuid"},
		{UserID: userID, OrderType: payment.OrderTypeBalance, PaymentType: payment.TypeWallet, PlanID: planID, IdempotencyKey: uuid.NewString()},
	} {
		_, err := svc.CreateOrder(context.Background(), request)
		require.Error(t, err)
	}
	require.Zero(t, client.PaymentOrder.Query().Where(paymentorder.UserIDEQ(userID)).CountX(context.Background()))
	user, err := client.User.Get(context.Background(), userID)
	require.NoError(t, err)
	require.InDelta(t, 12, user.Balance, 1e-8)
}

func TestWalletSubscriptionPurchaseRenewsExistingPlan(t *testing.T) {
	svc, client, userID, planID := walletPaymentFixture(t, 12, 5)
	request := service.CreateOrderRequest{UserID: userID, OrderType: payment.OrderTypeSubscription, PaymentType: payment.TypeWallet, PlanID: planID, IdempotencyKey: uuid.NewString()}
	_, err := svc.CreateOrder(context.Background(), request)
	require.NoError(t, err)
	first, err := client.UserSubscription.Query().Where(usersubscription.UserIDEQ(userID)).Only(context.Background())
	require.NoError(t, err)

	request.IdempotencyKey = uuid.NewString()
	_, err = svc.CreateOrder(context.Background(), request)
	require.NoError(t, err)
	renewed, err := client.UserSubscription.Query().Where(usersubscription.UserIDEQ(userID)).Only(context.Background())
	require.NoError(t, err)
	require.True(t, renewed.ExpiresAt.After(first.ExpiresAt))
	require.Equal(t, 2, client.PaymentOrder.Query().Where(paymentorder.UserIDEQ(userID)).CountX(context.Background()))
	user, err := client.User.Get(context.Background(), userID)
	require.NoError(t, err)
	require.InDelta(t, 2, user.Balance, 1e-8)
}

func TestWalletSubscriptionPurchaseKeepsSharedPlanGroups(t *testing.T) {
	svc, client, userID, planID := walletPaymentFixture(t, 12, 5)
	ctx := context.Background()
	plan, err := client.SubscriptionPlan.Get(ctx, planID)
	require.NoError(t, err)
	secondGroup, err := client.Group.Create().SetName("wallet-second-group").SetPlatform("openai").
		SetStatus("active").SetSubscriptionType("subscription").SetRateMultiplier(1).Save(ctx)
	require.NoError(t, err)
	_, err = client.SubscriptionPlan.UpdateOneID(planID).SetGroupIds([]int64{plan.GroupID, secondGroup.ID}).Save(ctx)
	require.NoError(t, err)

	result, err := svc.CreateOrder(ctx, service.CreateOrderRequest{
		UserID: userID, OrderType: payment.OrderTypeSubscription, PaymentType: payment.TypeWallet,
		PlanID: planID, IdempotencyKey: uuid.NewString(),
	})
	require.NoError(t, err)
	order, err := client.PaymentOrder.Get(ctx, result.OrderID)
	require.NoError(t, err)
	require.Equal(t, []int64{plan.GroupID, secondGroup.ID}, order.SubscriptionGroupIds)
	sub, err := client.UserSubscription.Query().Where(usersubscription.UserIDEQ(userID)).Only(ctx)
	require.NoError(t, err)
	require.Equal(t, []int64{plan.GroupID, secondGroup.ID}, sub.GroupIds)
}
