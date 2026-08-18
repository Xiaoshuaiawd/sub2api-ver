//go:build unit

package service

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type openAIWSProxyPolicyStub struct {
	plan OpenAIProxyPlan
}

func (s openAIWSProxyPolicyStub) Resolve(context.Context, string) OpenAIProxyPlan {
	return s.plan
}

type recordingOpenAIWSProxyPolicy struct {
	plan              OpenAIProxyPlan
	attempts          []OpenAIProxyCandidateSource
	transportFailures []OpenAIProxyTransport
	switches          int
	directFallbacks   int
}

func (s *recordingOpenAIWSProxyPolicy) Resolve(context.Context, string) OpenAIProxyPlan {
	return s.plan
}

func (s *recordingOpenAIWSProxyPolicy) RecordAttempt(source OpenAIProxyCandidateSource) {
	s.attempts = append(s.attempts, source)
}

func (s *recordingOpenAIWSProxyPolicy) RecordCandidateSwitch()      { s.switches++ }
func (s *recordingOpenAIWSProxyPolicy) RecordFailClosedExhaustion() {}
func (s *recordingOpenAIWSProxyPolicy) RecordDirectFallback()       { s.directFallbacks++ }
func (s *recordingOpenAIWSProxyPolicy) RecordTransportFailure(transport OpenAIProxyTransport) {
	s.transportFailures = append(s.transportFailures, transport)
}

type openAIWSProxyTestConn struct{}

func (*openAIWSProxyTestConn) WriteJSON(context.Context, any) error { return nil }
func (*openAIWSProxyTestConn) ReadMessage(context.Context) ([]byte, error) {
	return nil, nil
}
func (*openAIWSProxyTestConn) Ping(context.Context) error { return nil }
func (*openAIWSProxyTestConn) Close() error               { return nil }

func openAIWSProxyPlan(candidates ...OpenAIProxyCandidate) OpenAIProxyPlan {
	return OpenAIProxyPlan{Candidates: candidates}
}

func TestOpenAIWSDefaultDialerAdvancesOnTransportFailure(t *testing.T) {
	dialer := newDefaultOpenAIWSClientDialer(openAIWSProxyPolicyStub{plan: openAIWSProxyPlan(
		OpenAIProxyCandidate{URL: "http://a:1", Source: OpenAIProxyCandidateAccount},
		OpenAIProxyCandidate{URL: "http://b:2", Source: OpenAIProxyCandidateNode},
	)}).(*coderOpenAIWSClientDialer)
	var attempted []string
	dialer.dialOnce = func(_ context.Context, _ string, _ http.Header, proxyURL string) (openAIWSClientConn, int, http.Header, error) {
		attempted = append(attempted, proxyURL)
		if len(attempted) == 1 {
			return nil, 0, nil, errors.New("connect refused")
		}
		return &openAIWSProxyTestConn{}, 0, http.Header{"X-Test": []string{"ok"}}, nil
	}

	conn, status, headers, err := dialer.Dial(context.Background(), "wss://chatgpt.com/backend-api/codex/responses", nil, "http://account:8080")

	require.NoError(t, err)
	require.NotNil(t, conn)
	require.Zero(t, status)
	require.Equal(t, "ok", headers.Get("X-Test"))
	require.Equal(t, []string{"http://a:1", "http://b:2"}, attempted)
}

