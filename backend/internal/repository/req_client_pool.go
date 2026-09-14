package repository

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/proxyurl"
	"github.com/Wei-Shaw/sub2api/internal/pkg/servertiming"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/imroc/req/v3"
)

// reqClientOptions 定义 req 客户端的构建参数
type reqClientOptions struct {
	ProxyURL    string        // 代理 URL（支持 http/https/socks5）
	Timeout     time.Duration // 请求超时时间
	Impersonate bool          // 是否模拟浏览器指纹（当前为 Firefox，Chrome 伪装会被 chatgpt.com 的 Cloudflare 质询）
	ForceHTTP2  bool          // 是否强制使用 HTTP/2
}

// sharedReqClients 存储按配置参数缓存的 req 客户端实例
//
// 性能优化说明：
// 原实现在每次 OAuth 刷新时都创建新的 req.Client：
// 1. claude_oauth_service.go: 每次刷新创建新客户端
// 2. openai_oauth_service.go: 每次刷新创建新客户端
// 3. gemini_oauth_client.go: 每次刷新创建新客户端
//
// 新实现使用 sync.Map 缓存客户端：
// 1. 相同配置（代理+超时+模拟设置）复用同一客户端
// 2. 复用底层连接池，减少 TLS 握手开销
// 3. LoadOrStore 保证并发安全，避免重复创建
var sharedReqClients sync.Map

// getSharedReqClient 获取共享的 req 客户端实例
// 性能优化：相同配置复用同一客户端，避免重复创建
func getSharedReqClient(opts reqClientOptions) (*req.Client, error) {
	key := buildReqClientKey(opts)
	if cached, ok := sharedReqClients.Load(key); ok {
		if c, ok := cached.(*req.Client); ok {
			return c, nil
		}
	}

	client := req.C().SetTimeout(opts.Timeout)
	if opts.ForceHTTP2 {
		client = client.EnableForceHTTP2()
	}
	if opts.Impersonate {
		// chatgpt.com 的 Cloudflare 会对 req 内置的 Chrome 伪装（UA 固定为 Chrome/120，
		// 与 sec-ch-ua 等 Client Hints 一起已明显过时）直接返回 403 cf-mitigated=challenge，
		// 导致 accounts/check、subscriptions、隐私设置等 backend-api 调用全部失败，
		// 订阅到期时间因此长期不更新（见 issue #4825）。Firefox 伪装的 UA/头部组合
		// 在同一出口 IP 下稳定通过，故改用 Firefox 指纹。
		client = client.ImpersonateFirefox()
	}
	trimmed, _, err := proxyurl.Parse(opts.ProxyURL)
	if err != nil {
		return nil, err
	}
	if trimmed != "" {
		client.SetProxyURL(trimmed)
	}
	client = instrumentReqClient(client)

	actual, _ := sharedReqClients.LoadOrStore(key, client)
	if c, ok := actual.(*req.Client); ok {
		return c, nil
	}
	return client, nil
}

func instrumentReqClient(client *req.Client) *req.Client {
	if client == nil {
		return nil
	}
	client.GetTransport().WrapRoundTripFunc(func(rt http.RoundTripper) req.HttpRoundTripFunc {
		timed := servertiming.WrapRoundTripper(rt)
		return timed.RoundTrip
	})
	return client
}

func buildReqClientKey(opts reqClientOptions) string {
	return fmt.Sprintf("%s|%s|%t|%t",
		strings.TrimSpace(opts.ProxyURL),
		opts.Timeout.String(),
		opts.Impersonate,
		opts.ForceHTTP2,
	)
}

type openAIPrivacyRoundTripper struct {
	policy          service.OpenAIProxyPolicyProvider
	primaryProxyURL string
	transportFor    func(proxyURL string) (http.RoundTripper, error)
}

func (r *openAIPrivacyRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	if r.policy == nil {
		return nil, fmt.Errorf("OpenAI privacy proxy policy is not configured")
	}
	if r.transportFor == nil {
		return nil, fmt.Errorf("OpenAI privacy transport factory is not configured")
	}
	plan := r.policy.Resolve(request.Context(), r.primaryProxyURL)
	return executeOpenAIProxyAttempts(request, plan, 0, 1, openAIProxyMetricsRecorder(r.policy), func(attempt *http.Request, proxyURL string, _ int64, _ int) (*http.Response, error) {
		transport, err := r.transportFor(proxyURL)
		if err != nil {
			return nil, err
		}
		return transport.RoundTrip(attempt)
	})
}

type openAIPrivacyTransportPool struct {
	base       *req.Transport
	transports sync.Map
}

func (p *openAIPrivacyTransportPool) get(proxyURL string) (http.RoundTripper, error) {
	normalized, parsed, err := proxyurl.Parse(proxyURL)
	if err != nil {
		return nil, err
	}
	key := normalized
	if key == "" {
		key = directProxyKey
	}
	if cached, ok := p.transports.Load(key); ok {
		if transport, ok := cached.(*req.Transport); ok {
			return transport, nil
		}
	}

	transport := p.base.Clone()
	if parsed == nil {
		transport.SetProxy(nil)
	} else {
		transport.SetProxy(http.ProxyURL(parsed))
	}
	actual, _ := p.transports.LoadOrStore(key, transport)
	if cached, ok := actual.(*req.Transport); ok {
		return cached, nil
	}
	return transport, nil
}

// NewPrivacyClientFactory creates Chrome-impersonated ChatGPT clients whose
// requests use the shared OpenAI proxy policy at RoundTrip time.
func NewPrivacyClientFactory(policy service.OpenAIProxyPolicyProvider) service.PrivacyClientFactory {
	baseClient := req.C().SetTimeout(30 * time.Second).ImpersonateChrome()
	pool := &openAIPrivacyTransportPool{base: baseClient.GetTransport().Clone()}

	return func(primaryProxyURL string) (*req.Client, error) {
		if _, _, err := proxyurl.Parse(primaryProxyURL); err != nil {
			return nil, err
		}
		client := baseClient.Clone()
		router := &openAIPrivacyRoundTripper{
			policy:          policy,
			primaryProxyURL: primaryProxyURL,
			transportFor:    pool.get,
		}
		client.GetTransport().WrapRoundTripFunc(func(http.RoundTripper) req.HttpRoundTripFunc {
			return router.RoundTrip
		})
		return instrumentReqClient(client), nil
	}
}
