package repository

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type privacyProxyPolicyStub struct {
	plan    service.OpenAIProxyPlan
	primary string
}

func (s *privacyProxyPolicyStub) Resolve(_ context.Context, primaryProxyURL string) service.OpenAIProxyPlan {
	s.primary = primaryProxyURL
	return s.plan
}

type privacyRoundTripFunc func(*http.Request) (*http.Response, error)

func (f privacyRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestOpenAIPrivacyEmptyPrimaryUsesNodeProxy(t *testing.T) {
	policy := &privacyProxyPolicyStub{plan: openAIProxyTestPlan(
		service.OpenAIProxyCandidate{URL: "socks5h://warp-proxy:1080", Source: service.OpenAIProxyCandidateNode},
	)}
	var attempted []string
	router := &openAIPrivacyRoundTripper{
		policy: policy,
		transportFor: func(proxyURL string) (http.RoundTripper, error) {
			attempted = append(attempted, proxyURL)
			return privacyRoundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, nil
			}), nil
		},
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://chatgpt.com/backend-api/accounts/check", nil)
	require.NoError(t, err)

	resp, err := router.RoundTrip(req)

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Empty(t, policy.primary)
	require.Equal(t, []string{"socks5h://warp-proxy:1080"}, attempted)
}

func TestOpenAIPrivacyAccountProxyPrecedesNodeProxy(t *testing.T) {
	policy := &privacyProxyPolicyStub{plan: openAIProxyTestPlan(
		service.OpenAIProxyCandidate{URL: "http://account-proxy:8080", Source: service.OpenAIProxyCandidateAccount},
		service.OpenAIProxyCandidate{URL: "socks5h://warp-proxy:1080", Source: service.OpenAIProxyCandidateNode},
	)}
	var attempted []string
	router := &openAIPrivacyRoundTripper{
		policy:          policy,
		primaryProxyURL: "http://account-proxy:8080",
		transportFor: func(proxyURL string) (http.RoundTripper, error) {
			attempted = append(attempted, proxyURL)
			return privacyRoundTripFunc(func(*http.Request) (*http.Response, error) {
				if proxyURL == "http://account-proxy:8080" {
					return nil, errors.New("proxy connection refused")
				}
				return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, nil
			}), nil
		},
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://chatgpt.com/backend-api/accounts/check", nil)
	require.NoError(t, err)

	resp, err := router.RoundTrip(req)

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "http://account-proxy:8080", policy.primary)
	require.Equal(t, []string{"http://account-proxy:8080", "socks5h://warp-proxy:1080"}, attempted)
}

func TestOpenAIPrivacyHTTPResponseStopsProxyChain(t *testing.T) {
	policy := &privacyProxyPolicyStub{plan: openAIProxyTestPlan(
		service.OpenAIProxyCandidate{URL: "http://account-proxy:8080", Source: service.OpenAIProxyCandidateAccount},
		service.OpenAIProxyCandidate{URL: "socks5h://warp-proxy:1080", Source: service.OpenAIProxyCandidateNode},
	)}
	var attempted []string
	router := &openAIPrivacyRoundTripper{
		policy: policy,
		transportFor: func(proxyURL string) (http.RoundTripper, error) {
			attempted = append(attempted, proxyURL)
			return privacyRoundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusForbidden, Body: http.NoBody}, nil
			}), nil
		},
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPatch, "https://chatgpt.com/backend-api/settings", nil)
	require.NoError(t, err)

	resp, err := router.RoundTrip(req)

	require.NoError(t, err)
	require.Equal(t, http.StatusForbidden, resp.StatusCode)
	require.Equal(t, []string{"http://account-proxy:8080"}, attempted)
}

func TestOpenAIPrivacyRejectsNonReplayableBodyBeforeSending(t *testing.T) {
	policy := &privacyProxyPolicyStub{plan: openAIProxyTestPlan(
		service.OpenAIProxyCandidate{URL: "http://account-proxy:8080", Source: service.OpenAIProxyCandidateAccount},
		service.OpenAIProxyCandidate{URL: "socks5h://warp-proxy:1080", Source: service.OpenAIProxyCandidateNode},
	)}
	attempts := 0
	router := &openAIPrivacyRoundTripper{
		policy: policy,
		transportFor: func(string) (http.RoundTripper, error) {
			attempts++
			return privacyRoundTripFunc(func(*http.Request) (*http.Response, error) {
				return nil, nil
			}), nil
		},
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://chatgpt.com/backend-api/settings", nil)
	require.NoError(t, err)
	req.Body = io.NopCloser(strings.NewReader(`{"training":false}`))
	req.GetBody = nil

	resp, err := router.RoundTrip(req)

	require.Nil(t, resp)
	require.ErrorIs(t, err, service.ErrOpenAIProxyRequestNotReplayable)
	require.Zero(t, attempts)
}

func TestOpenAIPrivacyClientUsesFirefoxImpersonation(t *testing.T) {
	var userAgent string
	var secCHUA string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userAgent = r.Header.Get("User-Agent")
		secCHUA = r.Header.Get("sec-ch-ua")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	policy := &privacyProxyPolicyStub{plan: openAIProxyTestPlan(
		service.OpenAIProxyCandidate{Source: service.OpenAIProxyCandidateDirect},
	)}
	factory := NewPrivacyClientFactory(policy)
	client, err := factory("")
	require.NoError(t, err)

	resp, err := client.R().Get(server.URL)

	require.NoError(t, err)
	require.Equal(t, http.StatusNoContent, resp.StatusCode)
	// chatgpt.com 的 Cloudflare 会质询 req 内置的 Chrome/120 伪装，隐私客户端必须保持 Firefox 指纹。
	require.Contains(t, userAgent, "Firefox/")
	require.NotContains(t, userAgent, "Chrome/")
	require.NotContains(t, secCHUA, "Google Chrome")
}
