package routes

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

const vcodeAuthorizationCodeTTL = 5 * time.Minute

var errVcodeAuthorizationRateLimited = errors.New("Vcode authorization rate limited")

var vcodeChallengePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)

type vcodeAuthorizationCodeStore struct {
	redis *redis.Client
}

type vcodeAuthorizationCodeRecord struct {
	UserID    int64  `json:"user_id"`
	Challenge string `json:"challenge"`
}

func newVcodeAuthorizationCodeStore(redisClient *redis.Client) *vcodeAuthorizationCodeStore {
	return &vcodeAuthorizationCodeStore{redis: redisClient}
}

func vcodeAuthorizationCodeKey(code string) string {
	digest := sha256.Sum256([]byte(code))
	return "vcode:authorization:" + base64.RawURLEncoding.EncodeToString(digest[:])
}

func (s *vcodeAuthorizationCodeStore) Issue(ctx context.Context, userID int64, challenge string) (string, error) {
	if s == nil || s.redis == nil || userID <= 0 || !vcodeChallengePattern.MatchString(challenge) {
		return "", errors.New("invalid Vcode authorization request")
	}
	rateKey := fmt.Sprintf("vcode:authorize-rate:%d:%d", userID, time.Now().Unix()/60)
	count, err := s.redis.Incr(ctx, rateKey).Result()
	if err != nil {
		return "", err
	}
	if count == 1 {
		if err := s.redis.Expire(ctx, rateKey, 2*time.Minute).Err(); err != nil {
			return "", err
		}
	}
	if count > 20 {
		return "", errVcodeAuthorizationRateLimited
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	code := base64.RawURLEncoding.EncodeToString(raw)
	payload, err := json.Marshal(vcodeAuthorizationCodeRecord{UserID: userID, Challenge: challenge})
	if err != nil {
		return "", err
	}
	if err := s.redis.Set(ctx, vcodeAuthorizationCodeKey(code), payload, vcodeAuthorizationCodeTTL).Err(); err != nil {
		return "", err
	}
	return code, nil
}

func (s *vcodeAuthorizationCodeStore) Consume(ctx context.Context, code, verifier string) (int64, error) {
	if s == nil || s.redis == nil || len(code) != 43 || len(verifier) < 43 || len(verifier) > 128 {
		return 0, errors.New("invalid Vcode authorization code")
	}
	payload, err := s.redis.GetDel(ctx, vcodeAuthorizationCodeKey(code)).Bytes()
	if err != nil {
		return 0, err
	}
	var record vcodeAuthorizationCodeRecord
	if err := json.Unmarshal(payload, &record); err != nil {
		return 0, err
	}
	digest := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(digest[:])
	if record.UserID <= 0 || subtle.ConstantTimeCompare([]byte(challenge), []byte(record.Challenge)) != 1 {
		return 0, errors.New("Vcode authorization verifier mismatch")
	}
	return record.UserID, nil
}

type vcodeUserLookup func(context.Context, int64) (*service.User, error)
type vcodeCatalogLookup func(context.Context, int64) (any, error)

// RegisterVcodeAuthRoutes 只在部署端配置了独立服务间密钥时开放 Vcode 授权。
// 密码、验证码与双重验证仍走 sub2api 原登录；授权码只携带一次性用户身份。
func RegisterVcodeAuthRoutes(v1 *gin.RouterGroup, jwtAuth middleware.JWTAuthMiddleware, redisClient *redis.Client, lookup vcodeUserLookup, catalog vcodeCatalogLookup, settingService *service.SettingService, secret string) {
	if len(secret) < 32 || redisClient == nil || lookup == nil || catalog == nil {
		return
	}
	store := newVcodeAuthorizationCodeStore(redisClient)
	authorize := v1.Group("/vcode")
	authorize.Use(gin.HandlerFunc(jwtAuth))
	authorize.Use(middleware.BackendModeUserGuard(settingService))
	authorize.POST("/authorize", func(c *gin.Context) {
		subject, ok := middleware.GetAuthSubjectFromContext(c)
		if !ok || subject.UserID <= 0 {
			response.Unauthorized(c, "User not authenticated")
			return
		}
		var body struct {
			CodeChallenge string `json:"code_challenge" binding:"required"`
		}
		if err := c.ShouldBindJSON(&body); err != nil || !vcodeChallengePattern.MatchString(body.CodeChallenge) {
			response.BadRequest(c, "Invalid PKCE challenge")
			return
		}
		code, err := store.Issue(c.Request.Context(), subject.UserID, body.CodeChallenge)
		if err != nil {
			if errors.Is(err, errVcodeAuthorizationRateLimited) {
				response.Error(c, http.StatusTooManyRequests, "Too many authorization attempts")
				return
			}
			response.Error(c, http.StatusServiceUnavailable, "Authorization unavailable")
			return
		}
		c.Header("Cache-Control", "no-store")
		response.Success(c, gin.H{"code": code})
	})

	serviceRoutes := v1.Group("/vcode")
	serviceRoutes.Use(func(c *gin.Context) {
		// 固定长度比较避免服务间密钥被逐字节探测；不记录提供的值。
		if subtle.ConstantTimeCompare([]byte(c.GetHeader("X-Vcode-Client-Secret")), []byte(secret)) != 1 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		c.Next()
	})
	serviceRoutes.POST("/exchange", func(c *gin.Context) {
		var body struct {
			Code     string `json:"code" binding:"required"`
			Verifier string `json:"code_verifier" binding:"required"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			response.BadRequest(c, "Invalid authorization exchange")
			return
		}
		userID, err := store.Consume(c.Request.Context(), body.Code, body.Verifier)
		if err != nil {
			response.Unauthorized(c, "Invalid authorization code")
			return
		}
		writeVcodeIdentity(c, lookup, userID)
	})
	serviceRoutes.GET("/identity/:id", func(c *gin.Context) {
		userID, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil || userID <= 0 {
			response.BadRequest(c, "Invalid user ID")
			return
		}
		writeVcodeIdentity(c, lookup, userID)
	})
	serviceRoutes.GET("/catalog/:id", func(c *gin.Context) {
		userID, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil || userID <= 0 {
			response.BadRequest(c, "Invalid user ID")
			return
		}
		user, err := lookup(c.Request.Context(), userID)
		if err != nil || user == nil || !user.IsActive() {
			response.Unauthorized(c, "User unavailable")
			return
		}
		groups, err := catalog(c.Request.Context(), userID)
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
		c.Header("Cache-Control", "no-store")
		response.Success(c, gin.H{"groups": groups})
	})
}

func writeVcodeIdentity(c *gin.Context, lookup vcodeUserLookup, userID int64) {
	user, err := lookup(c.Request.Context(), userID)
	if err != nil || user == nil || !user.IsActive() {
		response.Unauthorized(c, "User unavailable")
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, gin.H{"user_id": user.ID, "username": user.Username})
}
