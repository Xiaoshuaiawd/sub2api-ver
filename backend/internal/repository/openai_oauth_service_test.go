package repository

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

type openAIOAuthHTTPUpstreamStub struct {
	do    func(req *http.Request, proxyURL string, accountID int64, accountConcurrency int) (*http.Response, error)
	calls int
}

func (s *openAIOAuthHTTPUpstreamStub) Do(req *http.Request, proxyURL string, accountID int64, accountConcurrency int) (*http.Response, error) {
	s.calls++
	return s.do(req, proxyURL, accountID, accountConcurrency)
}

func (s *openAIOAuthHTTPUpstreamStub) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return s.Do(req, proxyURL, accountID, accountConcurrency)
}

type OpenAIOAuthServiceSuite struct {
	suite.Suite
	ctx      context.Context
	srv      *httptest.Server
	svc      *openaiOAuthService
	received chan url.Values
}

func (s *OpenAIOAuthServiceSuite) SetupTest() {
	s.ctx = context.Background()
	s.received = make(chan url.Values, 1)
}

func (s *OpenAIOAuthServiceSuite) TearDownTest() {
	if s.srv != nil {
		s.srv.Close()
		s.srv = nil
	}
}

func (s *OpenAIOAuthServiceSuite) setupServer(handler http.HandlerFunc) {
	s.srv = newLocalTestServer(s.T(), handler)
	s.svc = &openaiOAuthService{
		tokenURL: s.srv.URL,
		httpUpstream: &openAIOAuthHTTPUpstreamStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
			return s.srv.Client().Do(req)
		}},
	}
}

func TestOpenAIOAuthExchangeCodeUsesOpenAIProxyProfileWithEmptyPrimary(t *testing.T) {
	var gotProxyURL string
	var gotForm url.Values
	upstream := &openAIOAuthHTTPUpstreamStub{do: func(req *http.Request, proxyURL string, accountID int64, accountConcurrency int) (*http.Response, error) {
		gotProxyURL = proxyURL
		require.Equal(t, service.HTTPUpstreamProfileOpenAI, service.HTTPUpstreamProfileFromContext(req.Context()))
		require.Equal(t, int64(0), accountID)
		require.Equal(t, 1, accountConcurrency)
		require.Equal(t, http.MethodPost, req.Method)
		require.Equal(t, "application/x-www-form-urlencoded", req.Header.Get("Content-Type"))
		wantUA, wantOriginator := service.CodexCanonicalAuthIdentity()
		require.Equal(t, wantUA, req.Header.Get("User-Agent"))
		require.Equal(t, wantOriginator, req.Header.Get("originator"))
		require.NoError(t, req.ParseForm())
		gotForm = req.PostForm
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"access_token":"at","refresh_token":"rt","expires_in":3600}`)),
		}, nil
	}}
	svc := &openaiOAuthService{tokenURL: "https://auth.openai.test/oauth/token", httpUpstream: upstream}

	result, err := svc.ExchangeCode(context.Background(), "code", "verifier", "", "", "")

	require.NoError(t, err)
	require.Equal(t, "at", result.AccessToken)
	require.Empty(t, gotProxyURL, "empty account proxy must remain empty so the shared policy selects WARP")
	require.Equal(t, "authorization_code", gotForm.Get("grant_type"))
	require.Equal(t, openai.ClientID, gotForm.Get("client_id"))
	require.Equal(t, "code", gotForm.Get("code"))
	require.Equal(t, openai.DefaultRedirectURI, gotForm.Get("redirect_uri"))
	require.Equal(t, "verifier", gotForm.Get("code_verifier"))
}

func TestOpenAIOAuthRefreshUsesOpenAIProxyProfileAndPrimary(t *testing.T) {
	const primaryProxy = "http://account-proxy.test:8080"
	var gotForm url.Values
	upstream := &openAIOAuthHTTPUpstreamStub{do: func(req *http.Request, proxyURL string, _ int64, _ int) (*http.Response, error) {
		require.Equal(t, primaryProxy, proxyURL)
		require.Equal(t, service.HTTPUpstreamProfileOpenAI, service.HTTPUpstreamProfileFromContext(req.Context()))
		require.NoError(t, req.ParseForm())
		gotForm = req.PostForm
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"access_token":"at2","refresh_token":"rt2","expires_in":3600}`)),
		}, nil
	}}
	svc := &openaiOAuthService{tokenURL: "https://auth.openai.test/oauth/token", httpUpstream: upstream}

	result, err := svc.RefreshTokenWithClientID(context.Background(), "refresh-token", primaryProxy, "custom-client")

	require.NoError(t, err)
	require.Equal(t, "at2", result.AccessToken)
	require.Equal(t, "refresh_token", gotForm.Get("grant_type"))
	require.Equal(t, "refresh-token", gotForm.Get("refresh_token"))
	require.Equal(t, "custom-client", gotForm.Get("client_id"))
	require.Equal(t, openai.RefreshScopes, gotForm.Get("scope"))
}

