package repository

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type openAIRetryRoundTripFunc func(*http.Request) (*http.Response, error)

func (f openAIRetryRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func newRetryTestEntry(fn openAIRetryRoundTripFunc) *upstreamClientEntry {
	return &upstreamClientEntry{
		client:       &http.Client{Transport: fn},
		protocolMode: upstreamProtocolModeOpenAIH1,
	}
}

func newOpenAIRetryRequest(t *testing.T, retries int, body string) *http.Request {
	t.Helper()
	ctx := service.WithOpenAIUpstreamRetryLimit(context.Background(), retries)
	ctx = service.WithHTTPUpstreamProfile(ctx, service.HTTPUpstreamProfileOpenAI)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://example.com/v1/responses", strings.NewReader(body))
	require.NoError(t, err)
	return req
}

func retryTestResponse(req *http.Request, status int, body string, headers http.Header) *http.Response {
	if headers == nil {
		headers = make(http.Header)
	}
	return &http.Response{
		StatusCode: status,
		Header:     headers,
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}

func TestOpenAIUpstreamRetry_ReplaysBodyAndSucceedsOnFourthAttempt(t *testing.T) {
	var attempts atomic.Int32
	var bodies []string
	entry := newRetryTestEntry(func(req *http.Request) (*http.Response, error) {
		attempt := attempts.Add(1)
		body, err := io.ReadAll(req.Body)
		require.NoError(t, err)
		bodies = append(bodies, string(body))
		if attempt < 4 {
			return retryTestResponse(req, http.StatusRequestTimeout, `{"detail":"Request body read timed out"}`, nil), nil
		}
		return retryTestResponse(req, http.StatusOK, `{"ok":true}`, nil), nil
	})

	svc := &httpUpstreamService{}
	req := newOpenAIRetryRequest(t, 3, `{"model":"gpt-test"}`)
	resp, err := svc.doRequestWithOpenAIRetry(req, entry, service.HTTPUpstreamProfileOpenAI, 7)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	require.Equal(t, int32(4), attempts.Load())
	require.Equal(t, []string{
		`{"model":"gpt-test"}`,
		`{"model":"gpt-test"}`,
		`{"model":"gpt-test"}`,
		`{"model":"gpt-test"}`,
	}, bodies)
	require.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestOpenAIUpstreamRetry_ReturnsOnlyFinalHTTPErrorAfterExhaustion(t *testing.T) {
	var attempts atomic.Int32
	entry := newRetryTestEntry(func(req *http.Request) (*http.Response, error) {
		attempt := attempts.Add(1)
		return retryTestResponse(req, http.StatusRequestTimeout, `{"attempt":`+string(rune('0'+attempt))+`}`, nil), nil
	})

	svc := &httpUpstreamService{}
	req := newOpenAIRetryRequest(t, 3, `{}`)
	resp, err := svc.doRequestWithOpenAIRetry(req, entry, service.HTTPUpstreamProfileOpenAI, 7)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	body, readErr := io.ReadAll(resp.Body)
	require.NoError(t, readErr)
	require.Equal(t, int32(4), attempts.Load())
	require.JSONEq(t, `{"attempt":4}`, string(body))
}

func TestOpenAIUpstreamRetry_Retries429WithoutCooldown(t *testing.T) {
	var attempts atomic.Int32
	entry := newRetryTestEntry(func(req *http.Request) (*http.Response, error) {
		if attempts.Add(1) == 1 {
			return retryTestResponse(req, http.StatusTooManyRequests, `{"error":{"message":"rate limited"}}`, nil), nil
		}
		return retryTestResponse(req, http.StatusOK, `{"ok":true}`, nil), nil
	})

	svc := &httpUpstreamService{}
	req := newOpenAIRetryRequest(t, 3, `{}`)
	resp, err := svc.doRequestWithOpenAIRetry(req, entry, service.HTTPUpstreamProfileOpenAI, 7)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	require.Equal(t, int32(2), attempts.Load())
	require.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestOpenAIUpstreamRetry_DoesNotRetry429WithExplicitCooldown(t *testing.T) {
	var attempts atomic.Int32
	entry := newRetryTestEntry(func(req *http.Request) (*http.Response, error) {
		attempts.Add(1)
		return retryTestResponse(
			req,
			http.StatusTooManyRequests,
			`{"error":{"message":"rate limited"}}`,
			http.Header{"Retry-After": []string{"60"}},
		), nil
	})

	svc := &httpUpstreamService{}
	req := newOpenAIRetryRequest(t, 3, `{}`)
	resp, err := svc.doRequestWithOpenAIRetry(req, entry, service.HTTPUpstreamProfileOpenAI, 7)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	body, readErr := io.ReadAll(resp.Body)
	require.NoError(t, readErr)
	require.Equal(t, int32(1), attempts.Load())
	require.Equal(t, http.StatusTooManyRequests, resp.StatusCode)
	require.JSONEq(t, `{"error":{"message":"rate limited"}}`, string(body))
}

func TestOpenAIUpstreamRetry_DoesNotRetry429WithBodyResetTimestamp(t *testing.T) {
	var attempts atomic.Int32
	resetAt := time.Now().Add(time.Hour).Unix()
	body := fmt.Sprintf(`{"error":{"type":"rate_limit_exceeded","resets_at":%d}}`, resetAt)
	entry := newRetryTestEntry(func(req *http.Request) (*http.Response, error) {
		attempts.Add(1)
		return retryTestResponse(req, http.StatusTooManyRequests, body, nil), nil
	})

	svc := &httpUpstreamService{}
	req := newOpenAIRetryRequest(t, 3, `{}`)
	resp, err := svc.doRequestWithOpenAIRetry(req, entry, service.HTTPUpstreamProfileOpenAI, 7)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	returnedBody, readErr := io.ReadAll(resp.Body)
	require.NoError(t, readErr)
	require.Equal(t, int32(1), attempts.Load())
	require.JSONEq(t, body, string(returnedBody))
}

func TestOpenAIUpstreamRetry_RetriesTransportErrors(t *testing.T) {
	var attempts atomic.Int32
	entry := newRetryTestEntry(func(req *http.Request) (*http.Response, error) {
		if attempts.Add(1) == 1 {
			return nil, errors.New("proxy connection reset")
		}
		return retryTestResponse(req, http.StatusOK, `{"ok":true}`, nil), nil
	})

	svc := &httpUpstreamService{}
	req := newOpenAIRetryRequest(t, 3, `{}`)
	resp, err := svc.doRequestWithOpenAIRetry(req, entry, service.HTTPUpstreamProfileOpenAI, 7)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	require.Equal(t, int32(2), attempts.Load())
	require.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestOpenAIUpstreamRetry_DoesNotRetryOtherProfiles(t *testing.T) {
	var attempts atomic.Int32
	entry := newRetryTestEntry(func(req *http.Request) (*http.Response, error) {
		attempts.Add(1)
		return retryTestResponse(req, http.StatusInternalServerError, `{"error":"failed"}`, nil), nil
	})

	ctx := service.WithOpenAIUpstreamRetryLimit(context.Background(), 3)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://example.com/v1/messages", strings.NewReader(`{}`))
	require.NoError(t, err)

	svc := &httpUpstreamService{}
	resp, err := svc.doRequestWithOpenAIRetry(req, entry, service.HTTPUpstreamProfileDefault, 7)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	require.Equal(t, int32(1), attempts.Load())
	require.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}
