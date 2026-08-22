package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

const DefaultOpenCodeProtocolVersion = "1.18.21"

const (
	openCodeProtocolCacheTTL = 60 * time.Second
	openCodeProtocolErrorTTL = 5 * time.Second
)

var openCodeProtocolVersionPattern = regexp.MustCompile(`^\d+(?:\.\d+)+$`)

type OpenCodeProtocolSettings struct {
	Enabled bool
	Version string
}

type cachedOpenCodeProtocolSettings struct {
	settings  OpenCodeProtocolSettings
	expiresAt int64
}

func NormalizeOpenCodeProtocolVersion(value string) (string, error) {
	version := strings.TrimSpace(value)
	if version == "" {
		return DefaultOpenCodeProtocolVersion, nil
	}
	if len(version) > 32 || !openCodeProtocolVersionPattern.MatchString(version) {
		return "", fmt.Errorf("opencode_protocol_version must be a dotted numeric version")
	}
	return version, nil
}

func normalizeStoredOpenCodeProtocolVersion(value string) string {
	version, err := NormalizeOpenCodeProtocolVersion(value)
	if err != nil {
		return DefaultOpenCodeProtocolVersion
	}
	return version
}

func (s *SettingService) GetOpenCodeProtocolSettings(ctx context.Context) OpenCodeProtocolSettings {
	fallback := OpenCodeProtocolSettings{Version: DefaultOpenCodeProtocolVersion}
	if s == nil || s.settingRepo == nil {
		return fallback
	}
	if cached, ok := s.openCodeProtocolCache.Load().(*cachedOpenCodeProtocolSettings); ok && cached != nil && time.Now().UnixNano() < cached.expiresAt {
		return cached.settings
	}

	result, _, _ := s.openCodeProtocolSF.Do("opencode_protocol", func() (any, error) {
		if cached, ok := s.openCodeProtocolCache.Load().(*cachedOpenCodeProtocolSettings); ok && cached != nil && time.Now().UnixNano() < cached.expiresAt {
			return cached.settings, nil
		}
		if ctx == nil {
			ctx = context.Background()
		}
		dbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), gatewayForwardingDBTimeout)
		defer cancel()
		values, err := s.settingRepo.GetMultiple(dbCtx, []string{SettingKeyOpenCodeProtocolEnabled, SettingKeyOpenCodeProtocolVersion})
		if err != nil && !errors.Is(err, ErrSettingNotFound) {
			s.openCodeProtocolCache.Store(&cachedOpenCodeProtocolSettings{settings: fallback, expiresAt: time.Now().Add(openCodeProtocolErrorTTL).UnixNano()})
			return fallback, nil
		}
		settings := OpenCodeProtocolSettings{
			Enabled: strings.EqualFold(strings.TrimSpace(values[SettingKeyOpenCodeProtocolEnabled]), "true"),
			Version: normalizeStoredOpenCodeProtocolVersion(values[SettingKeyOpenCodeProtocolVersion]),
		}
		s.openCodeProtocolCache.Store(&cachedOpenCodeProtocolSettings{settings: settings, expiresAt: time.Now().Add(openCodeProtocolCacheTTL).UnixNano()})
		return settings, nil
	})
	if settings, ok := result.(OpenCodeProtocolSettings); ok {
		return settings
	}
	return fallback
}
