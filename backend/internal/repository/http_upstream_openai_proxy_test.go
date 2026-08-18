package repository

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type stubOpenAIProxyPolicy struct {
	plan service.OpenAIProxyPlan
}

func (s stubOpenAIProxyPolicy) Resolve(context.Context, string) service.OpenAIProxyPlan {
	return s.plan
}

type openAIProxyAttemptResult struct {
	response *http.Response
	err      error
}

func openAIProxyTestPlan(candidates ...service.OpenAIProxyCandidate) service.OpenAIProxyPlan {
	return service.OpenAIProxyPlan{Candidates: candidates}
}

func openAIProxyTestRequest(t *testing.T, body io.Reader) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, "https://chatgpt.com/backend-api/codex/responses", body)
	require.NoError(t, err)
	return req.WithContext(service.WithHTTPUpstreamProfile(req.Context(), service.HTTPUpstreamProfileOpenAI))
}

func TestHTTPUpstreamOpenAIAdvancesOnlyOnTransportError(t *testing.T) {
	results := []openAIProxyAttemptResult{
		{err: io.ErrUnexpectedEOF},
		{response: &http.Response{StatusCode: http.StatusTooManyRequests, Body: http.NoBody}},
	}
	var attempted []string
	upstream := &httpUpstreamService{
		openAIProxyPolicy: stubOpenAIProxyPolicy{plan: openAIProxyTestPlan(
			service.OpenAIProxyCandidate{URL: "http://a:1", Source: service.OpenAIProxyCandidateAccount},
			service.OpenAIProxyCandidate{URL: "http://b:2", Source: service.OpenAIProxyCandidateBackup},
			service.OpenAIProxyCandidate{URL: "", Source: service.OpenAIProxyCandidateDirect},
		)},
		doAttempt: func(_ *http.Request, proxyURL string, _ int64, _ int) (*http.Response, error) {
			attempted = append(attempted, proxyURL)
			result := results[len(attempted)-1]
			return result.response, result.err
		},
	}
	req := openAIProxyTestRequest(t, bytes.NewReader([]byte(`{"model":"gpt-5"}`)))

	resp, err := upstream.Do(req, "http://account:8080", 7, 1)

	require.NoError(t, err)
	require.Equal(t, http.StatusTooManyRequests, resp.StatusCode)
	require.Equal(t, []string{"http://a:1", "http://b:2"}, attempted)
}

func TestHTTPUpstreamOpenAINonProfileUsesSingleExistingAttempt(t *testing.T) {
	var attempted []string
	upstream := &httpUpstreamService{
		openAIProxyPolicy: stubOpenAIProxyPolicy{plan: openAIProxyTestPlan(
			service.OpenAIProxyCandidate{URL: "http://warp:1080", Source: service.OpenAIProxyCandidateNode},
		)},
		doAttempt: func(_ *http.Request, proxyURL string, _ int64, _ int) (*http.Response, error) {
			attempted = append(attempted, proxyURL)
			return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, nil
		},
	}
	req, err := http.NewRequest(http.MethodGet, "https://api.anthropic.com/v1/messages", nil)
	require.NoError(t, err)

	resp, err := upstream.Do(req, "http://account:8080", 7, 1)

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, []string{"http://account:8080"}, attempted)
}

func TestHTTPUpstreamOpenAIRejectsNonReplayableBodyBeforeAttempt(t *testing.T) {
	attempts := 0
	upstream := &httpUpstreamService{
		openAIProxyPolicy: stubOpenAIProxyPolicy{plan: openAIProxyTestPlan(
			service.OpenAIProxyCandidate{URL: "http://a:1", Source: service.OpenAIProxyCandidateAccount},
			service.OpenAIProxyCandidate{URL: "http://b:2", Source: service.OpenAIProxyCandidateNode},
		)},
		doAttempt: func(*http.Request, string, int64, int) (*http.Response, error) {
			attempts++
			return nil, nil
		},
	}
	req := openAIProxyTestRequest(t, io.NopCloser(strings.NewReader(`{"model":"gpt-5"}`)))
	require.Nil(t, req.GetBody)

	resp, err := upstream.Do(req, "http://account:8080", 7, 1)

	require.Nil(t, resp)
	require.ErrorIs(t, err, service.ErrOpenAIProxyRequestNotReplayable)
	require.Zero(t, attempts)
}

func TestHTTPUpstreamOpenAIStopsAfterContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var attempted []string
	upstream := &httpUpstreamService{
		openAIProxyPolicy: stubOpenAIProxyPolicy{plan: openAIProxyTestPlan(
			service.OpenAIProxyCandidate{URL: "http://a:1", Source: service.OpenAIProxyCandidateAccount},
			service.OpenAIProxyCandidate{URL: "http://b:2", Source: service.OpenAIProxyCandidateNode},
		)},
		doAttempt: func(_ *http.Request, proxyURL string, _ int64, _ int) (*http.Response, error) {
			attempted = append(attempted, proxyURL)
			cancel()
			return nil, context.Canceled
		},
	}
	req := openAIProxyTestRequest(t, nil)
	req = req.WithContext(service.WithHTTPUpstreamProfile(ctx, service.HTTPUpstreamProfileOpenAI))

	resp, err := upstream.Do(req, "http://account:8080", 7, 1)

	require.Nil(t, resp)
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, []string{"http://a:1"}, attempted)
}