func TestOpenAIWSDefaultDialerRecordsCandidateFallback(t *testing.T) {
	policy := &recordingOpenAIWSProxyPolicy{plan: openAIWSProxyPlan(
		OpenAIProxyCandidate{URL: "http://a:1", Source: OpenAIProxyCandidateAccount},
		OpenAIProxyCandidate{URL: "socks5h://warp:1080", Source: OpenAIProxyCandidateNode},
	)}
	dialer := newDefaultOpenAIWSClientDialer(policy).(*coderOpenAIWSClientDialer)
	dialer.dialOnce = func(_ context.Context, _ string, _ http.Header, proxyURL string) (openAIWSClientConn, int, http.Header, error) {
		if proxyURL == "http://a:1" {
			return nil, 0, nil, errors.New("connect refused")
		}
		return &openAIWSProxyTestConn{}, 0, nil, nil
	}

	conn, _, _, err := dialer.Dial(context.Background(), "wss://chatgpt.com/backend-api/codex/responses", nil, "")

	require.NoError(t, err)
	require.NotNil(t, conn)
	require.Equal(t, []OpenAIProxyCandidateSource{
		OpenAIProxyCandidateAccount,
		OpenAIProxyCandidateNode,
	}, policy.attempts)
	require.Equal(t, []OpenAIProxyTransport{OpenAIProxyTransportWebSocket}, policy.transportFailures)
	require.Equal(t, 1, policy.switches)
}

func TestOpenAIWSDefaultDialerDoesNotCountNormalDirectAsFallback(t *testing.T) {
	policy := &recordingOpenAIWSProxyPolicy{plan: openAIWSProxyPlan(
		OpenAIProxyCandidate{Source: OpenAIProxyCandidateDirect},
	)}
	dialer := newDefaultOpenAIWSClientDialer(policy).(*coderOpenAIWSClientDialer)
	dialer.dialOnce = func(_ context.Context, _ string, _ http.Header, _ string) (openAIWSClientConn, int, http.Header, error) {
		return &openAIWSProxyTestConn{}, 0, nil, nil
	}

	conn, _, _, err := dialer.Dial(context.Background(), "wss://chatgpt.com/backend-api/codex/responses", nil, "")

	require.NoError(t, err)
	require.NotNil(t, conn)
	require.Equal(t, []OpenAIProxyCandidateSource{OpenAIProxyCandidateDirect}, policy.attempts)
	require.Zero(t, policy.directFallbacks)
}

func TestOpenAIWSDefaultDialerStopsOnHandshakeHTTPResponse(t *testing.T) {
	dialer := newDefaultOpenAIWSClientDialer(openAIWSProxyPolicyStub{plan: openAIWSProxyPlan(
		OpenAIProxyCandidate{URL: "http://a:1", Source: OpenAIProxyCandidateAccount},
		OpenAIProxyCandidate{URL: "http://b:2", Source: OpenAIProxyCandidateNode},
	)}).(*coderOpenAIWSClientDialer)
	var attempted []string
	dialer.dialOnce = func(_ context.Context, _ string, _ http.Header, proxyURL string) (openAIWSClientConn, int, http.Header, error) {
		attempted = append(attempted, proxyURL)
		return nil, http.StatusTooManyRequests, http.Header{"Retry-After": []string{"30"}}, errors.New("handshake rejected")
	}

	conn, status, headers, err := dialer.Dial(context.Background(), "wss://chatgpt.com/backend-api/codex/responses", nil, "http://account:8080")

	require.Error(t, err)
	require.Nil(t, conn)
	require.Equal(t, http.StatusTooManyRequests, status)
	require.Equal(t, "30", headers.Get("Retry-After"))
	require.Equal(t, []string{"http://a:1"}, attempted)
}

func TestOpenAIWSDefaultDialerFailClosedExhaustionRedactsProxyError(t *testing.T) {
	dialer := newDefaultOpenAIWSClientDialer(openAIWSProxyPolicyStub{plan: openAIWSProxyPlan(
		OpenAIProxyCandidate{URL: "http://user:secret@account:8080", Source: OpenAIProxyCandidateAccount},
		OpenAIProxyCandidate{URL: "socks5h://warp:1080", Source: OpenAIProxyCandidateNode},
	)}).(*coderOpenAIWSClientDialer)
	var attempted []string
	dialer.dialOnce = func(_ context.Context, _ string, _ http.Header, proxyURL string) (openAIWSClientConn, int, http.Header, error) {
		attempted = append(attempted, proxyURL)
		return nil, 0, nil, errors.New("dial http://user:secret@account:8080 failed")
	}

	conn, status, _, err := dialer.Dial(context.Background(), "wss://chatgpt.com/backend-api/codex/responses", nil, "http://account:8080")

	require.Error(t, err)
	require.Nil(t, conn)
	require.Zero(t, status)
	require.NotContains(t, err.Error(), "secret")
	require.Equal(t, []string{"http://user:secret@account:8080", "socks5h://warp:1080"}, attempted)
}

