package admin

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// GetJuiceFixerConfig 获取 Juice 值修正配置
// GET /api/v1/admin/settings/juice-fixer
func (h *SettingHandler) GetJuiceFixerConfig(c *gin.Context) {
	setting := h.settingService.GetJuiceFixerSetting(c.Request.Context())
	response.Success(c, setting)
}

// UpdateJuiceFixerConfig 更新 Juice 值修正配置
// PUT /api/v1/admin/settings/juice-fixer
func (h *SettingHandler) UpdateJuiceFixerConfig(c *gin.Context) {
	var req service.JuiceFixerSetting
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	if err := h.settingService.SetJuiceFixerSetting(c.Request.Context(), &req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	response.Success(c, h.settingService.GetJuiceFixerSetting(c.Request.Context()))
}