func TestHTTPUpstreamOpenAIFailClosedExhaustionDoesNotAttemptDirect(t *testing.T) {
	var attempted []string
	upstream := &httpUpstreamService{
		openAIProxyPolicy: stubOpenAIProxyPolicy{plan: openAIProxyTestPlan(
			service.OpenAIProxyCandidate{URL: "http://account:8080", Source: service.OpenAIProxyCandidateAccount},
			service.OpenAIProxyCandidate{URL: "socks5h://warp:1080", Source: service.OpenAIProxyCandidateNode},
		)},
		doAttempt: func(_ *http.Request, proxyURL string, _ int64, _ int) (*http.Response, error) {
			attempted = append(attempted, proxyURL)
			return nil, errors.New("connection refused")
		},
	}

	resp, err := upstream.Do(openAIProxyTestRequest(t, nil), "http://account:8080", 7, 1)

	require.Nil(t, resp)
	require.Error(t, err)
	require.Equal(t, []string{"http://account:8080", "socks5h://warp:1080"}, attempted)
}

func TestHTTPUpstreamOpenAIFallbackDirectAttemptsDirectLast(t *testing.T) {
	var attempted []string
	upstream := &httpUpstreamService{
		openAIProxyPolicy: stubOpenAIProxyPolicy{plan: openAIProxyTestPlan(
			service.OpenAIProxyCandidate{URL: "http://account:8080", Source: service.OpenAIProxyCandidateAccount},
			service.OpenAIProxyCandidate{URL: "socks5h://warp:1080", Source: service.OpenAIProxyCandidateNode},
			service.OpenAIProxyCandidate{Source: service.OpenAIProxyCandidateDirect},
		)},
		doAttempt: func(_ *http.Request, proxyURL string, _ int64, _ int) (*http.Response, error) {
			attempted = append(attempted, proxyURL)
			if proxyURL == "" {
				return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, nil
			}
			return nil, errors.New("connection refused")
		},
	}

	resp, err := upstream.Do(openAIProxyTestRequest(t, nil), "http://account:8080", 7, 1)

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, []string{"http://account:8080", "socks5h://warp:1080", ""}, attempted)
}

func TestHTTPUpstreamOpenAIDoWithTLSUsesProxyCandidates(t *testing.T) {
	var attempted []string
	upstream := &httpUpstreamService{
		openAIProxyPolicy: stubOpenAIProxyPolicy{plan: openAIProxyTestPlan(
			service.OpenAIProxyCandidate{URL: "http://account:8080", Source: service.OpenAIProxyCandidateAccount},
			service.OpenAIProxyCandidate{URL: "socks5h://warp:1080", Source: service.OpenAIProxyCandidateNode},
		)},
		doTLSAttempt: func(_ *http.Request, proxyURL string, _ int64, _ int, _ *tlsfingerprint.Profile) (*http.Response, error) {
			attempted = append(attempted, proxyURL)
			if len(attempted) == 1 {
				return nil, errors.New("proxy handshake failed")
			}
			return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, nil
		},
	}

	resp, err := upstream.DoWithTLS(openAIProxyTestRequest(t, nil), "http://account:8080", 7, 1, &tlsfingerprint.Profile{Name: "chrome"})

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, []string{"http://account:8080", "socks5h://warp:1080"}, attempted)
}

func TestHTTPUpstreamOpenAIReplaysBodyForEveryCandidate(t *testing.T) {
	payload := []byte(`{"model":"gpt-5","input":"hello"}`)
	var bodies [][]byte
	upstream := &httpUpstreamService{
		openAIProxyPolicy: stubOpenAIProxyPolicy{plan: openAIProxyTestPlan(
			service.OpenAIProxyCandidate{URL: "http://account:8080", Source: service.OpenAIProxyCandidateAccount},
			service.OpenAIProxyCandidate{URL: "socks5h://warp:1080", Source: service.OpenAIProxyCandidateNode},
		)},
		doAttempt: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
			body, err := io.ReadAll(req.Body)
			require.NoError(t, err)
			bodies = append(bodies, body)
			if len(bodies) == 1 {
				return nil, errors.New("connection reset")
			}
			return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, nil
		},
	}

	resp, err := upstream.Do(openAIProxyTestRequest(t, bytes.NewReader(payload)), "http://account:8080", 7, 1)

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, [][]byte{payload, payload}, bodies)
}

func TestHTTPUpstreamOpenAIRejectsReplayConstructionFailureBeforeAttempt(t *testing.T) {
	attempts := 0
	upstream := &httpUpstreamService{
		openAIProxyPolicy: stubOpenAIProxyPolicy{plan: openAIProxyTestPlan(
			service.OpenAIProxyCandidate{URL: "http://account:8080", Source: service.OpenAIProxyCandidateAccount},
			service.OpenAIProxyCandidate{URL: "socks5h://warp:1080", Source: service.OpenAIProxyCandidateNode},
		)},
		doAttempt: func(*http.Request, string, int64, int) (*http.Response, error) {
			attempts++
			return nil, nil
		},
	}
	req := openAIProxyTestRequest(t, strings.NewReader(`{"model":"gpt-5"}`))
	req.GetBody = func() (io.ReadCloser, error) { return nil, errors.New("body unavailable") }

	resp, err := upstream.Do(req, "http://account:8080", 7, 1)

	require.Nil(t, resp)
	require.ErrorContains(t, err, "replay OpenAI upstream request body")
	require.Zero(t, attempts)
}

func TestHTTPUpstreamOpenAIRejectsEmptyCandidatePlan(t *testing.T) {
	upstream := &httpUpstreamService{
		openAIProxyPolicy: stubOpenAIProxyPolicy{},
		doAttempt: func(*http.Request, string, int64, int) (*http.Response, error) {
			t.Fatal("empty candidate plan must not attempt a request")
			return nil, nil
		},
	}

	resp, err := upstream.Do(openAIProxyTestRequest(t, nil), "", 7, 1)

	require.Nil(t, resp)
	require.ErrorContains(t, err, "no candidates")
}