func TestOpenAIOAuthHTTPResponseStopsAtSharedUpstream(t *testing.T) {
	upstream := &openAIOAuthHTTPUpstreamStub{do: func(*http.Request, string, int64, int) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusBadRequest,
			Body:       io.NopCloser(strings.NewReader("invalid grant")),
		}, nil
	}}
	svc := &openaiOAuthService{tokenURL: "https://auth.openai.test/oauth/token", httpUpstream: upstream}

	_, err := svc.ExchangeCode(context.Background(), "code", "verifier", "", "", "")

	require.ErrorContains(t, err, "status 400")
	require.Equal(t, 1, upstream.calls, "OAuth must not retry an HTTP response outside the shared transport policy")
}

func TestOpenAIOAuthErrorBodyIsBounded(t *testing.T) {
	upstream := &openAIOAuthHTTPUpstreamStub{do: func(*http.Request, string, int64, int) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusBadRequest,
			Body:       io.NopCloser(strings.NewReader(strings.Repeat("a", 16*1024) + "END_MARKER")),
		}, nil
	}}
	svc := &openaiOAuthService{tokenURL: "https://auth.openai.test/oauth/token", httpUpstream: upstream}

	_, err := svc.ExchangeCode(context.Background(), "code", "verifier", "", "", "")

	require.Error(t, err)
	require.NotContains(t, err.Error(), "END_MARKER")
	require.Less(t, len(err.Error()), 8*1024)
}

func TestOpenAIOAuthErrorBodyRedactsTokens(t *testing.T) {
	upstream := &openAIOAuthHTTPUpstreamStub{do: func(*http.Request, string, int64, int) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusBadRequest,
			Body: io.NopCloser(strings.NewReader(
				`{"error":"invalid_grant","access_token":"access-secret","refresh_token":"refresh-secret","code_verifier":"verifier-secret"}`,
			)),
		}, nil
	}}
	svc := &openaiOAuthService{tokenURL: "https://auth.openai.test/oauth/token", httpUpstream: upstream}

	_, err := svc.RefreshToken(context.Background(), "refresh-secret", "")

	require.Error(t, err)
	require.NotContains(t, err.Error(), "access-secret")
	require.NotContains(t, err.Error(), "refresh-secret")
	require.NotContains(t, err.Error(), "verifier-secret")
	require.Contains(t, err.Error(), `\"refresh_token\":\"***\"`)
}

