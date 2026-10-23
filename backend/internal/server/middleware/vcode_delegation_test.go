//go:build unit

package middleware

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type vcodeManagedKeyStub struct{ calls int }

func (s *vcodeManagedKeyStub) EnsureVcodeManagedKey(_ context.Context, userID, groupID int64, secret string) (string, error) {
	s.calls++
	if userID != 17 || groupID != 4 || secret != "different-managed-key-secret-at-least-32-bytes" {
		return "", fmt.Errorf("unexpected principal")
	}
	return "sk-managed-test", nil
}

func TestVcodeDelegationRejectsTamperingAndReplayBeforeKeyLookup(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()
	keys := &vcodeManagedKeyStub{}
	secret := "vcode-delegation-secret-at-least-32-bytes"
	r := gin.New()
	r.Use(RequestBodyLimit(4096), NewVcodeDelegationMiddleware(keys, client, secret, "different-managed-key-secret-at-least-32-bytes"))
	r.POST("/v1/messages", func(c *gin.Context) {
		if !c.GetBool("vcode_delegated") {
			c.AbortWithStatus(http.StatusInternalServerError)
			return
		}
		c.String(200, c.GetHeader("Authorization"))
	})
	body := []byte(`{"model":"claude-test"}`)
	makeRequest := func(payload []byte, valid bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(payload))
		timestamp := fmt.Sprint(time.Now().Unix())
		bodyDigest := sha256.Sum256(body)
		bodyHash := hex.EncodeToString(bodyDigest[:])
		canonical := fmt.Sprintf("POST\n/v1/messages\n17\n4\n%s\ntest-nonce-1234567890\n%s", timestamp, bodyHash)
		mac := hmac.New(sha256.New, []byte(secret))
		_, _ = mac.Write([]byte(canonical))
		sig := hex.EncodeToString(mac.Sum(nil))
		if !valid {
			sig = "00" + sig[2:]
		}
		req.Header.Set("X-Vcode-User", "17")
		req.Header.Set("X-Vcode-Group", "4")
		req.Header.Set("X-Vcode-Timestamp", timestamp)
		req.Header.Set("X-Vcode-Nonce", "test-nonce-1234567890")
		req.Header.Set("X-Vcode-Body-SHA256", bodyHash)
		req.Header.Set("X-Vcode-Signature", sig)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	require.Equal(t, http.StatusUnauthorized, makeRequest(body, false).Code)
	require.Equal(t, http.StatusUnauthorized, makeRequest([]byte(`{"model":"other"}`), true).Code)
	require.Zero(t, keys.calls)
	good := makeRequest(body, true)
	require.Equal(t, http.StatusOK, good.Code)
	require.Equal(t, "Bearer sk-managed-test", good.Body.String())
	require.Equal(t, http.StatusUnauthorized, makeRequest(body, true).Code)
	require.Equal(t, 1, keys.calls)
}