func TestOpenAIWSDefaultDialerFallbackDirectAttemptsDirectLast(t *testing.T) {
	dialer := newDefaultOpenAIWSClientDialer(openAIWSProxyPolicyStub{plan: openAIWSProxyPlan(
		OpenAIProxyCandidate{URL: "socks5h://warp:1080", Source: OpenAIProxyCandidateNode},
		OpenAIProxyCandidate{Source: OpenAIProxyCandidateDirect},
	)}).(*coderOpenAIWSClientDialer)
	var attempted []string
	dialer.dialOnce = func(_ context.Context, _ string, _ http.Header, proxyURL string) (openAIWSClientConn, int, http.Header, error) {
		attempted = append(attempted, proxyURL)
		if proxyURL == "" {
			return &openAIWSProxyTestConn{}, 0, nil, nil
		}
		return nil, 0, nil, errors.New("connect refused")
	}

	conn, _, _, err := dialer.Dial(context.Background(), "wss://chatgpt.com/backend-api/codex/responses", nil, "")

	require.NoError(t, err)
	require.NotNil(t, conn)
	require.Equal(t, []string{"socks5h://warp:1080", ""}, attempted)
}

func TestOpenAIWSDefaultDialerStopsAfterContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	dialer := newDefaultOpenAIWSClientDialer(openAIWSProxyPolicyStub{plan: openAIWSProxyPlan(
		OpenAIProxyCandidate{URL: "http://a:1", Source: OpenAIProxyCandidateAccount},
		OpenAIProxyCandidate{URL: "http://b:2", Source: OpenAIProxyCandidateNode},
	)}).(*coderOpenAIWSClientDialer)
	var attempted []string
	dialer.dialOnce = func(_ context.Context, _ string, _ http.Header, proxyURL string) (openAIWSClientConn, int, http.Header, error) {
		attempted = append(attempted, proxyURL)
		cancel()
		return nil, 0, nil, context.Canceled
	}

	_, _, _, err := dialer.Dial(ctx, "wss://chatgpt.com/backend-api/codex/responses", nil, "")

	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, []string{"http://a:1"}, attempted)
}

func TestOpenAIGatewayDefaultWSDialersShareProxyPolicy(t *testing.T) {
	policy := openAIWSProxyPolicyStub{plan: openAIWSProxyPlan(OpenAIProxyCandidate{URL: DefaultOpenAIDefaultProxyURL, Source: OpenAIProxyCandidateNode})}
	gateway := &OpenAIGatewayService{cfg: &config.Config{}, openAIProxyPolicy: policy}

	poolDialer, ok := gateway.getOpenAIWSConnPool().clientDialer.(*coderOpenAIWSClientDialer)
	require.True(t, ok)
	passthroughDialer, ok := gateway.getOpenAIWSPassthroughDialer().(*coderOpenAIWSClientDialer)
	require.True(t, ok)

	require.Equal(t, []string{DefaultOpenAIDefaultProxyURL}, poolDialer.policy.Resolve(context.Background(), "").URLs())
	require.Equal(t, []string{DefaultOpenAIDefaultProxyURL}, passthroughDialer.policy.Resolve(context.Background(), "").URLs())
}

func TestOpenAIWSProxyExhaustionErrorDoesNotExposeWrappedCredential(t *testing.T) {
	err := newOpenAIWSProxyExhaustedError(OpenAIProxyCandidateNode, errors.New("proxy password secret-value"))

	require.False(t, strings.Contains(err.Error(), "secret-value"))
	require.ErrorContains(t, errors.Unwrap(err), "secret-value")
}