func (s *OpenAIOAuthServiceSuite) TestExchangeCode_DefaultRedirectURI() {
	errCh := make(chan string, 1)
	s.setupServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			errCh <- "method mismatch"
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if err := r.ParseForm(); err != nil {
			errCh <- "ParseForm failed"
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if got := r.PostForm.Get("grant_type"); got != "authorization_code" {
			errCh <- "grant_type mismatch"
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if got := r.PostForm.Get("client_id"); got != openai.ClientID {
			errCh <- "client_id mismatch"
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if got := r.PostForm.Get("code"); got != "code" {
			errCh <- "code mismatch"
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if got := r.PostForm.Get("redirect_uri"); got != openai.DefaultRedirectURI {
			errCh <- "redirect_uri mismatch"
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if got := r.PostForm.Get("code_verifier"); got != "ver" {
			errCh <- "code_verifier mismatch"
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		wantUA, wantOriginator := service.CodexCanonicalAuthIdentity()
		if got := r.Header.Get("User-Agent"); got != wantUA {
			errCh <- "user-agent mismatch"
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if got := r.Header.Get("originator"); got != wantOriginator {
			errCh <- "originator mismatch"
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"at","refresh_token":"rt","token_type":"bearer","expires_in":3600}`)
	}))

	resp, err := s.svc.ExchangeCode(s.ctx, "code", "ver", "", "", "")
	require.NoError(s.T(), err, "ExchangeCode")
	select {
	case msg := <-errCh:
		require.Fail(s.T(), msg)
	default:
	}
	require.Equal(s.T(), "at", resp.AccessToken)
	require.Equal(s.T(), "rt", resp.RefreshToken)
}

func (s *OpenAIOAuthServiceSuite) TestRefreshToken_FormFields() {
	errCh := make(chan string, 1)
	s.setupServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			errCh <- "ParseForm failed"
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if got := r.PostForm.Get("grant_type"); got != "refresh_token" {
			errCh <- "grant_type mismatch"
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if got := r.PostForm.Get("refresh_token"); got != "rt" {
			errCh <- "refresh_token mismatch"
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if got := r.PostForm.Get("client_id"); got != openai.ClientID {
			errCh <- "client_id mismatch"
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if got := r.PostForm.Get("scope"); got != openai.RefreshScopes {
			errCh <- "scope mismatch"
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		wantUA, wantOriginator := service.CodexCanonicalAuthIdentity()
		if got := r.Header.Get("User-Agent"); got != wantUA {
			errCh <- "user-agent mismatch"
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if got := r.Header.Get("originator"); got != wantOriginator {
			errCh <- "originator mismatch"
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"at2","refresh_token":"rt2","token_type":"bearer","expires_in":3600}`)
	}))

	resp, err := s.svc.RefreshToken(s.ctx, "rt", "")
	require.NoError(s.T(), err, "RefreshToken")
	select {
	case msg := <-errCh:
		require.Fail(s.T(), msg)
	default:
	}
	require.Equal(s.T(), "at2", resp.AccessToken)
	require.Equal(s.T(), "rt2", resp.RefreshToken)
}

// TestRefreshToken_DefaultsToOpenAIClientID 验证未指定 client_id 时默认使用 OpenAI ClientID，
// 且只发送一次请求（不再盲猜多个 client_id）。
func (s *OpenAIOAuthServiceSuite) TestRefreshToken_DefaultsToOpenAIClientID() {
	var seenClientIDs []string
	s.setupServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		clientID := r.PostForm.Get("client_id")
		seenClientIDs = append(seenClientIDs, clientID)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"at","refresh_token":"rt","token_type":"bearer","expires_in":3600}`)
	}))

	resp, err := s.svc.RefreshToken(s.ctx, "rt", "")
	require.NoError(s.T(), err, "RefreshToken")
	require.Equal(s.T(), "at", resp.AccessToken)
	// 只发送了一次请求，使用默认的 OpenAI ClientID
	require.Equal(s.T(), []string{openai.ClientID}, seenClientIDs)
}

func (s *OpenAIOAuthServiceSuite) TestRefreshToken_UseProvidedClientID() {
	const customClientID = "custom-client-id"
	var seenClientIDs []string
	s.setupServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		clientID := r.PostForm.Get("client_id")
		seenClientIDs = append(seenClientIDs, clientID)
		if clientID != customClientID {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"at-custom","refresh_token":"rt-custom","token_type":"bearer","expires_in":3600}`)
	}))

	resp, err := s.svc.RefreshTokenWithClientID(s.ctx, "rt", "", customClientID)
	require.NoError(s.T(), err, "RefreshTokenWithClientID")
	require.Equal(s.T(), "at-custom", resp.AccessToken)
	require.Equal(s.T(), "rt-custom", resp.RefreshToken)
	require.Equal(s.T(), []string{customClientID}, seenClientIDs)
}

func (s *OpenAIOAuthServiceSuite) TestNonSuccessStatus_IncludesBody() {
	s.setupServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, "bad")
	}))

	_, err := s.svc.ExchangeCode(s.ctx, "code", "ver", openai.DefaultRedirectURI, "", "")
	require.Error(s.T(), err)
	require.ErrorContains(s.T(), err, "status 400")
	require.ErrorContains(s.T(), err, "bad")
}

func (s *OpenAIOAuthServiceSuite) TestRequestError_ClosedServer() {
	s.setupServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	s.srv.Close()

	_, err := s.svc.ExchangeCode(s.ctx, "code", "ver", openai.DefaultRedirectURI, "", "")
	require.Error(s.T(), err)
	require.ErrorContains(s.T(), err, "request failed")
}

