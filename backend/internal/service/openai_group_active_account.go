package service

import "context"

const openAIGroupActiveAccountSessionPrefix = "__openai_group_active_account__:"

func openAIGroupActiveAccountSessionHash(platform string) string {
	return openAIGroupActiveAccountSessionPrefix + normalizeOpenAICompatiblePlatform(platform)
}

// selectOpenAIGroupActiveAccount keeps all requests in an OpenAI group on the
// same active account, independent of the caller's session hash. The sticky
// lookup validates schedulability and clears the binding when it is unavailable.
func (s *OpenAIGatewayService) selectOpenAIGroupActiveAccount(
	ctx context.Context,
	groupID *int64,
	platform string,
	requestedModel string,
	excludedIDs map[int64]struct{},
	requireCompact bool,
	requiredCapability OpenAIEndpointCapability,
) (*AccountSelectionResult, error) {
	if s == nil || normalizeOpenAICompatiblePlatform(platform) != PlatformOpenAI || s.cache == nil {
		return nil, nil
	}

	account := s.tryStickySessionHit(
		ctx,
		groupID,
		platform,
		openAIGroupActiveAccountSessionHash(platform),
		requestedModel,
		excludedIDs,
		requireCompact,
		0,
		requiredCapability,
	)
	if account == nil {
		return nil, nil
	}

	result, err := s.tryAcquireAccountSlot(ctx, account.ID, account.Concurrency)
	if err == nil && result != nil && result.Acquired {
		return s.newAcquiredSelectionResult(ctx, account, result.ReleaseFunc)
	}

	cfg := s.schedulingConfig()
	return s.newSelectionResult(ctx, account, false, nil, &AccountWaitPlan{
		AccountID:      account.ID,
		MaxConcurrency: account.Concurrency,
		Timeout:        cfg.StickySessionWaitTimeout,
		MaxWaiting:     cfg.StickySessionMaxWaiting,
	})
}

func (s *OpenAIGatewayService) bindOpenAIGroupActiveAccount(ctx context.Context, groupID *int64, platform string, accountID int64) {
	if s == nil || normalizeOpenAICompatiblePlatform(platform) != PlatformOpenAI || accountID <= 0 {
		return
	}
	_ = s.setStickySessionAccountID(ctx, groupID, openAIGroupActiveAccountSessionHash(platform), accountID, openaiStickySessionTTL)
}

func (s *OpenAIGatewayService) refreshOpenAIGroupActiveAccount(ctx context.Context, groupID *int64, platform string) {
	if s == nil || normalizeOpenAICompatiblePlatform(platform) != PlatformOpenAI {
		return
	}
	_ = s.refreshStickySessionTTL(ctx, groupID, openAIGroupActiveAccountSessionHash(platform), openaiStickySessionTTL)
}
