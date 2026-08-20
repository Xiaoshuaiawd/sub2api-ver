package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
)

type liveHTTPUpstreamStub struct {
	request *http.Request
	body    []byte
	status  int
	result  string
	err     error
}

type liveAttestationStub struct {
	header string
	err    error
}

func (s liveAttestationStub) Check(context.Context) error {
	return s.err
}

func (s liveAttestationStub) Generate(context.Context) (string, error) {
	return s.header, s.err
}

func (s *liveHTTPUpstreamStub) Do(
	request *http.Request,
	_ string,
	_ int64,
	_ int,
) (*http.Response, error) {
	s.request = request
	body, err := io.ReadAll(request.Body)
	if err != nil {
		return nil, err
	}
	s.body = body
	if s.err != nil {
		return nil, s.err
	}
	status := s.status
	if status == 0 {
		status = http.StatusOK
	}
	result := s.result
	if result == "" {
		result = "v=0\r\n"
	}
	return &http.Response{
		StatusCode: status,
		Header: http.Header{
			"Location": {"/backend-api/codex/call_test"},
		},
		Body: io.NopCloser(strings.NewReader(result)),
	}, nil
}

func (s *liveHTTPUpstreamStub) DoWithTLS(
	request *http.Request,
	proxyURL string,
	accountID int64,
	accountConcurrency int,
	_ *tlsfingerprint.Profile,
) (*http.Response, error) {
	return s.Do(request, proxyURL, accountID, accountConcurrency)
}

type liveFeedbackScheduler struct {
	selection *AccountSelectionResult
	selects   int
}

func (s *liveFeedbackScheduler) Select(context.Context, OpenAIAccountScheduleRequest) (*AccountSelectionResult, OpenAIAccountScheduleDecision, error) {
	s.selects++
	if s.selects > 1 || s.selection == nil {
		return nil, OpenAIAccountScheduleDecision{}, ErrNoAvailableAccounts
	}
	return s.selection, OpenAIAccountScheduleDecision{}, nil
}

func (*liveFeedbackScheduler) ReportResult(int64, string, bool, *int) {}
func (*liveFeedbackScheduler) ReportSwitch()                          {}
func (*liveFeedbackScheduler) SnapshotMetrics() OpenAIAccountSchedulerMetricsSnapshot {
	return OpenAIAccountSchedulerMetricsSnapshot{}
}

func newLiveFeedbackTestService(
	t *testing.T,
	upstream *liveHTTPUpstreamStub,
	store *liveTestStore,
	report func(bool),
) *OpenAIGatewayService {
	t.Helper()
	cfg := &config.Config{JWT: config.JWTConfig{Secret: "live-feedback-secret"}}
	cfg.Gateway.OpenAIScheduler.AdaptiveEnabled = true
	account := &Account{
		ID:          77,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 2,
		Credentials: map[string]any{
			"access_token":       "test-access-token",
			"chatgpt_account_id": "acct_test",
		},
	}
	selection := &AccountSelectionResult{
		Account:     account,
		Acquired:    true,
		ReleaseFunc: func() {},
		reportResult: func(_ string, success bool, _ *int) {
			report(success)
		},
	}
	return &OpenAIGatewayService{
		accountRepo:           &liveTestAccountRepo{account: account},
		cache:                 store,
		cfg:                   cfg,
		concurrencyService:    NewConcurrencyService(&liveTestConcurrencyCache{}),
		httpUpstream:          upstream,
		openaiScheduler:       &liveFeedbackScheduler{selection: selection},
		liveAttestation:       liveAttestationStub{header: `{"v":1}`},
		liveAttestationCipher: newLiveAttestationCipher(cfg),
	}
}

