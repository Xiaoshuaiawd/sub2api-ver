//go:build unit

package service

import (
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
)

func TestPaymentDashboardDoesNotCountWalletTransferAsNewRevenue(t *testing.T) {
	now := time.Now()
	orders := []*dbent.PaymentOrder{
		{PaymentType: payment.TypeAlipay, PayAmount: 10, PaidAt: &now},
		{PaymentType: payment.TypeWallet, PayAmount: 5, PaidAt: &now},
	}
	var stats DashboardStats
	computeBasicStats(&stats, externalPaymentOrders(orders), now.Add(-time.Hour))
	require.Equal(t, 1, stats.TotalCount)
	require.InDelta(t, 10, stats.TotalAmount[payment.DefaultPaymentCurrency], 1e-8)
}
