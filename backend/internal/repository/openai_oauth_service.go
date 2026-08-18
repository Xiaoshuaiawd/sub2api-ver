package repository

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/internal/util/logredact"
)

const (
	openAIOAuthErrorBodyLimit   = 4 << 10
	openAIOAuthSuccessBodyLimit = 1 << 20
)

// NewOpenAIOAuthClient creates a new OpenAI OAuth client
func NewOpenAIOAuthClient(httpUpstream service.HTTPUpstream) service.OpenAIOAuthClient {
	return &openaiOAuthService{tokenURL: openai.TokenURL, httpUpstream: httpUpstream}
}

type openaiOAuthService struct {
	tokenURL     string
	httpUpstream service.HTTPUpstream
}

func (s *openaiOAuthService) ExchangeCode(ctx context.Context, code, codeVerifier, redirectURI, proxyURL, clientID string) (*openai.TokenResponse, error) {
	if redirectURI == "" {
		redirectURI = openai.DefaultRedirectURI
	}
	clientID = strings.TrimSpace(clientID)
	if clientID == "" {
		clientID = openai.ClientID
	}

	formData := url.Values{}
	formData.Set("grant_type", "authorization_code")
	formData.Set("client_id", clientID)
	formData.Set("code", code)
	formData.Set("redirect_uri", redirectURI)
	formData.Set("code_verifier", codeVerifier)

	return s.postTokenForm(ctx, formData, proxyURL, "OPENAI_OAUTH_TOKEN_EXCHANGE_FAILED", "token exchange")
}

func (s *openaiOAuthService) RefreshToken(ctx context.Context, refreshToken, proxyURL string) (*openai.TokenResponse, error) {
	return s.RefreshTokenWithClientID(ctx, refreshToken, proxyURL, "")
}

func (s *openaiOAuthService) RefreshTokenWithClientID(ctx context.Context, refreshToken, proxyURL string, clientID string) (*openai.TokenResponse, error) {
	// 调用方应始终传入正确的 client_id；为兼容旧数据，未指定时默认使用 OpenAI ClientID
	clientID = strings.TrimSpace(clientID)
	if clientID == "" {
		clientID = openai.ClientID
	}
	return s.refreshTokenWithClientID(ctx, refreshToken, proxyURL, clientID)
}

func (s *openaiOAuthService) refreshTokenWithClientID(ctx context.Context, refreshToken, proxyURL, clientID string) (*openai.TokenResponse, error) {
	formData := url.Values{}
	formData.Set("grant_type", "refresh_token")
	formData.Set("refresh_token", refreshToken)
	formData.Set("client_id", clientID)
	formData.Set("scope", openai.RefreshScopes)

	return s.postTokenForm(ctx, formData, proxyURL, "OPENAI_OAUTH_TOKEN_REFRESH_FAILED", "token refresh")
}

func (s *openaiOAuthService) postTokenForm(
	ctx context.Context,
	formData url.Values,
	proxyURL string,
	statusReason string,
	statusAction string,
) (*openai.TokenResponse, error) {
	if s.httpUpstream == nil {
		return nil, infraerrors.New(http.StatusInternalServerError, "OPENAI_OAUTH_CLIENT_INIT_FAILED", "OpenAI OAuth HTTP upstream is not configured")
	}

	reqCtx := service.WithHTTPUpstreamProfile(ctx, service.HTTPUpstreamProfileOpenAI)
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, s.tokenURL, strings.NewReader(formData.Encode()))
	if err != nil {
		return nil, infraerrors.Newf(http.StatusBadGateway, "OPENAI_OAUTH_REQUEST_FAILED", "build request failed: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	authUA, authOriginator := service.CodexCanonicalAuthIdentity()
	req.Header.Set("User-Agent", authUA)
	req.Header.Set("originator", authOriginator)

	resp, err := s.httpUpstream.Do(req, proxyURL, 0, 1)
	if err != nil {
		return nil, infraerrors.Newf(http.StatusBadGateway, "OPENAI_OAUTH_REQUEST_FAILED", "request failed: %v", err)
	}
	if resp == nil {
		return nil, infraerrors.New(http.StatusBadGateway, "OPENAI_OAUTH_REQUEST_FAILED", "request failed: empty upstream response")
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, openAIOAuthErrorBodyLimit))
		message := logredact.RedactText(string(body))
		if message == "" {
			message = http.StatusText(resp.StatusCode)
		}
		return nil, infraerrors.Newf(http.StatusBadGateway, statusReason, "%s failed: status %d, body: %s", statusAction, resp.StatusCode, message)
	}

	var tokenResp openai.TokenResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, openAIOAuthSuccessBodyLimit)).Decode(&tokenResp); err != nil {
		return nil, infraerrors.Newf(http.StatusBadGateway, "OPENAI_OAUTH_REQUEST_FAILED", "decode token response failed: %v", err)
	}
	return &tokenResp, nil
}