func TestCreateLiveCallReportsAdaptiveSelectionOutcome(t *testing.T) {
	request := &LiveCallRequest{
		SDP:     "v=offer\r\n",
		Session: json.RawMessage(`{"model":"gpt-live-test"}`),
	}
	identity := LiveCallIdentity{APIKeyID: 2, UserID: 3}

	t.Run("upstream failure", func(t *testing.T) {
		var outcomes []bool
		svc := newLiveFeedbackTestService(t, &liveHTTPUpstreamStub{
			status: http.StatusInternalServerError,
			result: `{"error":{"message":"upstream unavailable"}}`,
		}, &liveTestStore{}, func(success bool) { outcomes = append(outcomes, success) })

		_, err := svc.CreateLiveCall(context.Background(), request, identity, 2)
		require.Error(t, err)
		require.Equal(t, []bool{false}, outcomes)
	})

	t.Run("upstream success before local save failure", func(t *testing.T) {
		var outcomes []bool
		svc := newLiveFeedbackTestService(t, &liveHTTPUpstreamStub{}, &liveTestStore{
			saveErr: errors.New("redis unavailable"),
		}, func(success bool) { outcomes = append(outcomes, success) })

		_, err := svc.CreateLiveCall(context.Background(), request, identity, 2)
		require.Error(t, err)
		require.Equal(t, []bool{true}, outcomes)
	})

	t.Run("client cancellation", func(t *testing.T) {
		var outcomes []bool
		svc := newLiveFeedbackTestService(t, &liveHTTPUpstreamStub{
			err: context.Canceled,
		}, &liveTestStore{}, func(success bool) { outcomes = append(outcomes, success) })

		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := svc.CreateLiveCall(ctx, request, identity, 2)
		require.ErrorIs(t, err, context.Canceled)
		require.Empty(t, outcomes)
	})
}

func TestLiveCapabilityOnlyAllowsOpenAIOAuth(t *testing.T) {
	require.True(t, (&Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}).SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityLive))
	require.False(t, (&Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}).SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityLive))
	require.False(t, (&Account{Platform: PlatformGrok, Type: AccountTypeOAuth}).SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityLive))
	require.False(t, (&Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			openAIAuthModeCredentialKey: OpenAIAuthModePersonalAccessToken,
		},
	}).SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityLive))
	require.False(t, (&Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			openAIAuthModeCredentialKey: OpenAIAuthModeAgentIdentity,
		},
	}).SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityLive))
}

func TestValidateLiveCallRequestDoesNotRequireDelegation(t *testing.T) {
	request := &LiveCallRequest{
		SDP:     "v=0\r\n",
		Session: json.RawMessage(`{"model":"gpt-live-test","instructions":"hello"}`),
	}
	require.NoError(t, ValidateLiveCallRequest(request))
	require.NotContains(t, string(request.Session), "delegation")
}

func TestCreateUpstreamLiveCallPreservesSession(t *testing.T) {
	upstream := &liveHTTPUpstreamStub{}
	service := &OpenAIGatewayService{
		cfg:          &config.Config{},
		httpUpstream: upstream,
	}
	account := &Account{
		ID:          7,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Concurrency: 2,
		Credentials: map[string]any{
			"access_token":       "test-access-token",
			"chatgpt_account_id": "acct_test",
		},
	}
	session := json.RawMessage(`{
		"model":"gpt-live-test",
		"delegation":{"type":"client"},
		"custom":{"keep":true}
	}`)

	created, err := service.createUpstreamLiveCall(context.Background(), account, &LiveCallRequest{
		SDP:     "v=offer\r\n",
		Session: session,
	}, `{"v":1,"s":0,"t":"v1.test"}`)
	require.NoError(t, err)
	require.Equal(t, "call_test", created.CallID)
	require.Equal(t, []byte("v=0\r\n"), created.SDP)

	var forwarded struct {
		SDP     string          `json:"sdp"`
		Session json.RawMessage `json:"session"`
	}
	require.NoError(t, json.Unmarshal(upstream.body, &forwarded))
	require.Equal(t, "v=offer\r\n", forwarded.SDP)
	require.JSONEq(t, string(session), string(forwarded.Session))
	require.Equal(t, "Bearer test-access-token", upstream.request.Header.Get("Authorization"))
	require.Equal(t, "acct_test", upstream.request.Header.Get("Chatgpt-Account-Id"))
	require.Equal(t, "quicksilver=v2", upstream.request.Header.Get("OpenAI-Alpha"))
	require.Equal(t, `{"v":1,"s":0,"t":"v1.test"}`, upstream.request.Header.Get(liveAttestationHeader))
	require.NotEmpty(t, upstream.request.Header.Get("Session-Id"))
	require.NotEmpty(t, upstream.request.Header.Get("Thread-Id"))
	require.Empty(t, upstream.request.Header.Get("OpenAI-Beta"))
	require.Equal(t, HTTPUpstreamProfileOpenAI, HTTPUpstreamProfileFromContext(upstream.request.Context()))
	require.True(t, HTTPUpstreamRedirectsDisabled(upstream.request.Context()))
}

