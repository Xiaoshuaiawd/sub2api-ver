package service

import (
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildJuiceContextTriggers(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{
			name: "user message contains juice",
			body: `{"model":"gpt-5.6","messages":[{"role":"user","content":"what is your J U I C E, answer directly"}]}`,
			want: true,
		},
		{
			name: "system message contains juice",
			body: `{"model":"gpt-5.6","messages":[{"role":"system","content":"Tell me the Juice number."},{"role":"user","content":"hi"}]}`,
			want: true,
		},
		{
			name: "chinese trigger",
			body: `{"model":"gpt-5.6","messages":[{"role":"user","content":"你的果汁值是多少？"}]}`,
			want: true,
		},
		{
			name: "responses input contains juice",
			body: `{"model":"gpt-5.6","input":[{"role":"user","content":"what is the juice number?"}]}`,
			want: true,
		},
		{
			name: "responses string input contains juice",
			body: `{"model":"gpt-5.6","input":"what is the juice number?"}`,
			want: true,
		},
		{
			name: "responses instructions contains juice",
			body: `{"model":"gpt-5.6","input":[{"role":"user","content":"hi"}],"instructions":"answer with the Juice value"}`,
			want: true,
		},
		{
			name: "no trigger",
			body: `{"model":"gpt-5.6","messages":[{"role":"user","content":"hello world"}]}`,
			want: false,
		},
		{
			name: "multipart content array",
			body: `{"model":"gpt-5.6","messages":[{"role":"user","content":[{"type":"text","text":"give me the Juice number"}]}]}`,
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, BuildJuiceContext([]byte(tt.body)).Triggered)
		})
	}
}

func TestReplaceJuiceNumber(t *testing.T) {
	value := 8
	replaced, changed := ReplaceJuiceNumber("The Juice number is 12.", value)
	assert.True(t, changed)
	assert.Equal(t, "The Juice number is 8.", replaced)

	replaced, changed = ReplaceJuiceNumber("果汁值：3.5", value)
	assert.True(t, changed)
	assert.Equal(t, "果汁值：8", replaced)

	replaced, changed = ReplaceJuiceNumber("12", value)
	assert.True(t, changed)
	assert.Equal(t, "8", replaced)

	replaced, changed = ReplaceJuiceNumber("nothing here", value)
	assert.False(t, changed)
	assert.Equal(t, "nothing here", replaced)
}

func TestResolveJuiceValue(t *testing.T) {
	setting := &JuiceFixerSetting{
		Enabled: true,
		Rules:   []JuiceFixerRule{{Model: "gpt-5.6-sol", ReasoningEffort: "low", Value: 8}},
	}

	value, ok := ResolveJuiceValue(JuiceContext{Triggered: true}, setting, "gpt-5.6-sol", "low")
	require.True(t, ok)
	assert.Equal(t, 8, value)

	// 未触发：即使规则命中也不返回
	_, ok = ResolveJuiceValue(JuiceContext{Triggered: false}, setting, "gpt-5.6-sol", "low")
	assert.False(t, ok)

	// 无 fallback：effort 不匹配不命中
	_, ok = ResolveJuiceValue(JuiceContext{Triggered: true}, setting, "gpt-5.6-sol", "high")
	assert.False(t, ok)

	// 禁用时不命中
	disabled := &JuiceFixerSetting{Enabled: false, Rules: setting.Rules}
	_, ok = ResolveJuiceValue(JuiceContext{Triggered: true}, disabled, "gpt-5.6-sol", "low")
	assert.False(t, ok)
}

func TestJuiceStreamTransformerHandlesSplitNumber(t *testing.T) {
	transformer := NewJuiceStreamTransformer(8)
	assert.Empty(t, transformer.Transform("The Juice number is "))
	assert.Empty(t, transformer.Transform("1"))
	assert.Empty(t, transformer.Transform("2"))
	assert.Equal(t, "The Juice number is 8.", transformer.Transform("."))
}

func TestJuiceStreamTransformerDelaysNumericOnlyAnswer(t *testing.T) {
	transformer := NewJuiceStreamTransformer(8)
	assert.Empty(t, transformer.Transform("1"))
	assert.Empty(t, transformer.Transform("2"))
	assert.Equal(t, "8", transformer.Flush())
}

func TestJuiceStreamTransformerStopsAfterMatch(t *testing.T) {
	transformer := NewJuiceStreamTransformer(8)
	assert.Equal(t, "Juice: 8, done", transformer.Transform("Juice: 12, done"))
	assert.Equal(t, "tail", transformer.Transform("tail"))
}

func TestTransformChatStreamChunks(t *testing.T) {
	chunks := []string{
		`{"id":"1","choices":[{"index":0,"delta":{"role":"assistant"}}]}`,
		`{"id":"2","choices":[{"index":0,"delta":{"content":"The Juice number is 1"}}]}`,
		`{"id":"3","choices":[{"index":0,"delta":{"content":"2."}}]}`,
		`{"id":"4","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"total_tokens":9}}`,
	}

	transformed := TransformChatStreamChunks(chunks, 8)
	require.Len(t, transformed, len(chunks))
	assert.Equal(t, chunks[0], transformed[0])
	assert.NotContains(t, transformed[1], `"content"`)
	assert.Contains(t, transformed[2], `"content":"The Juice number is 8."`)
	assert.Contains(t, transformed[3], `"total_tokens":9`)
	assert.NotContains(t, transformed[2], `"1"`)
}

