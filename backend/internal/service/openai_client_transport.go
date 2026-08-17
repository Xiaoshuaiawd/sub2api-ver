package service

import (
	"strings"

	"github.com/gin-gonic/gin"
)

// OpenAIClientTransport 表示客户端入站协议类型。
type OpenAIClientTransport string

const (
	OpenAIClientTransportUnknown OpenAIClientTransport = ""
	OpenAIClientTransportHTTP    OpenAIClientTransport = "http"
	OpenAIClientTransportWS      OpenAIClientTransport = "ws"
)

const (
	openAIClientTransportContextKey  = "openai_client_transport"
	openAIWSHTTPIngressBridgeContext = "openai_ws_http_ingress_bridge"
)

// SetOpenAIClientTransport 标记当前请求的客户端入站协议。
func SetOpenAIClientTransport(c *gin.Context, transport OpenAIClientTransport) {
	if c == nil {
		return
	}
	normalized := normalizeOpenAIClientTransport(transport)
	if normalized == OpenAIClientTransportUnknown {
		return
	}
	c.Set(openAIClientTransportContextKey, string(normalized))
}

// GetOpenAIClientTransport 读取当前请求的客户端入站协议。
func GetOpenAIClientTransport(c *gin.Context) OpenAIClientTransport {
	if c == nil {
		return OpenAIClientTransportUnknown
	}
	raw, ok := c.Get(openAIClientTransportContextKey)
	if !ok || raw == nil {
		return OpenAIClientTransportUnknown
	}

	switch v := raw.(type) {
	case OpenAIClientTransport:
		return normalizeOpenAIClientTransport(v)
	case string:
		return normalizeOpenAIClientTransport(OpenAIClientTransport(v))
	default:
		return OpenAIClientTransportUnknown
	}
}

func setOpenAIWSHTTPIngressBridge(c *gin.Context, enabled bool) {
	if c == nil {
		return
	}
	c.Set(openAIWSHTTPIngressBridgeContext, enabled)
}

func isOpenAIWSHTTPIngressBridge(c *gin.Context) bool {
	if c == nil {
		return false
	}
	enabled, ok := c.Get(openAIWSHTTPIngressBridgeContext)
	if !ok {
		return false
	}
	value, _ := enabled.(bool)
	return value
}

func isBareOpenAIResponsesHTTPPath(c *gin.Context) bool {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return false
	}
	normalizedPath := strings.TrimRight(strings.TrimSpace(c.Request.URL.Path), "/")
	switch normalizedPath {
	case "/v1/responses", "/openai/v1/responses", "/responses", "/backend-api/codex/responses":
		return true
	default:
		return false
	}
}

func normalizeOpenAIClientTransport(transport OpenAIClientTransport) OpenAIClientTransport {
	switch strings.ToLower(strings.TrimSpace(string(transport))) {
	case string(OpenAIClientTransportHTTP), "http_sse", "sse":
		return OpenAIClientTransportHTTP
	case string(OpenAIClientTransportWS), "websocket":
		return OpenAIClientTransportWS
	default:
		return OpenAIClientTransportUnknown
	}
}

func resolveOpenAIWSDecisionByClientTransport(
	decision OpenAIWSProtocolDecision,
	clientTransport OpenAIClientTransport,
	httpIngressBridgeEnabled bool,
) OpenAIWSProtocolDecision {
	if clientTransport == OpenAIClientTransportHTTP {
		if httpIngressBridgeEnabled {
			if decision.Transport == OpenAIUpstreamTransportResponsesWebsocketV2 {
				decision.Reason = "http_ingress_bridge_" + decision.Reason
			}
			return decision
		}
		return openAIWSHTTPDecision("client_protocol_http")
	}
	return decision
}
