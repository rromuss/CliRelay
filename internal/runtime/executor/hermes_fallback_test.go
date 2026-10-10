package executor

import (
	"bytes"
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

func TestHermesRetirementNoticeIsExactAndDoesNotMisclassifyAnswers(t *testing.T) {
	body := []byte(`{"response":{"candidates":[{"content":{"parts":[{"text":"` + retiredGeminiFlashNotice + `"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":0,"candidatesTokenCount":0,"totalTokenCount":0}}}`)
	opts := cliproxyexecutor.Options{Metadata: map[string]any{cliproxyexecutor.HermesManualMetadataKey: true}}
	if err := hermesAntigravityRetiredError("gemini-3.5-flash-low", body, opts); err == nil {
		t.Fatal("retirement not detected")
	}
	for _, tc := range []struct {
		model string
		body  []byte
		opts  cliproxyexecutor.Options
	}{
		{"gemini-3.8-flash-tiered", body, opts},
		{"gemini-3.5-flash-lite", body, opts},
		{"gemini-3.5-flash-low", body, cliproxyexecutor.Options{}},
		{"gemini-3.5-flash-low", bytes.Replace(body, []byte(`"candidatesTokenCount":0`), []byte(`"candidatesTokenCount":8`), 1), opts},
		{"gemini-3.5-flash-low", bytes.Replace(body, []byte(`"usageMetadata":`), []byte(`"modelVersion":"gemini-3.6-flash","usageMetadata":`), 1), opts},
		{"gemini-3.5-flash-low", bytes.Replace(body, []byte(`"STOP"`), []byte(`"SAFETY"`), 1), opts},
		{"gemini-3.5-flash-low", bytes.Replace(body, []byte(retiredGeminiFlashNotice), []byte("An example notice: "+retiredGeminiFlashNotice), 1), opts},
		{"gemini-3.5-flash-low", []byte(`{"response":{"candidates":[{"content":{"parts":[{"functionCall":{"name":"synthetic","args":{}}}]}}]}}`), opts},
	} {
		if err := hermesAntigravityRetiredError(tc.model, tc.body, tc.opts); err != nil {
			t.Fatal("normal response/client classified as retired", tc.model)
		}
	}
}

func TestHermesRetirementHTTP200Becomes410BeforeTranslationOneAttempt(t *testing.T) {
	var count atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"response":{"candidates":[{"content":{"parts":[{"text":"`+retiredGeminiFlashNotice+`"}]}}],"usageMetadata":{"totalTokenCount":0}}}`)
	}))
	defer server.Close()
	payload := []byte(`{"model":"gemini-3.5-flash-low","messages":[{"role":"user","content":"synthetic"}],"reasoning_effort":"medium"}`)
	opts := cliproxyexecutor.Options{SourceFormat: sdktranslator.FromString("openai"), OriginalRequest: payload, Metadata: map[string]any{cliproxyexecutor.HermesManualMetadataKey: true, cliproxyexecutor.SinglePickMetadataKey: true}}
	resp, err := NewAntigravityExecutor(&config.Config{RequestRetry: 3}).Execute(context.Background(), antigravityTestAuth(server.URL), cliproxyexecutor.Request{Model: "gemini-3.5-flash-low", Payload: payload}, opts)
	se, ok := err.(interface{ StatusCode() int })
	if !ok || se.StatusCode() != http.StatusGone {
		t.Fatal("expected 410", err)
	}
	if count.Load() != 1 || len(resp.Payload) != 0 {
		t.Fatal("notice translated/retried", count.Load())
	}
	if gjson.Get(err.Error(), "error.code").String() != "model_retired" {
		t.Fatal("machine-readable code missing")
	}
}

func TestHermesV2TranslatedProfilesPreserveModelEffortAndToolHistory(t *testing.T) {
	for _, model := range []string{"gemini-3.8-flash-tiered", "gemini-3.6-flash-medium", "gemini-3.1-flash-lite"} {
		payload := []byte(`{"model":"` + model + `","reasoning_effort":"medium","messages":[{"role":"user","content":"synthetic"},{"role":"assistant","tool_calls":[{"id":"done","type":"function","function":{"name":"synthetic","arguments":"{}"}}]},{"role":"tool","tool_call_id":"done","content":"already done"}]}`)
		opts := cliproxyexecutor.Options{SourceFormat: sdktranslator.FromString("openai"), OriginalRequest: payload, Metadata: map[string]any{cliproxyexecutor.HermesFallbackMetadataKey: true, cliproxyexecutor.HermesFallbackV2MetadataKey: true}}
		e := NewAntigravityExecutor(nil)
		ec := e.newAntigravityExecutionContext(context.Background(), nil, cliproxyexecutor.Request{Model: model, Payload: payload}, opts, false)
		body, err := e.buildAntigravityPayload(ec)
		if err != nil {
			t.Fatal(err)
		}
		if gjson.GetBytes(body, "request.generationConfig.thinkingConfig.thinkingLevel").String() != "medium" {
			t.Fatal("v2 effort lost", model)
		}
		if !bytes.Contains(body, []byte("already done")) {
			t.Fatal("completed tool missing")
		}
		_, wire, err := e.buildRequest(context.Background(), nil, "synthetic", ec.BaseModel, body, false, "", "http://synthetic.invalid")
		if err != nil || gjson.GetBytes(wire, "model").String() != model {
			t.Fatal("upstream model replaced", model, err)
		}
	}
}

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

func TestHermesManual31OneActualRequestNoOtherModelAndMedium(t *testing.T) {
	var sent [][]byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		sent = append(sent, b)
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, `{"error":{"message":"no capacity available"}}`)
	}))
	defer server.Close()
	payload := []byte(`{"model":"gemini-3.1-flash-lite","messages":[{"role":"user","content":"synthetic"}],"reasoning_effort":"medium"}`)
	opts := cliproxyexecutor.Options{SourceFormat: sdktranslator.FromString("openai"), OriginalRequest: payload, Metadata: map[string]any{cliproxyexecutor.HermesManualMetadataKey: true, cliproxyexecutor.SinglePickMetadataKey: true}}
	_, err := NewAntigravityExecutor(&config.Config{RequestRetry: 3}).Execute(context.Background(), antigravityTestAuth(server.URL), cliproxyexecutor.Request{Model: "gemini-3.1-flash-lite", Payload: payload}, opts)
	if err == nil || len(sent) != 1 {
		t.Fatal("manual executor retried/switched", len(sent), err)
	}
	if gjson.GetBytes(sent[0], "model").String() != "gemini-3.1-flash-lite" {
		t.Fatal("manual model replaced")
	}
	if gjson.GetBytes(sent[0], "request.generationConfig.thinkingConfig.thinkingLevel").String() != "medium" {
		t.Fatal("manual medium lost")
	}
	urls := []string{server.URL, "https://never-contact.invalid"}
	attempts, selected := antigravityAttemptPlan(nil, &config.Config{RequestRetry: 3}, opts, urls)
	if attempts != 1 || len(selected) != 1 {
		t.Fatal("alternate endpoint remains enabled")
	}
}

func TestHermesManualGeminiEffortSurvivesDynamicModelAndPayloadRules(t *testing.T) {
	for _, effort := range []string{"medium", "high", "none"} {
		payload := []byte(`{"messages":[{"role":"user","content":"synthetic"}],"reasoning_effort":"` + effort + `"}`)
		opts := cliproxyexecutor.Options{SourceFormat: sdktranslator.FromString("openai"), OriginalRequest: payload, Metadata: map[string]any{cliproxyexecutor.HermesManualMetadataKey: true}}
		e := NewAntigravityExecutor(nil)
		ec := e.newAntigravityExecutionContext(context.Background(), nil, cliproxyexecutor.Request{Model: "gemini-3.1-flash-lite", Payload: payload}, opts, false)
		body, err := e.buildAntigravityPayload(ec)
		if err != nil {
			t.Fatal(err)
		}
		got := gjson.GetBytes(body, "request.generationConfig.thinkingConfig.thinkingLevel").String()
		if effort == "none" {
			if got != "" {
				t.Fatal("manual none became thinking")
			}
		} else if got != effort {
			t.Fatal("manual effort changed", effort, got)
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
