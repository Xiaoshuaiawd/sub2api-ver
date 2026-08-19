package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type openAITTFTFlushObservation struct {
	body      string
	elapsedMs int
}

type openAITTFTFlushWriter struct {
	gin.ResponseWriter
	recorder *httptest.ResponseRecorder
	started  time.Time
	flushes  chan openAITTFTFlushObservation
}

func (w *openAITTFTFlushWriter) Flush() {
	w.ResponseWriter.Flush()
	observation := openAITTFTFlushObservation{
		body:      w.recorder.Body.String(),
		elapsedMs: int(time.Since(w.started).Milliseconds()),
	}
	select {
	case w.flushes <- observation:
	default:
	}
}

func TestOpenAIVisibleOutputClassification(t *testing.T) {
	tests := []struct {
		name      string
		data      string
		eventType string
		want      bool
	}{
		{name: "keepalive", data: `{"type":"keepalive"}`, want: false},
		{name: "created", data: `{"type":"response.created"}`, want: false},
		{name: "empty output item", data: `{"type":"response.output_item.added","item":{"id":"item_test","type":"reasoning","summary":[]}}`, want: false},
		{name: "empty delta", data: `{"type":"response.output_text.delta","delta":""}`, want: false},
		{name: "text delta", data: `{"type":"response.output_text.delta","delta":"test output"}`, want: true},
		{name: "tool arguments", data: `{"type":"response.function_call_arguments.delta","delta":"{}"}`, want: true},
		{name: "partial image", data: `{"type":"response.image_generation_call.partial_image","partial_image_b64":"dGVzdA=="}`, want: true},
		{name: "completed image item", data: `{"type":"response.output_item.done","item":{"id":"item_test","type":"image_generation_call","result":"dGVzdA=="}}`, want: true},
		{name: "empty completed", data: `{"type":"response.completed","response":{"id":"resp_test","output":[]}}`, want: false},
		{name: "completed with output usage only", data: `{"type":"response.completed","response":{"id":"resp_test","usage":{"input_tokens":1,"output_tokens":2}}}`, want: false},
		{name: "completed with text", data: `{"type":"response.completed","response":{"id":"resp_test","output":[{"type":"message","content":[{"type":"output_text","text":"test output"}]}]}}`, want: true},
		{name: "done marker", data: `[DONE]`, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, openAIStreamDataStartsVisibleOutput(tt.data, tt.eventType))
		})
	}
}

func TestOpenAIStreamEventStartsTTFTClassification(t *testing.T) {
	require.True(t, openAIStreamEventStartsTTFT(" response.created "))
	require.False(t, openAIStreamEventStartsTTFT("response.in_progress"))
	require.False(t, openAIStreamEventStartsTTFT("response.output_text.delta"))
}

func TestOpenAIResponsesTTFTStartsAtResponseCreatedAndFlushesImmediately(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		name := "native"
		if passthrough {
			name = "passthrough"
		}
		t.Run(name, func(t *testing.T) {
			result, firstFlush := runSyntheticCreatedTTFTStream(t, passthrough)
			require.NotNil(t, result.firstTokenMs)
			require.Contains(t, firstFlush.body, `"type":"response.created"`)
			require.NotContains(t, firstFlush.body, `"type":"response.output_text.delta"`)
			require.LessOrEqual(t, *result.firstTokenMs, firstFlush.elapsedMs+50)
		})
	}
}

func TestOpenAIResponsesTTFTFallsBackToVisibleOutputWithoutCreated(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		name := "native"
		if passthrough {
			name = "passthrough"
		}
		t.Run(name, func(t *testing.T) {
			result := runSyntheticVisibleTTFTStream(t, passthrough, false, 120*time.Millisecond, 0,
				`{"type":"response.output_text.delta","delta":"test output"}`)
			require.NotNil(t, result.firstTokenMs)
			require.GreaterOrEqual(t, *result.firstTokenMs, 100)
		})
	}
}

func TestOpenAIResponsesTTFTStartsAtCompletedImage(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		name := "native"
		if passthrough {
			name = "passthrough"
		}
		t.Run(name, func(t *testing.T) {
			result := runSyntheticVisibleTTFTStream(t, passthrough, false, 120*time.Millisecond, 0,
				`{"type":"response.output_item.done","item":{"id":"item_test","type":"image_generation_call","result":"dGVzdA=="}}`)
			require.NotNil(t, result.firstTokenMs)
			require.GreaterOrEqual(t, *result.firstTokenMs, 100)
		})
	}
}

func TestOpenAINativeProgressDisarmsTimeoutWithoutStartingTTFT(t *testing.T) {
	result := runSyntheticVisibleTTFTStream(t, false, false, 1200*time.Millisecond, 1,
		`{"type":"response.output_text.delta","delta":"test output"}`)
	require.NotNil(t, result.firstTokenMs)
	require.GreaterOrEqual(t, *result.firstTokenMs, 1100)
}

