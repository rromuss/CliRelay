package executor

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v6/sdk/translator"
	"github.com/tidwall/gjson"
)

func TestHermesFallbackSingleActualUpstreamRequestOnNoCapacity(t *testing.T) {
	for _, marked := range []bool{false, true} {
		var count atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			count.Add(1)
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, `{"error":{"message":"no capacity available"}}`)
		}))
		opts := cliproxyexecutor.Options{SourceFormat: sdktranslator.FromString("antigravity"), Metadata: map[string]any{cliproxyexecutor.HermesFallbackMetadataKey: marked}}
		_, err := NewAntigravityExecutor(&config.Config{RequestRetry: 3}).Execute(context.Background(), antigravityTestAuth(server.URL), cliproxyexecutor.Request{
			Model: "gemini-3.8-flash-tiered", Payload: []byte(`{"request":{"contents":[{"role":"user","parts":[{"text":"synthetic"}]}]}}`),
		}, opts)
		server.Close()
		want := int32(4)
		if marked {
			want = 1
		}
		if err == nil || count.Load() != want {
			t.Fatalf("marked=%v requests=%d want=%d error=%v", marked, count.Load(), want, err)
		}
	}
}

func TestHermesFallbackLunaMediumAndCompletedToolResultReachCompatUpstream(t *testing.T) {
	var sent []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sent, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()
	auth := &cliproxyauth.Auth{Attributes: map[string]string{"base_url": server.URL + "/v1", "api_key": "test"}}
	payload := []byte(`{"model":"gpt-6-luna","reasoning_effort":"medium","messages":[{"role":"user","content":"synthetic"},{"role":"assistant","tool_calls":[{"id":"call-1","type":"function","function":{"name":"synthetic_tool","arguments":"{}"}}]},{"role":"tool","tool_call_id":"call-1","content":"already completed"}]}`)
	cfg := &config.Config{Payload: config.PayloadConfig{Override: []config.PayloadRule{{Models: []config.PayloadModelRule{{Name: "gpt-6-luna", Protocol: "openai"}}, Params: map[string]any{"reasoning_effort": "high", "reasoning": map[string]any{"effort": "high"}}}}}}
	_, err := NewOpenAICompatExecutor("modelhub", cfg).Execute(context.Background(), auth, cliproxyexecutor.Request{Model: "gpt-6-luna", Payload: payload}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FromString("openai"), Metadata: map[string]any{cliproxyexecutor.HermesFallbackMetadataKey: true, cliproxyexecutor.SinglePickMetadataKey: true}})
	if err != nil {
		t.Fatal(err)
	}
	if gjson.GetBytes(sent, "reasoning_effort").String() != "medium" {
		t.Fatal("Luna medium lost", string(sent))
	}
	if gjson.GetBytes(sent, "reasoning").Exists() {
		t.Fatal("conflicting reasoning override survived")
	}
	if gjson.GetBytes(sent, "messages.2.tool_call_id").String() != "call-1" || gjson.GetBytes(sent, "messages.2.content").String() != "already completed" {
		t.Fatal("completed tool result lost", string(sent))
	}
}

func TestHermesFallbackSuppressesEveryExecutorRetryAndEndpointFallback(t *testing.T) {
	cfg := &config.Config{RequestRetry: 3}
	urls := []string{"https://first.invalid", "https://second.invalid"}
	marked := cliproxyexecutor.Options{Metadata: map[string]any{cliproxyexecutor.HermesFallbackMetadataKey: true}}
	attempts, selected := antigravityAttemptPlan(nil, cfg, marked, urls)
	if attempts != 1 || len(selected) != 1 || selected[0] != urls[0] {
		t.Fatal("executor can send multiple attempts", attempts, selected)
	}
	attempts, selected = antigravityAttemptPlan(nil, cfg, cliproxyexecutor.Options{}, urls)
	if attempts != 4 || len(selected) != 2 {
		t.Fatal("ordinary request policy changed", attempts, selected)
	}
}

func TestHermesFallbackPreservesMediumThroughActualTranslation(t *testing.T) {
	for _, model := range []string{"gemini-3.8-flash-tiered", "gemini-3.6-flash-tiered", "gemini-3.5-flash-lite", "gemini-3.1-flash-lite"} {
		t.Run(model, func(t *testing.T) {
			payload := []byte(`{"messages":[{"role":"user","content":"synthetic input"}],"reasoning_effort":"medium"}`)
			opts := cliproxyexecutor.Options{SourceFormat: sdktranslator.FromString("openai"), Metadata: map[string]any{cliproxyexecutor.HermesFallbackMetadataKey: true}}
			e := NewAntigravityExecutor(nil)
			ec := e.newAntigravityExecutionContext(context.Background(), nil, cliproxyexecutor.Request{Model: model, Payload: payload}, opts, false)
			body, err := e.buildAntigravityPayload(ec)
			if err != nil {
				t.Fatal(err)
			}
			if got := gjson.GetBytes(body, "request.generationConfig.thinkingConfig.thinkingLevel").String(); got != "medium" {
				t.Fatal("medium lost", got, string(body))
			}
			if gjson.GetBytes(body, "request.generationConfig.thinkingConfig.thinkingBudget").Exists() {
				t.Fatal("conflicting budget retained")
			}
			if !gjson.GetBytes(body, "request.contents").Exists() {
				t.Fatal("conversation lost during translation", string(body))
			}
		})
	}
}

func TestHermesMediumRemovesConflictingBudget(t *testing.T) {
	body, err := applyHermesGeminiMedium([]byte(`{"request":{"generationConfig":{"thinkingConfig":{"thinkingBudget":8192,"thinkingLevel":"high"}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if gjson.GetBytes(body, "request.generationConfig.thinkingConfig.thinkingBudget").Exists() || gjson.GetBytes(body, "request.generationConfig.thinkingConfig.thinkingLevel").String() != "medium" {
		t.Fatal(string(body))
	}
}
