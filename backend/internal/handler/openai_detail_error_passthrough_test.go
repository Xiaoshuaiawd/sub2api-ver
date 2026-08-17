package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOpenAIHandleFailoverExhaustedPreservesDetailEnvelope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	body := []byte(`{"detail":"Unsupported parameter: max_output_tokens","request_id":"req_123"}`)

	(&OpenAIGatewayHandler{}).handleFailoverExhausted(c, &service.UpstreamFailoverError{
		StatusCode:   http.StatusBadRequest,
		ResponseBody: body,
	}, false)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.JSONEq(t, string(body), rec.Body.String())
}