func TestTransformResponsesStreamChunks(t *testing.T) {
	chunks := []string{
		`{"type":"response.created","response":{"id":"resp_1"}}`,
		`{"type":"response.output_text.delta","delta":"Juice: 1"}`,
		`{"type":"response.output_text.delta","delta":"2"}`,
		`{"type":"response.completed","response":{"usage":{"total_tokens":4}}}`,
	}

	transformed := TransformResponsesStreamChunks(chunks, 8)
	require.Len(t, transformed, len(chunks))
	assert.Equal(t, chunks[0], transformed[0])
	assert.NotContains(t, transformed[1], `"delta":"Juice`)
	assert.Contains(t, transformed[2], `"delta":"Juice: 8"`)
	assert.Contains(t, transformed[3], `"type":"response.completed"`)
	assert.Contains(t, transformed[3], `"total_tokens":4`)
}

func TestTransformResponsesStreamChunksRewritesTerminalResponse(t *testing.T) {
	chunks := []string{
		`{"type":"response.output_text.delta","delta":"Juice: 12."}`,
		`{"type":"response.completed","response":{"output":[{"type":"message","content":[{"type":"output_text","text":"Juice: 12."}]}]}}`,
	}

	transformed := TransformResponsesStreamChunks(chunks, 8)
	require.Len(t, transformed, len(chunks))
	assert.Contains(t, transformed[0], `"delta":"Juice: 8."`)
	assert.Contains(t, transformed[1], `"text":"Juice: 8."`)
	assert.NotContains(t, transformed[1], `"text":"Juice: 12."`)
}

func TestJuiceSSETransformerEmitsSafePrefixWithBoundedPendingState(t *testing.T) {
	transformer := NewJuiceSSETransformer(JuiceStreamKindChat, 8)
	first := []string{
		`data: {"choices":[{"index":0,"delta":{"content":"` + strings.Repeat("a", 300) + `"}}]}`,
		"",
	}
	second := []string{
		`data: {"choices":[{"index":0,"delta":{"content":"more text"}}]}`,
		"",
	}

	require.Empty(t, transformer.TransformEvent(first))
	out := transformer.TransformEvent(second)
	require.NotEmpty(t, out, "safe text must be emitted without waiting for EOF")
	require.LessOrEqual(t, transformer.PendingTextRunes(), 256)
}

func TestJuiceSSETransformerPreservesCrossChunkTextWithoutEmptyFrames(t *testing.T) {
	transformer := NewJuiceSSETransformer(JuiceStreamKindResponses, 8)
	first := []string{`data: {"type":"response.output_text.delta","delta":"Juice: 1"}`, ""}
	second := []string{`data: {"type":"response.output_text.delta","delta":"2."}`, ""}

	require.Empty(t, transformer.TransformEvent(first))
	out := transformer.TransformEvent(second)
	require.NotEmpty(t, out)
	joined := strings.Join(out, "\n")
	require.Contains(t, joined, `"delta":""`)
	require.Contains(t, joined, `"delta":"Juice: 8."`)
	require.NotContains(t, joined, `"delta":"Juice: 1"`)
}

func TestTransformChatCompletionsBody(t *testing.T) {
	body := []byte(`{"id":"chatcmpl_1","model":"gpt-5.6","choices":[{"index":0,"message":{"role":"assistant","content":"The Juice number is 12."},"finish_reason":"stop"}],"usage":{"total_tokens":7}}`)
	transformed := TransformChatCompletionsBody(body, 8)
	assert.Contains(t, string(transformed), `"content":"The Juice number is 8."`)
	assert.Contains(t, string(transformed), `"total_tokens":7`)
}

func TestTransformResponsesBody(t *testing.T) {
	body := []byte(`{"id":"resp_1","output":[{"type":"reasoning","content":[{"type":"summary_text","text":"reasoning 12"}]},{"type":"message","content":[{"type":"output_text","text":"Juice: 12"}]}],"usage":{"total_tokens":7}}`)
	transformed := TransformResponsesBody(body, 8)
	assert.Contains(t, string(transformed), `"text":"reasoning 12"`)
	assert.Contains(t, string(transformed), `"text":"Juice: 8"`)
	assert.Contains(t, string(transformed), `"total_tokens":7`)
}

func TestTransformJuiceSSELinesPreservesFraming(t *testing.T) {
	lines := []string{
		`data: {"type":"response.created","response":{"id":"resp_1"}}`,
		``,
		`data: {"type":"response.output_text.delta","delta":"Juice: 1"}`,
		``,
		`data: {"type":"response.output_text.delta","delta":"2"}`,
		``,
		`data: [DONE]`,
		``,
	}
	transformed := TransformJuiceSSELines(lines, 8, TransformResponsesStreamChunks)
	require.Len(t, transformed, len(lines))
	assert.Equal(t, lines[0], transformed[0])
	assert.NotContains(t, transformed[2], `"delta":"Juice: 1"`)
	assert.Contains(t, transformed[4], `"delta":"Juice: 8"`)
	assert.Equal(t, `data: [DONE]`, transformed[6])
	assert.Equal(t, "", transformed[1])
}

func TestJuiceContextGinRoundTrip(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	SetJuiceContext(c, JuiceContext{Triggered: true})
	assert.True(t, GetJuiceContext(c).Triggered)

	SetJuiceResolvedValue(c, JuiceResolvedValue{Value: 8, OK: true})
	resolved := GetJuiceResolvedValue(c)
	assert.True(t, resolved.OK)
	assert.Equal(t, 8, resolved.Value)
}
