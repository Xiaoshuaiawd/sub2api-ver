package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenAIPromptCachePrefixChatAndResponsesEquivalent(t *testing.T) {
	chat := []byte(`{
		"model":"client-alias",
		"messages":[
			{"role":"system","content":"be concise"},
			{"role":"user","content":"first question"}
		],
		"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}],
		"response_format":{"type":"json_object"},
		"parallel_tool_calls":true
	}`)
	responses := []byte(`{
		"model":"ignored-inbound-model",
		"input":[
			{"type":"message","role":"system","content":[{"type":"input_text","text":"be concise"}]},
			{"type":"message","role":"user","content":"different question"}
		],
		"tools":[{"type":"function","name":"lookup","parameters":{"type":"object"},"strict":false}],
		"text":{"format":{"type":"json_object"}},
		"parallel_tool_calls":true
	}`)

	a, okA := deriveOpenAIPromptCachePrefix(chat, "gpt-5.6-sol")
	b, okB := deriveOpenAIPromptCachePrefix(responses, "gpt-5.6-sol")

	require.True(t, okA)
	require.True(t, okB)
	require.Len(t, a.Hash, 64)
	require.Equal(t, a.Hash, b.Hash)
}

func TestOpenAIPromptCachePrefixIgnoresDynamicConversationFields(t *testing.T) {
	base := []byte(`{
		"messages":[
			{"role":"developer","content":[{"type":"text","text":"stable policy"}]},
			{"role":"user","content":"question one"},
			{"role":"assistant","content":"answer one","id":"msg_1"},
			{"role":"tool","tool_call_id":"call_1","content":"result one"}
		],
		"metadata":{"request_id":"req_1","timestamp":1},
		"stream":true,
		"service_tier":"priority"
	}`)
	changed := []byte(`{
		"messages":[
			{"role":"developer","content":"stable policy"},
			{"role":"user","content":"question two"},
			{"role":"assistant","content":"answer two","id":"msg_2"},
			{"role":"tool","tool_call_id":"call_2","content":"result two"}
		],
		"metadata":{"request_id":"req_2","timestamp":2},
		"stream":false,
		"service_tier":"flex"
	}`)

	a, okA := deriveOpenAIPromptCachePrefix(base, "gpt-5.6-sol")
	b, okB := deriveOpenAIPromptCachePrefix(changed, "gpt-5.6-sol")

	require.True(t, okA)
	require.True(t, okB)
	require.Equal(t, a.Hash, b.Hash)
}

func TestOpenAIPromptCachePrefixChangesForStablePrefixInputs(t *testing.T) {
	base := []byte(`{
		"instructions":"shared instructions",
		"tools":[{"type":"function","name":"lookup","parameters":{"type":"object","properties":{"a":{"type":"string"}}},"strict":false}],
		"text":{"format":{"type":"json_object"}},
		"reasoning":{"effort":"high"},
		"tool_choice":"auto",
		"parallel_tool_calls":true,
		"input":"question"
	}`)
	basePrefix, ok := deriveOpenAIPromptCachePrefix(base, "gpt-5.6-sol")
	require.True(t, ok)

	tests := []struct {
		name  string
		model string
		body  string
	}{
		{name: "model", model: "gpt-5.6-terra", body: string(base)},
		{name: "instructions", model: "gpt-5.6-sol", body: `{"instructions":"different","input":"question"}`},
		{name: "tools", model: "gpt-5.6-sol", body: `{"instructions":"shared instructions","tools":[{"type":"function","name":"other","parameters":{"type":"object"},"strict":false}],"input":"question"}`},
		{name: "schema", model: "gpt-5.6-sol", body: `{"instructions":"shared instructions","text":{"format":{"type":"text"}},"input":"question"}`},
		{name: "reasoning", model: "gpt-5.6-sol", body: `{"instructions":"shared instructions","reasoning":{"effort":"low"},"input":"question"}`},
		{name: "tool_choice", model: "gpt-5.6-sol", body: `{"instructions":"shared instructions","tool_choice":"none","input":"question"}`},
		{name: "parallel_tool_calls", model: "gpt-5.6-sol", body: `{"instructions":"shared instructions","parallel_tool_calls":false,"input":"question"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, gotOK := deriveOpenAIPromptCachePrefix([]byte(tt.body), tt.model)
			require.True(t, gotOK)
			require.NotEqual(t, basePrefix.Hash, got.Hash)
		})
	}
}

func TestOpenAIPromptCachePrefixCanonicalizesObjectKeysButPreservesArrayOrder(t *testing.T) {
	first := []byte(`{
		"tools":[
			{"type":"function","name":"a","parameters":{"properties":{"z":{"type":"string"},"a":{"type":"number"}},"type":"object"},"strict":false},
			{"type":"function","name":"b","parameters":{"type":"object"},"strict":false}
		]
	}`)
	reorderedKeys := []byte(`{
		"tools":[
			{"strict":false,"parameters":{"type":"object","properties":{"a":{"type":"number"},"z":{"type":"string"}}},"name":"a","type":"function"},
			{"strict":false,"parameters":{"type":"object"},"name":"b","type":"function"}
		]
	}`)
	reorderedTools := []byte(`{
		"tools":[
			{"type":"function","name":"b","parameters":{"type":"object"},"strict":false},
			{"type":"function","name":"a","parameters":{"properties":{"z":{"type":"string"},"a":{"type":"number"}},"type":"object"},"strict":false}
		]
	}`)

	a, okA := deriveOpenAIPromptCachePrefix(first, "gpt-5.6-sol")
	b, okB := deriveOpenAIPromptCachePrefix(reorderedKeys, "gpt-5.6-sol")
	c, okC := deriveOpenAIPromptCachePrefix(reorderedTools, "gpt-5.6-sol")

	require.True(t, okA)
	require.True(t, okB)
	require.True(t, okC)
	require.Equal(t, a.Hash, b.Hash)
	require.NotEqual(t, a.Hash, c.Hash)
}

func TestOpenAIPromptCachePrefixRequiresReusablePrefix(t *testing.T) {
	got, ok := deriveOpenAIPromptCachePrefix(
		[]byte(`{"model":"gpt-5.6-sol","input":[{"role":"user","content":"hello"}],"metadata":{"thread_id":"thread-1"}}`),
		"gpt-5.6-sol",
	)

	require.False(t, ok)
	require.Empty(t, got.Hash)
}