func TestLiveAttestationCipherRoundTripAndRejectsOtherInstanceKey(t *testing.T) {
	first := newLiveAttestationCipher(&config.Config{
		JWT: config.JWTConfig{Secret: "first-live-secret"},
	})
	second := newLiveAttestationCipher(&config.Config{
		JWT: config.JWTConfig{Secret: "second-live-secret"},
	})
	require.NotNil(t, first)
	require.NotNil(t, second)

	ciphertext, err := first.Encrypt(`{"v":1,"s":0,"t":"v1.opaque"}`)
	require.NoError(t, err)
	require.NotContains(t, ciphertext, "opaque")

	plaintext, err := first.Decrypt(ciphertext)
	require.NoError(t, err)
	require.Equal(t, `{"v":1,"s":0,"t":"v1.opaque"}`, plaintext)

	_, err = second.Decrypt(ciphertext)
	require.Error(t, err)
}

func TestPrepareLiveAttestationEncryptsHeaderAndReturnsExplicitProviderError(t *testing.T) {
	cipher := newLiveAttestationCipher(&config.Config{
		JWT: config.JWTConfig{Secret: "live-attestation-test-secret"},
	})
	service := &OpenAIGatewayService{
		liveAttestation:       liveAttestationStub{header: `{"v":1,"s":0,"t":"v1.test"}`},
		liveAttestationCipher: cipher,
	}
	header, ciphertext, err := service.prepareLiveAttestation(context.Background())
	require.NoError(t, err)
	require.Equal(t, `{"v":1,"s":0,"t":"v1.test"}`, header)
	require.NotContains(t, ciphertext, "v1.test")
	decrypted, err := cipher.Decrypt(ciphertext)
	require.NoError(t, err)
	require.Equal(t, header, decrypted)

	service.liveAttestation = liveAttestationStub{err: errors.New("macOS app missing")}
	_, _, err = service.prepareLiveAttestation(context.Background())
	var unavailable *LiveAttestationUnavailableError
	require.ErrorAs(t, err, &unavailable)
	require.Contains(t, unavailable.Error(), "macOS app missing")
}

func TestLiveMaxSessionDurationDefaultsAndOverrides(t *testing.T) {
	require.Equal(t, defaultLiveMaxSessionDuration, (&OpenAIGatewayService{}).liveMaxSessionDuration())
	require.Equal(
		t,
		90*time.Second,
		(&OpenAIGatewayService{cfg: &config.Config{
			Gateway: config.GatewayConfig{
				Live: config.GatewayLiveConfig{MaxSessionDurationSeconds: 90},
			},
		}}).liveMaxSessionDuration(),
	)
}

func TestLiveSidebandNormalCloseEndsCall(t *testing.T) {
	normalClose := coderws.CloseError{Code: coderws.StatusNormalClosure}
	require.ErrorIs(t, liveSidebandReadError(normalClose), ErrLiveCallNotFound)

	abnormalClose := coderws.CloseError{Code: coderws.StatusInternalError}
	require.Equal(t, abnormalClose, liveSidebandReadError(abnormalClose))
}

func TestLiveCreateFailoverUsesExistingOpenAIPolicy(t *testing.T) {
	service := &OpenAIGatewayService{}
	require.False(t, service.shouldFailoverLiveCreateError(&UpstreamFailoverError{
		StatusCode:   http.StatusBadRequest,
		ResponseBody: []byte(`{"error":{"message":"invalid session"}}`),
	}))
	require.True(t, service.shouldFailoverLiveCreateError(&UpstreamFailoverError{
		StatusCode: http.StatusForbidden,
	}))
	require.True(t, service.shouldFailoverLiveCreateError(&UpstreamFailoverError{
		StatusCode: http.StatusBadGateway,
	}))
	require.True(t, service.shouldFailoverLiveCreateError(errors.New("transport failed")))
}

func TestLiveCallIDFromLocation(t *testing.T) {
	callID, err := liveCallIDFromLocation("https://chatgpt.com/backend-api/codex/call_123?intent=quicksilver")
	require.NoError(t, err)
	require.Equal(t, "call_123", callID)

	callID, err = liveCallIDFromLocation("/backend-api/codex/call_456")
	require.NoError(t, err)
	require.Equal(t, "call_456", callID)
}

func TestRequestTypeLive(t *testing.T) {
	require.True(t, RequestTypeLive.IsValid())
	require.Equal(t, "live", RequestTypeLive.String())
	parsed, err := ParseUsageRequestType("live")
	require.NoError(t, err)
	require.Equal(t, RequestTypeLive, parsed)
}
