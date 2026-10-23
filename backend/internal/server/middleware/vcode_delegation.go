package middleware

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"

	"github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

type VcodeManagedKeyEnsurer interface {
	EnsureVcodeManagedKey(ctx context.Context, userID, groupID int64, secret string) (string, error)
}

var vcodeNoncePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{16,80}$`)

var vcodeDelegationHeaders = [...]string{
	"X-Vcode-User", "X-Vcode-Group", "X-Vcode-Timestamp", "X-Vcode-Nonce",
	"X-Vcode-Body-SHA256", "X-Vcode-Signature",
}

// NewVcodeDelegationMiddleware 把服务间委托转换为仅在 sub2api 内使用的受管 Key。
// 后续仍经过原 APIKeyAuth、分组白名单、额度与计费链路。
func NewVcodeDelegationMiddleware(keys VcodeManagedKeyEnsurer, redisClient *redis.Client, secret, managedKeySecret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		hasDelegation := false
		for _, name := range vcodeDelegationHeaders {
			if c.GetHeader(name) != "" {
				hasDelegation = true
				break
			}
		}
		if !hasDelegation {
			c.Next()
			return
		}
		deny := func() { c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid_vcode_delegation"}) }
		if len(secret) < 32 || len(managedKeySecret) < 32 || secret == managedKeySecret || keys == nil || redisClient == nil || c.Request == nil ||
			c.Request.Method != http.MethodPost || c.GetHeader("Authorization") != "" || c.GetHeader("x-api-key") != "" ||
			(c.Request.URL.Path != "/v1/messages" && c.Request.URL.Path != "/v1/responses" && c.Request.URL.Path != "/v1/chat/completions") ||
			c.GetHeader("Content-Encoding") != "" {
			deny()
			return
		}
		userText, groupText := c.GetHeader("X-Vcode-User"), c.GetHeader("X-Vcode-Group")
		userID, userErr := strconv.ParseInt(userText, 10, 64)
		groupID, groupErr := strconv.ParseInt(groupText, 10, 64)
		timestampText := c.GetHeader("X-Vcode-Timestamp")
		timestamp, timeErr := strconv.ParseInt(timestampText, 10, 64)
		nonce := c.GetHeader("X-Vcode-Nonce")
		bodyHash := c.GetHeader("X-Vcode-Body-SHA256")
		signature, sigErr := hex.DecodeString(c.GetHeader("X-Vcode-Signature"))
		if userErr != nil || groupErr != nil || timeErr != nil || sigErr != nil || userID <= 0 || groupID <= 0 ||
			len(signature) != sha256.Size || len(bodyHash) != 64 || !vcodeNoncePattern.MatchString(nonce) ||
			time.Since(time.Unix(timestamp, 0)) > time.Minute || time.Until(time.Unix(timestamp, 0)) > time.Minute {
			deny()
			return
		}
		body, err := httputil.ReadRequestBodyWithPrealloc(c.Request)
		if err != nil {
			deny()
			return
		}
		c.Request.Body = httputil.NewPrereadBody(body)
		bodyDigest := sha256.Sum256(body)
		if !hmac.Equal([]byte(bodyHash), []byte(hex.EncodeToString(bodyDigest[:]))) {
			deny()
			return
		}
		canonical := fmt.Sprintf("%s\n%s\n%s\n%s\n%s\n%s\n%s", c.Request.Method, c.Request.URL.RequestURI(), userText, groupText, timestampText, nonce, bodyHash)
		mac := hmac.New(sha256.New, []byte(secret))
		_, _ = mac.Write([]byte(canonical))
		if !hmac.Equal(signature, mac.Sum(nil)) {
			deny()
			return
		}
		nonceDigest := sha256.Sum256([]byte(nonce))
		fresh, err := redisClient.SetNX(c.Request.Context(), "vcode:delegation:"+hex.EncodeToString(nonceDigest[:]), "1", 2*time.Minute).Result()
		if err != nil || !fresh {
			deny()
			return
		}
		key, err := keys.EnsureVcodeManagedKey(c.Request.Context(), userID, groupID, managedKeySecret)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "vcode_group_unavailable"})
			return
		}
		for _, name := range vcodeDelegationHeaders {
			c.Request.Header.Del(name)
		}
		c.Request.Header.Set("Authorization", "Bearer "+key)
		c.Set("vcode_delegated", true)
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), ctxkey.VcodeSelectedGroupOnly, true))
		c.Next()
	}
}
