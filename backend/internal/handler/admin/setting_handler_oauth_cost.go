package admin

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"

	"github.com/gin-gonic/gin"
)

type oauthCostResponse struct {
	PurchaseCostCNY float64 `json:"purchase_cost_cny"`
}

type updateOAuthCostRequest struct {
	PurchaseCostCNY float64 `json:"purchase_cost_cny"`
}

// GetOAuthCost returns the stored OAuth purchase cost.
// GET /api/v1/admin/settings/oauth-cost
func (h *SettingHandler) GetOAuthCost(c *gin.Context) {
	amount, err := h.settingService.GetOAuthPurchaseCostCNY(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, oauthCostResponse{PurchaseCostCNY: amount})
}

// UpdateOAuthCost stores the OAuth purchase cost.
// PUT /api/v1/admin/settings/oauth-cost
func (h *SettingHandler) UpdateOAuthCost(c *gin.Context) {
	var req updateOAuthCostRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if err := h.settingService.SetOAuthPurchaseCostCNY(c.Request.Context(), req.PurchaseCostCNY); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.Success(c, oauthCostResponse{PurchaseCostCNY: req.PurchaseCostCNY})
}
