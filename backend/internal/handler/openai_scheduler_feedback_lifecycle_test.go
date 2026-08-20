package handler

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenAIWebSocketUsesFreshSelectionForEveryTurn(t *testing.T) {
	source := stripGoComments(goFunctionSource(t, "openai_gateway_handler.go", "ResponsesWebSocket"))
	require.Contains(t, source, "selection.AcquireTurn(",
		"BeforeTurn must reserve a fresh adaptive permit for the bound account")
	require.Contains(t, source, "ReportOpenAIAccountSelectionResult(turnSelection,",
		"AfterTurn must report against the permit acquired for that turn")
	require.GreaterOrEqual(t, strings.Count(source, "turnSelection.ReleaseFunc"), 1,
		"every per-turn permit needs an explicit release path")
}

func TestOpenAICountTokensReportsSelectionOutcome(t *testing.T) {
	source := stripGoComments(goFunctionSource(t, "openai_gateway_count_tokens.go", "CountTokens"))
	forwardIndex := strings.Index(source, "forwardFeedback, forwardErr := h.gatewayService.ForwardCountTokensAsAnthropic(")
	reportIndex := strings.Index(source, "ReportOpenAIAccountSelectionResult(selection,")
	require.NotEqual(t, -1, forwardIndex, "count_tokens must retain the forwarding outcome")
	require.NotEqual(t, -1, reportIndex, "count_tokens must report adaptive permit outcome")
	require.Less(t, forwardIndex, reportIndex, "feedback must describe the completed upstream attempt")
	require.Contains(t, source[forwardIndex:reportIndex], "if forwardFeedback.ReportSelectionResult {",
		"local conversion and estimation failures must not affect account health")
	require.Contains(t, source[reportIndex:], "forwardFeedback.Success",
		"success and failure must both close the permit feedback lifecycle")
}

func TestOpenAIEmbeddingsChecksClientCancellationBeforeFailureFeedback(t *testing.T) {
	source := stripGoComments(goFunctionSource(t, "openai_embeddings.go", "Embeddings"))
	require.GreaterOrEqual(t, strings.Count(source, "if !failoverClientGone(c) && failoverErr.ShouldReportAccountScheduleFailure() {"), 2,
		"both written and unwritten failover errors must exclude disconnected clients before feedback")
	require.Contains(t, source, "if !failoverClientGone(c) {",
		"generic forwarding errors must exclude disconnected clients before feedback")
}

func TestOpenAIWrittenStreamingFailuresReportBeforeReturning(t *testing.T) {
	tests := []struct {
		file     string
		function string
		branch   string
	}{
		{file: "openai_gateway_handler.go", function: "Responses", branch: "if !openAIForwardMayFailover(c, writerSizeBeforeForward, failoverErr) {"},
		{file: "openai_gateway_handler.go", function: "Messages", branch: "if c.Writer.Size() != writerSizeBeforeForward {"},
		{file: "openai_chat_completions.go", function: "ChatCompletions", branch: "if c.Writer.Size() != writerSizeBeforeForward {"},
		{file: "openai_embeddings.go", function: "Embeddings", branch: "if c.Writer.Size() != writerSizeBeforeForward {"},
	}
	for _, tt := range tests {
		t.Run(tt.function, func(t *testing.T) {
			source := stripGoComments(goFunctionSource(t, tt.file, tt.function))
			branchCount := 0
			for offset := 0; ; {
				index := strings.Index(source[offset:], tt.branch)
				if index < 0 {
					break
				}
				branchCount++
				start := offset + index
				returnIndex := strings.Index(source[start:], "return")
				require.NotEqual(t, -1, returnIndex)
				beforeReturn := source[start : start+returnIndex]
				require.Contains(t, beforeReturn, "ReportOpenAIAccountSelectionResult(selection,",
					"a known upstream failure must be reported even after streaming output was written")
				offset = start + len(tt.branch)
			}
			require.Positive(t, branchCount, "test case must cover a written-response early return")
		})
	}
}
