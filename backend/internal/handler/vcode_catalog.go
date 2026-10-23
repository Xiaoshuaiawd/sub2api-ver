package handler

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type vcodeCatalogModel struct {
	ID       string `json:"id"`
	Platform string `json:"platform"`
}

type vcodeCatalogGroup struct {
	ID                 int64               `json:"id"`
	Name               string              `json:"name"`
	Platform           string              `json:"platform"`
	RateMultiplier     float64             `json:"rate_multiplier"`
	UserRateMultiplier *float64            `json:"user_rate_multiplier,omitempty"`
	Models             []vcodeCatalogModel `json:"models"`
}

// buildVcodeCatalog 只展示当前用户能绑定的活跃分组；模型白名单与网关使用同一判定。
// 目录只是选择提示，最终准入仍由网关在请求时重新检查。
func buildVcodeCatalog(available []service.Group, plaza []service.PlazaGroup, rates map[int64]float64) []vcodeCatalogGroup {
	visible := make(map[int64]*service.Group, len(available))
	for i := range available {
		visible[available[i].ID] = &available[i]
	}
	out := make([]vcodeCatalogGroup, 0, len(plaza))
	for _, group := range plaza {
		permission := visible[group.ID]
		// Vcode Agent 不是 Claude Code 客户端；这类分组只能跨组兜底，
		// 委托请求为保证所选组计费已禁用兜底，因此不能展示为可用模型。
		if permission == nil || permission.ClaudeCodeOnly {
			continue
		}
		models := make([]vcodeCatalogModel, 0, len(group.Models))
		for _, model := range group.Models {
			// Vcode Agent 的模型选择器只调用文本协议；图片/视频专用计费模型不能误当聊天模型。
			if model.Pricing != nil && (model.Pricing.BillingMode == service.BillingModeImage || model.Pricing.BillingMode == service.BillingModeVideo) {
				continue
			}
			if permission.ModelAllowlistEnabled() && !permission.ModelAllowlist.Allows(model.Name) {
				continue
			}
			models = append(models, vcodeCatalogModel{ID: model.Name, Platform: model.Platform})
		}
		if len(models) == 0 {
			continue
		}
		item := vcodeCatalogGroup{
			ID: group.ID, Name: group.Name, Platform: group.Platform,
			RateMultiplier: group.RateMultiplier, Models: models,
		}
		if rate, ok := rates[group.ID]; ok {
			item.UserRateMultiplier = &rate
		}
		out = append(out, item)
	}
	return out
}

// GetVcodeCatalog 不依赖模型广场展示开关，按 API Key 可绑定权限返回选择目录。
func (h *ModelPlazaHandler) GetVcodeCatalog(c *gin.Context) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	groups, err := h.VcodeCatalogForUser(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, gin.H{"groups": groups})
}

// VcodeCatalogForUser 与 JWT 目录和服务间目录共用同一可绑定分组/模型投影。
func (h *ModelPlazaHandler) VcodeCatalogForUser(ctx context.Context, userID int64) ([]vcodeCatalogGroup, error) {
	available, err := h.apiKeyService.GetAvailableGroups(ctx, userID)
	if err != nil {
		return nil, err
	}
	groups, err := h.plazaService.ListGroups(ctx)
	if err != nil {
		return nil, err
	}
	rates, err := h.apiKeyService.GetUserGroupRates(ctx, userID)
	if err != nil {
		return nil, err
	}
	return buildVcodeCatalog(available, groups, rates), nil
}
