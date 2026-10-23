//go:build unit

package routes

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestVcodeAuthorizationCodeIsOneUseAndBoundToVerifier(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()
	store := newVcodeAuthorizationCodeStore(client)
	verifier := "one-long-unpredictable-verifier-for-pkce-tests"
	digest := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(digest[:])
	code, err := store.Issue(context.Background(), 42, challenge)
	require.NoError(t, err)
	require.NotEmpty(t, code)
	userID, err := store.Consume(context.Background(), code, verifier)
	require.NoError(t, err)
	require.Equal(t, int64(42), userID)
	_, err = store.Consume(context.Background(), code, verifier)
	require.Error(t, err)
}

func TestVcodeAuthRoutesExchangeCodeForActiveUserOnce(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()
	router := gin.New()
	jwt := middleware.JWTAuthMiddleware(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 42})
		c.Next()
	})
	secret := "vcode-integration-secret-32-bytes-for-test"
	RegisterVcodeAuthRoutes(router.Group("/api/v1"), jwt, client, func(_ context.Context, id int64) (*service.User, error) {
		return &service.User{ID: id, Username: "alice", Status: service.StatusActive}, nil
	}, func(_ context.Context, id int64) (any, error) {
		return []map[string]any{{"id": 7, "name": "allowed"}}, nil
	}, nil, secret)
	verifier := "one-long-unpredictable-verifier-for-pkce-tests"
	digest := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(digest[:])
	issueBody, _ := json.Marshal(map[string]string{"code_challenge": challenge})
	issued := httptest.NewRecorder()
	router.ServeHTTP(issued, httptest.NewRequest(http.MethodPost, "/api/v1/vcode/authorize", bytes.NewReader(issueBody)))
	require.Equal(t, http.StatusOK, issued.Code)
	var issueResponse struct {
		Data struct {
			Code string `json:"code"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(issued.Body.Bytes(), &issueResponse))
	require.NotEmpty(t, issueResponse.Data.Code)

	exchangeBody, _ := json.Marshal(map[string]string{"code": issueResponse.Data.Code, "code_verifier": verifier})
	exchange := func(secret string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/vcode/exchange", bytes.NewReader(exchangeBody))
		req.Header.Set("X-Vcode-Client-Secret", secret)
		router.ServeHTTP(w, req)
		return w
	}
	require.Equal(t, http.StatusUnauthorized, exchange("wrong-secret").Code)
	first := exchange(secret)
	require.Equal(t, http.StatusOK, first.Code)
	require.Contains(t, first.Body.String(), `"user_id":42`)
	require.Contains(t, first.Body.String(), `"username":"alice"`)
	require.Equal(t, http.StatusUnauthorized, exchange(secret).Code)
	catalog := httptest.NewRecorder()
	catalogRequest := httptest.NewRequest(http.MethodGet, "/api/v1/vcode/catalog/42", nil)
	catalogRequest.Header.Set("X-Vcode-Client-Secret", secret)
	router.ServeHTTP(catalog, catalogRequest)
	require.Equal(t, http.StatusOK, catalog.Code)
	require.Contains(t, catalog.Body.String(), `"name":"allowed"`)
}

func TestVcodeAuthorizationCodeWrongVerifierConsumesCode(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()
	store := newVcodeAuthorizationCodeStore(client)
	verifier := "one-long-unpredictable-verifier-for-pkce-tests"
	digest := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(digest[:])
	code, err := store.Issue(context.Background(), 42, challenge)
	require.NoError(t, err)
	_, err = store.Consume(context.Background(), code, "another-long-unpredictable-verifier-value-long")
	require.Error(t, err)
	_, err = store.Consume(context.Background(), code, verifier)
	require.Error(t, err)
}

func TestVcodeAuthorizationCodeIssueRateLimitPerUser(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()
	store := newVcodeAuthorizationCodeStore(client)
	challenge := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	for range 20 {
		_, err := store.Issue(context.Background(), 42, challenge)
		require.NoError(t, err)
	}
	_, err := store.Issue(context.Background(), 42, challenge)
	require.Error(t, err)
	_, err = store.Issue(context.Background(), 43, challenge)
	require.NoError(t, err)
}