func (s *OpenAIOAuthServiceSuite) TestExchangeCode_RequestErrorWithoutPrimaryUsesSharedPolicyError() {
	s.setupServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	s.srv.Close()

	_, err := s.svc.ExchangeCode(s.ctx, "code", "ver", openai.DefaultRedirectURI, "", "")

	require.Error(s.T(), err)
	require.ErrorContains(s.T(), err, "request failed")
	require.NotContains(s.T(), err.Error(), "no proxy is configured")
}

func (s *OpenAIOAuthServiceSuite) TestContextCancel() {
	started := make(chan struct{})
	block := make(chan struct{})
	s.setupServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-block
	}))

	ctx, cancel := context.WithCancel(s.ctx)

	done := make(chan error, 1)
	go func() {
		_, err := s.svc.ExchangeCode(ctx, "code", "ver", openai.DefaultRedirectURI, "", "")
		done <- err
	}()

	<-started
	cancel()
	close(block)

	err := <-done
	require.Error(s.T(), err)
}

func (s *OpenAIOAuthServiceSuite) TestExchangeCode_UsesProvidedRedirectURI() {
	want := "http://localhost:9999/cb"
	errCh := make(chan string, 1)
	s.setupServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if got := r.PostForm.Get("redirect_uri"); got != want {
			errCh <- "redirect_uri mismatch"
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"at","token_type":"bearer","expires_in":1}`)
	}))

	_, err := s.svc.ExchangeCode(s.ctx, "code", "ver", want, "", "")
	require.NoError(s.T(), err, "ExchangeCode")
	select {
	case msg := <-errCh:
		require.Fail(s.T(), msg)
	default:
	}
}

func (s *OpenAIOAuthServiceSuite) TestExchangeCode_UseProvidedClientID() {
	wantClientID := "custom-exchange-client-id"
	errCh := make(chan string, 1)
	s.setupServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if got := r.PostForm.Get("client_id"); got != wantClientID {
			errCh <- "client_id mismatch"
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"at","token_type":"bearer","expires_in":1}`)
	}))

	_, err := s.svc.ExchangeCode(s.ctx, "code", "ver", openai.DefaultRedirectURI, "", wantClientID)
	require.NoError(s.T(), err, "ExchangeCode")
	select {
	case msg := <-errCh:
		require.Fail(s.T(), msg)
	default:
	}
}

func (s *OpenAIOAuthServiceSuite) TestTokenURL_CanBeOverriddenWithQuery() {
	s.setupServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		s.received <- r.PostForm
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"at","token_type":"bearer","expires_in":1}`)
	}))
	s.svc.tokenURL = s.srv.URL + "?x=1"

	_, err := s.svc.ExchangeCode(s.ctx, "code", "ver", openai.DefaultRedirectURI, "", "")
	require.NoError(s.T(), err, "ExchangeCode")
	select {
	case <-s.received:
	default:
		require.Fail(s.T(), "expected server to receive request")
	}
}

func (s *OpenAIOAuthServiceSuite) TestExchangeCode_SuccessButInvalidJSON() {
	s.setupServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "not-valid-json")
	}))

	_, err := s.svc.ExchangeCode(s.ctx, "code", "ver", openai.DefaultRedirectURI, "", "")
	require.Error(s.T(), err, "expected error for invalid JSON response")
}

func (s *OpenAIOAuthServiceSuite) TestRefreshToken_NonSuccessStatus() {
	s.setupServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, "unauthorized")
	}))

	_, err := s.svc.RefreshToken(s.ctx, "rt", "")
	require.Error(s.T(), err, "expected error for non-2xx status")
	require.ErrorContains(s.T(), err, "status 401")
}

func TestNewOpenAIOAuthClient_DefaultTokenURL(t *testing.T) {
	upstream := &openAIOAuthHTTPUpstreamStub{do: func(*http.Request, string, int64, int) (*http.Response, error) {
		return nil, nil
	}}
	client := NewOpenAIOAuthClient(upstream)
	svc, ok := client.(*openaiOAuthService)
	require.True(t, ok)
	require.Equal(t, openai.TokenURL, svc.tokenURL)
	require.Same(t, upstream, svc.httpUpstream)
}

func TestOpenAIOAuthServiceSuite(t *testing.T) {
	suite.Run(t, new(OpenAIOAuthServiceSuite))
}