func TestOpenAINativeResponseCreatedFollowedByFailureDoesNotFailOver(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{
		MaxLineSize:                     defaultMaxLineSize,
		OpenAIFirstOutputTimeoutSeconds: 2,
	}}}
	stream := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_failed"}}`,
		"",
		`data: {"type":"response.failed","response":{"id":"resp_failed","error":{"code":"server_error","message":"upstream failed"}}}`,
		"",
	}, "\n")
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(stream))}

	result, err := svc.handleStreamingResponse(context.Background(), resp, c, &Account{ID: 1, Platform: PlatformOpenAI}, time.Now(), "model", "model")

	require.Error(t, err)
	var failoverErr *UpstreamFailoverError
	require.False(t, errors.As(err, &failoverErr))
	require.NotNil(t, result)
	require.NotNil(t, result.firstTokenMs)
	require.Contains(t, recorder.Body.String(), `"type":"response.created"`)
	require.Contains(t, recorder.Body.String(), `"type":"response.failed"`)
}

func runSyntheticCreatedTTFTStream(t *testing.T, passthrough bool) (*openaiStreamingResult, openAITTFTFlushObservation) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{
		MaxLineSize:                     defaultMaxLineSize,
		OpenAIFirstOutputTimeoutSeconds: 2,
	}}}
	reader, writer := io.Pipe()
	releaseVisible := make(chan struct{})
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		defer func() { _ = writer.Close() }()
		_, _ = io.WriteString(writer, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_created\"}}\n\n")
		<-releaseVisible
		_, _ = io.WriteString(writer, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"test output\"}\n\n")
		_, _ = io.WriteString(writer, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_created\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\n")
	}()

	started := time.Now()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	flushWriter := &openAITTFTFlushWriter{
		ResponseWriter: c.Writer,
		recorder:       recorder,
		started:        started,
		flushes:        make(chan openAITTFTFlushObservation, 8),
	}
	c.Writer = flushWriter
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: reader}
	account := &Account{ID: 1, Name: "account_test", Platform: PlatformOpenAI}
	type streamOutcome struct {
		result *openaiStreamingResult
		err    error
	}
	outcomeCh := make(chan streamOutcome, 1)
	go func() {
		if passthrough {
			passthroughResult, err := svc.handleStreamingResponsePassthrough(context.Background(), resp, c, account, started, "test-model", "test-model")
			var result *openaiStreamingResult
			if passthroughResult != nil {
				result = &openaiStreamingResult{firstTokenMs: passthroughResult.firstTokenMs}
			}
			outcomeCh <- streamOutcome{result: result, err: err}
			return
		}
		result, err := svc.handleStreamingResponse(context.Background(), resp, c, account, started, "test-model", "test-model")
		outcomeCh <- streamOutcome{result: result, err: err}
	}()

	var firstFlush openAITTFTFlushObservation
	createdFlushed := false
	select {
	case firstFlush = <-flushWriter.flushes:
		createdFlushed = true
	case <-time.After(150 * time.Millisecond):
	}
	if createdFlushed {
		time.Sleep(200 * time.Millisecond)
	}
	close(releaseVisible)
	outcome := <-outcomeCh
	require.NoError(t, outcome.err)
	require.NotNil(t, outcome.result)
	require.True(t, createdFlushed, "response.created was not flushed before the delayed visible delta")
	select {
	case <-writerDone:
	case <-time.After(time.Second):
		t.Fatal("synthetic upstream writer did not exit")
	}
	return outcome.result, firstFlush
}

func runSyntheticVisibleTTFTStream(t *testing.T, passthrough bool, includeCreated bool, visibleDelay time.Duration, timeoutSeconds int, visibleEvent string) *openaiStreamingResult {
	t.Helper()
	gin.SetMode(gin.TestMode)
	svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{
		MaxLineSize:                     defaultMaxLineSize,
		OpenAIFirstOutputTimeoutSeconds: timeoutSeconds,
	}}}
	reader, writer := io.Pipe()
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		defer func() { _ = writer.Close() }()
		if includeCreated {
			_, _ = io.WriteString(writer, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_test\"}}\n\n")
		}
		_, _ = io.WriteString(writer, "data: {\"type\":\"response.output_item.added\",\"item\":{\"id\":\"item_test\",\"type\":\"reasoning\",\"summary\":[]}}\n\n")
		time.Sleep(visibleDelay)
		_, _ = io.WriteString(writer, "data: "+visibleEvent+"\n\n")
		_, _ = io.WriteString(writer, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_test\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\n")
	}()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: reader}
	account := &Account{ID: 1, Name: "account_test", Platform: PlatformOpenAI}
	started := time.Now()

	var result *openaiStreamingResult
	var err error
	if passthrough {
		var passthroughResult *openaiStreamingResultPassthrough
		passthroughResult, err = svc.handleStreamingResponsePassthrough(context.Background(), resp, c, account, started, "test-model", "test-model")
		if passthroughResult != nil {
			result = &openaiStreamingResult{firstTokenMs: passthroughResult.firstTokenMs}
		}
	} else {
		result, err = svc.handleStreamingResponse(context.Background(), resp, c, account, started, "test-model", "test-model")
	}
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Contains(t, recorder.Body.String(), `"type":"response.output_item.added"`)
	require.Contains(t, recorder.Body.String(), visibleEvent)
	select {
	case <-writerDone:
	case <-time.After(time.Second):
		t.Fatal("synthetic upstream writer did not exit")
	}
	return result
}
